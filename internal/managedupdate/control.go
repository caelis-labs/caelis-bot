package managedupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"
)

func (m *Manager) handler() http.Handler {
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, value any, e error) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if e != nil {
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": Code(e)})
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}
	decode := func(w http.ResponseWriter, r *http.Request, out any) error {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if e := d.Decode(out); e != nil {
			return problem("invalid_request")
		}
		if d.Decode(&struct{}{}) != io.EOF {
			return problem("invalid_request")
		}
		return nil
	}
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) { write(w, m.Status(), nil) })
	mux.HandleFunc("GET /receipt/{id}", func(w http.ResponseWriter, r *http.Request) { v, e := m.Receipt(r.PathValue("id")); write(w, v, e) })
	mux.HandleFunc("POST /command", func(w http.ResponseWriter, r *http.Request) {
		var c Command
		if e := decode(w, r, &c); e != nil {
			write(w, nil, e)
			return
		}
		v, e := m.Command(c)
		write(w, v, e)
	})
	// Same-user Unix socket only. This is deliberately absent from the Web
	// allowlist: browsers cannot supply archive paths or signing material.
	mux.HandleFunc("POST /local-upgrade", func(w http.ResponseWriter, r *http.Request) {
		var in localUpgrade
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || d.Decode(&struct{}{}) != io.EOF {
			write(w, nil, problem("invalid_request"))
			return
		}
		write(w, struct{}{}, m.scheduleLocal(in))
	})
	mux.HandleFunc("GET /local-upgrade/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !hexID.MatchString(r.PathValue("id")) {
			write(w, nil, problem("invalid_request"))
			return
		}
		write(w, m.localUpgradeState(r.PathValue("id")), nil)
	})
	mux.HandleFunc("POST /ready", func(w http.ResponseWriter, r *http.Request) {
		var b Boot
		if e := decode(w, r, &b); e != nil {
			write(w, nil, e)
			return
		}
		write(w, struct{}{}, m.Ready(b))
	})
	mux.HandleFunc("POST /handoff", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			BootID string `json:"bootId"`
		}
		if e := decode(w, r, &b); e != nil {
			write(w, nil, e)
			return
		}
		v, e := m.Handoff(b.BootID)
		write(w, v, e)
	})
	mux.HandleFunc("POST /finish", func(w http.ResponseWriter, r *http.Request) {
		var h Handoff
		if e := decode(w, r, &h); e != nil {
			write(w, nil, e)
			return
		}
		write(w, struct{}{}, m.FinishHandoff(h))
	})
	return mux
}

type Client struct{ http *http.Client }

func NewClient(root string) (*Client, error) {
	if e := privateDirectory(root); e != nil {
		return nil, e
	}
	socket := filepath.Join(root, "control.sock")
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &Client{http: &http.Client{Transport: tr, Timeout: 8 * time.Second}}, nil
}
func (c *Client) call(method, path string, in, out any) error {
	var b []byte
	if in != nil {
		b, _ = json.Marshal(in)
	}
	req, e := http.NewRequest(method, "http://managed"+path, bytes.NewReader(b))
	if e != nil {
		return e
	}
	r, e := c.http.Do(req)
	if e != nil {
		return problem("launcher_unavailable")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		var p struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&p)
		if p.Error == "" {
			p.Error = "launcher_unavailable"
		}
		return problem(p.Error)
	}
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(out)
}
func (c *Client) Status() Status {
	var s Status
	if c.call("GET", "/status", nil, &s) != nil {
		return Status{Protocol: Protocol, Mode: "managed", State: "unknown", Reason: "launcher_unavailable"}
	}
	return s
}
func (c *Client) Command(cmd Command) (Receipt, error) {
	var r Receipt
	e := c.call("POST", "/command", cmd, &r)
	return r, e
}
func (c *Client) Receipt(id string) (Receipt, error) {
	if !ingressID.MatchString(id) {
		return Receipt{}, problem("request_id_required")
	}
	var r Receipt
	e := c.call("GET", "/receipt/"+id, nil, &r)
	return r, e
}
func (c *Client) Ready(b Boot) error { return c.call("POST", "/ready", b, &struct{}{}) }
func (c *Client) Handoff(boot string) (Handoff, error) {
	var h Handoff
	e := c.call("POST", "/handoff", map[string]string{"bootId": boot}, &h)
	return h, e
}
func (c *Client) Finish(h Handoff) error { return c.call("POST", "/finish", h, &struct{}{}) }
