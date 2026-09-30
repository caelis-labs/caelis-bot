package productrpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func resourceHeaders(r *http.Request) (Scope, Resource, error) {
	scope := Scope{BotID: r.Header.Get("X-Product-Bot"), Generation: r.Header.Get("X-Product-Generation")}
	name, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Resource-Name"))
	if err != nil {
		return scope, Resource{}, err
	}
	size, err := strconv.ParseInt(r.Header.Get("X-Resource-Size"), 10, 64)
	hash := r.Header.Get("X-Resource-SHA256")
	if err != nil || size < 0 || size > MaxResourceBytes || len(name) == 0 || len(name) > 256 || strings.ContainsAny(string(name), "/\\\x00\r\n") || len(hash) != 64 {
		return scope, Resource{}, errors.New("invalid resource metadata")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return scope, Resource{}, err
	}
	return scope, Resource{Name: string(name), Size: size, SHA256: hash}, nil
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	scope, meta, err := resourceHeaders(r)
	if err != nil {
		problem(w, 400, "invalid-resource")
		return
	}
	if !s.scope(w, scope) {
		return
	}
	if !s.identity.Capabilities.Files || s.opts.Resources == nil {
		problem(w, 409, "resources-unavailable")
		return
	}
	if r.Header.Get("Content-Type") != "application/octet-stream" {
		problem(w, 415, "bytes-required")
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxResourceBytes))
	hash := sha256.Sum256(b)
	if err != nil || int64(len(b)) != meta.Size || hex.EncodeToString(hash[:]) != meta.SHA256 {
		problem(w, 400, "resource-integrity-failed")
		return
	}
	s.commands.Lock()
	defer s.commands.Unlock()
	if s.stopping {
		problem(w, 409, "bot-stopping")
		return
	}
	result, err := s.opts.Resources.Upload(r.Context(), meta, bytes.NewReader(b))
	if err != nil || !identifier.MatchString(result.ID) || result.Size != meta.Size || result.SHA256 != meta.SHA256 || result.Name != meta.Name {
		problem(w, 409, "resource-unavailable")
		return
	}
	s.write(w, result)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	scope := Scope{BotID: r.Header.Get("X-Product-Bot"), Generation: r.Header.Get("X-Product-Generation")}
	if !s.scope(w, scope) {
		return
	}
	if !s.identity.Capabilities.Files || s.opts.Resources == nil {
		problem(w, 409, "resources-unavailable")
		return
	}
	id := r.URL.Query().Get("id")
	if !identifier.MatchString(id) {
		problem(w, 400, "invalid-resource-id")
		return
	}
	// The composed Service owns the artifact catalog; no client-chosen path or
	// arbitrary native binding can select a resource outside that catalog.
	for _, item := range s.port.Snapshot().Items {
		for _, artifact := range item.Artifacts {
			if s.projection.handle("resource", artifact.ID) == id {
				id = artifact.ID
			}
		}
	}
	meta, reader, err := s.opts.Resources.Open(r.Context(), id)
	if err != nil || reader == nil {
		problem(w, 404, "resource-unavailable")
		return
	}
	defer reader.Close()
	if meta.Size < 0 || meta.Size > MaxResourceBytes {
		problem(w, 413, "resource-limit")
		return
	}
	b, err := io.ReadAll(io.LimitReader(reader, MaxResourceBytes+1))
	hash := sha256.Sum256(b)
	if err != nil || int64(len(b)) != meta.Size || hex.EncodeToString(hash[:]) != meta.SHA256 {
		problem(w, 409, "resource-integrity-failed")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Resource-Name", base64.RawURLEncoding.EncodeToString([]byte(meta.Name)))
	w.Header().Set("X-Resource-Size", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("X-Resource-SHA256", meta.SHA256)
	_, _ = w.Write(b)
}

func (c *Client) Upload(ctx context.Context, name string, b []byte) (Resource, error) {
	ctx, cancel := c.requestContext(ctx)
	defer cancel()
	scope, err := c.scope()
	if err != nil {
		return Resource{}, err
	}
	if len(b) > MaxResourceBytes || len(name) == 0 || len(name) > 256 || strings.ContainsAny(name, "/\\\x00\r\n") {
		return Resource{}, errors.New("invalid resource upload")
	}
	hash := sha256.Sum256(b)
	meta := Resource{Name: name, Size: int64(len(b)), SHA256: hex.EncodeToString(hash[:])}
	r, err := http.NewRequestWithContext(ctx, "PUT", c.options.URL+"/v1/resources", bytes.NewReader(b))
	if err != nil {
		return Resource{}, err
	}
	r.Header.Set("Authorization", "Bearer "+c.options.Token)
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("X-Product-Bot", scope.BotID)
	r.Header.Set("X-Product-Generation", scope.Generation)
	r.Header.Set("X-Resource-Name", base64.RawURLEncoding.EncodeToString([]byte(name)))
	r.Header.Set("X-Resource-Size", strconv.FormatInt(meta.Size, 10))
	r.Header.Set("X-Resource-SHA256", meta.SHA256)
	response, err := c.http.Do(r)
	if err != nil {
		return Resource{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Resource{}, &ProtocolError{Status: response.StatusCode, Code: "resource-upload-failed"}
	}
	var result Resource
	if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) != nil || result.Size != meta.Size || result.SHA256 != meta.SHA256 || result.Name != name || !identifier.MatchString(result.ID) {
		return Resource{}, errors.New("invalid resource receipt")
	}
	return result, nil
}

func (c *Client) Download(ctx context.Context, id string) (Resource, []byte, error) {
	ctx, cancel := c.requestContext(ctx)
	defer cancel()
	scope, err := c.scope()
	if err != nil {
		return Resource{}, nil, err
	}
	if !identifier.MatchString(id) {
		return Resource{}, nil, errors.New("invalid resource id")
	}
	r, err := http.NewRequestWithContext(ctx, "GET", c.options.URL+"/v1/resources?id="+id, nil)
	if err != nil {
		return Resource{}, nil, err
	}
	r.Header.Set("Authorization", "Bearer "+c.options.Token)
	r.Header.Set("X-Product-Bot", scope.BotID)
	r.Header.Set("X-Product-Generation", scope.Generation)
	response, err := c.http.Do(r)
	if err != nil {
		return Resource{}, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Resource{}, nil, &ProtocolError{Status: response.StatusCode, Code: "resource-download-failed"}
	}
	_, meta, err := resourceHeaders(&http.Request{Header: response.Header})
	if err != nil {
		return Resource{}, nil, err
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, MaxResourceBytes+1))
	hash := sha256.Sum256(b)
	if err != nil || int64(len(b)) != meta.Size || hex.EncodeToString(hash[:]) != meta.SHA256 {
		return Resource{}, nil, errors.New("resource integrity failed")
	}
	meta.ID = id
	return meta, b, nil
}
