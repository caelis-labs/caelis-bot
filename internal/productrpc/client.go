package productrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
)

// ClientOptions is native-only pairing configuration. URL is the local endpoint
// of an explicitly configured tunnel, not a server address supplied by a model.
type ClientOptions struct {
	URL, ExpectedNode, ExpectedBot string
	Token                          string `json:"-"`
	HTTP                           *http.Client
}

type Client struct {
	options  ClientOptions
	http     *http.Client
	mu       sync.Mutex
	identity Identity
	life     context.Context
	detach   context.CancelFunc
}

type ProtocolError struct {
	Status int
	Code   string
}

func (e *ProtocolError) Error() string { return "product connection: " + e.Code }

func NewClient(opts ClientOptions) (*Client, error) {
	u, err := url.Parse(opts.URL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !net.ParseIP(u.Hostname()).IsLoopback() || !identifier.MatchString(opts.ExpectedNode) || !identifier.MatchString(opts.ExpectedBot) || len(opts.Token) < 32 || len(opts.Token) > 256 {
		return nil, errors.New("invalid native product pairing")
	}
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Transport: &http.Transport{Proxy: nil}}
	}
	// Native endpoints must never redirect a bearer to another service.
	copyClient := *opts.HTTP
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	opts.URL = u.Scheme + "://" + u.Host
	c := &Client{options: opts, http: &copyClient}
	c.life, c.detach = context.WithCancel(context.Background())
	return c, nil
}

func (c *Client) Connect(ctx context.Context) (Identity, error) {
	var identity Identity
	if err := c.request(ctx, "GET", "/v1/identity", nil, &identity); err != nil {
		return Identity{}, err
	}
	if identity.Version != ProtocolVersion || identity.NodeID != c.options.ExpectedNode || identity.BotID != c.options.ExpectedBot || identity.Generation == "" {
		return Identity{}, errors.New("product identity mismatch")
	}
	c.mu.Lock()
	c.identity = identity
	c.mu.Unlock()
	return identity, nil
}

func (c *Client) scope() (Scope, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.identity.Generation == "" {
		return Scope{}, errors.New("product connection has not been inspected")
	}
	return c.identity.Scope, nil
}

func (c *Client) State(ctx context.Context) (State, error) {
	scope, err := c.scope()
	if err != nil {
		return State{}, err
	}
	var state State
	err = c.request(ctx, "POST", "/v1/state", scope, &state)
	if err == nil && state.Scope != scope {
		err = errors.New("product state identity mismatch")
	}
	return state, err
}

func (c *Client) Watch(ctx context.Context, cursor Cursor) (State, error) {
	scope, err := c.scope()
	if err != nil {
		return State{}, err
	}
	var state State
	err = c.request(ctx, "POST", "/v1/watch", struct {
		Scope
		Cursor Cursor `json:"cursor"`
	}{scope, cursor}, &state)
	if err == nil && state.Scope != scope {
		err = errors.New("product state identity mismatch")
	}
	return state, err
}

// Command never retries. A lost response returns unknown with the original ID;
// callers reconnect and Receipt that ID instead of sending a replacement command.
func (c *Client) Command(ctx context.Context, command Command) (Result, error) {
	scope, err := c.scope()
	if err != nil {
		return Result{}, err
	}
	command.Scope = scope
	result := Result{ID: command.ID, Outcome: "unknown", Code: "response-unobserved"}
	err = c.request(ctx, "POST", "/v1/commands", command, &result)
	if err == nil && result.ID != command.ID {
		err = errors.New("product receipt identity mismatch")
		result = Result{ID: command.ID, Outcome: "unknown", Code: "invalid-receipt"}
	}
	return result, err
}

func (c *Client) Receipt(ctx context.Context, id string) (Result, error) {
	scope, err := c.scope()
	if err != nil {
		return Result{}, err
	}
	result := Result{ID: id, Outcome: "unknown", Code: "response-unobserved"}
	err = c.request(ctx, "POST", "/v1/receipt", struct {
		Scope
		ID string `json:"id"`
	}{scope, id}, &result)
	if err == nil && result.ID != id {
		err = errors.New("product receipt identity mismatch")
		result = Result{ID: id, Outcome: "unknown", Code: "invalid-receipt"}
	}
	return result, err
}

// Close detaches this client's observation. It sends no product mutation.
func (c *Client) Close() { c.detach(); c.http.CloseIdleConnections() }

func (c *Client) requestContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(c.life, cancel)
	if c.life.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	ctx, cancel := c.requestContext(ctx)
	defer cancel()
	if c.life.Err() != nil {
		return c.life.Err()
	}
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if len(b) > MaxCommandBytes {
			return errors.New("product command exceeds limit")
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequestWithContext(ctx, method, c.options.URL+path, body)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+c.options.Token)
	if input != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, MaxSnapshotBytes+1))
	if err != nil {
		return err
	}
	if len(b) > MaxSnapshotBytes {
		return errors.New("product response exceeds limit")
	}
	if response.StatusCode != http.StatusOK {
		var problem struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(b, &problem) != nil || problem.Code == "" {
			problem.Code = "invalid-response"
		}
		return &ProtocolError{Status: response.StatusCode, Code: problem.Code}
	}
	if json.Unmarshal(b, output) != nil {
		return errors.New("invalid product response")
	}
	return nil
}
