package nodeagent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// This closed product relay uses the existing paired private agent stream.
// The target owner supplies its product token locally; no bearer is returned.
type ManagedProductRequest struct {
	Target         api.WorkTarget      `json:"target"`
	Identity       productrpc.Identity `json:"identity"`
	Method         string              `json:"method"`
	Path           string              `json:"path"`
	Body           []byte              `json:"body,omitempty"`
	ResourceID     string              `json:"resourceId,omitempty"`
	ResourceName   string              `json:"resourceName,omitempty"`
	ResourceSize   string              `json:"resourceSize,omitempty"`
	ResourceSHA256 string              `json:"resourceSha256,omitempty"`
}
type ManagedProductResponse struct {
	Status         int    `json:"status"`
	ContentType    string `json:"contentType"`
	Body           []byte `json:"body"`
	ResourceName   string `json:"resourceName,omitempty"`
	ResourceSize   string `json:"resourceSize,omitempty"`
	ResourceSHA256 string `json:"resourceSha256,omitempty"`
}

const MaxManagedProductBody = 4 << 20
const MaxManagedProductRequest = 256 << 10 // base64 envelope remains below native frame limit
func ValidManagedProductRequest(r ManagedProductRequest) bool {
	if r.Target.Validate() != nil || r.Target.Role != api.RoleBot || r.Identity.NodeID != r.Target.NodeID || r.Identity.Generation == "" || len(r.Body) > MaxManagedProductRequest {
		return false
	}
	if r.Path == "/v1/resources" {
		if r.Method == "GET" {
			return identifier.MatchString(r.ResourceID) && len(r.Body) == 0 && r.ResourceName == "" && r.ResourceSize == "" && r.ResourceSHA256 == ""
		}
		if r.Method == "PUT" {
			return r.ResourceID == "" && len(r.ResourceName) > 0 && len(r.ResourceName) <= 384 && len(r.ResourceSize) > 0 && len(r.ResourceSize) <= 20 && len(r.ResourceSHA256) == 64
		}
		return false
	}
	if r.ResourceID != "" || r.ResourceName != "" || r.ResourceSize != "" || r.ResourceSHA256 != "" {
		return false
	}
	switch r.Method {
	case "GET":
		return r.Path == "/v1/identity" && len(r.Body) == 0
	case "POST":
		switch r.Path {
		case "/v1/state", "/v1/watch", "/v1/commands", "/v1/receipt", "/v1/management/capabilities", "/v1/management/releases", "/v1/management/status", "/v1/management/configuration", "/v1/management/resolve", "/v1/management/execution":
			return true
		}
	}
	return false
}
func (s *Service) ProxyManagedProduct(ctx context.Context, r ManagedProductRequest) (ManagedProductResponse, error) {
	if r.Target.NodeID != s.options.NodeID || !ValidManagedProductRequest(r) || s.options.ManagedProduct == nil {
		return ManagedProductResponse{}, errors.New("managed product proxy unavailable")
	}
	return s.options.ManagedProduct.ProxyManagedProduct(ctx, r)
}
func (c *Client) ProxyManagedProduct(ctx context.Context, r ManagedProductRequest) (ManagedProductResponse, error) {
	var out ManagedProductResponse
	if r.Target.NodeID != c.expected || !ValidManagedProductRequest(r) {
		return out, errors.New("managed product proxy scope mismatch")
	}
	err := c.request(ctx, "POST", "/v1/node/managed-product-proxy", r, &out)
	if err == nil && (out.Status < 100 || out.Status > 599 || (out.ContentType != "application/json" && out.ContentType != "application/octet-stream") || len(out.Body) > MaxManagedProductBody) {
		err = errors.New("managed product proxy response invalid")
	}
	return out, err
}
func (m *ManagedStarter) ProxyManagedProduct(ctx context.Context, r ManagedProductRequest) (ManagedProductResponse, error) {
	c, err := m.child(ctx)
	if err != nil {
		return ManagedProductResponse{}, err
	}
	defer c.Close()
	return c.ProxyManagedProduct(ctx, r)
}

type managedProductTransport struct {
	client   *Client
	target   api.WorkTarget
	identity productrpc.Identity
}

// ManagedProductTransport carries standard product requests over an already
// paired outgoing-only agent connection. URL/auth-file never select a peer.
func ManagedProductTransport(c *Client, t api.WorkTarget, i productrpc.Identity) (http.RoundTripper, error) {
	if c == nil || t.NodeID != c.expected || t.Validate() != nil || t.Role != api.RoleBot || i.NodeID != t.NodeID || i.Generation == "" {
		return nil, errors.New("exact paired managed product required")
	}
	return &managedProductTransport{c, t, i}, nil
}
func (t *managedProductTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if queryErr != nil || r.URL.Fragment != "" || (r.URL.RawQuery != "" && (r.Method != "GET" || r.URL.Path != "/v1/resources" || len(query) != 1 || len(query["id"]) != 1)) {
		return nil, errors.New("managed product path unavailable")
	}
	var body []byte
	if r.Body != nil {
		defer r.Body.Close()
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, MaxManagedProductRequest+1))
		if err != nil || len(body) > MaxManagedProductRequest {
			return nil, errors.New("managed product body exceeds paired frame limit")
		}
	}
	out, err := t.client.ProxyManagedProduct(r.Context(), ManagedProductRequest{Target: t.target, Identity: t.identity, Method: r.Method, Path: r.URL.Path, Body: body, ResourceID: query.Get("id"), ResourceName: r.Header.Get("X-Resource-Name"), ResourceSize: r.Header.Get("X-Resource-Size"), ResourceSHA256: r.Header.Get("X-Resource-SHA256")})
	if err != nil {
		return nil, err
	}
	headers := http.Header{"Content-Type": []string{out.ContentType}}
	if out.ResourceName != "" {
		headers.Set("X-Resource-Name", out.ResourceName)
		headers.Set("X-Resource-Size", out.ResourceSize)
		headers.Set("X-Resource-SHA256", out.ResourceSHA256)
	}
	return &http.Response{StatusCode: out.Status, Status: http.StatusText(out.Status), Header: headers, Body: io.NopCloser(bytes.NewReader(out.Body)), ContentLength: int64(len(out.Body)), Request: r}, nil
}

// ProductProxyBearer is a native transport placeholder. It is never transmitted
// to the product endpoint; only the target's existing private bearer is used.
const ProductProxyBearer = "paired-native-managed-product-transport"

func CleanManagedProductContentType(v string) string {
	kind := strings.Split(v, ";")[0]
	if kind == "application/json" || kind == "application/octet-stream" {
		return kind
	}
	return ""
}
