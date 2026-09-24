package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}
type client struct {
	ws      *websocket.Conn
	mu      sync.Mutex
	next    int
	pending map[string]chan message
	counts  map[string]int
	events  chan message
	done    chan struct{}
	backlog []message
}

func dial(ctx context.Context, socket string) (*client, error) {
	ht := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	ws, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: &http.Client{Transport: ht}})
	ht.CloseIdleConnections()
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(8 << 20)
	c := &client{ws: ws, pending: map[string]chan message{}, counts: map[string]int{}, events: make(chan message, 2048), done: make(chan struct{})}
	go c.read(ctx)
	_, err = c.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "caelis_event_poc", "version": "0.1"}, "capabilities": map[string]any{"experimentalApi": true}})
	if err == nil {
		err = c.write(ctx, map[string]any{"method": "initialized"})
	}
	if err != nil {
		ws.CloseNow()
		return nil, err
	}
	return c, nil
}
func (c *client) write(ctx context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.ws.Write(ctx, websocket.MessageText, b)
}
func (c *client) read(ctx context.Context) {
	defer close(c.done)
	for {
		_, b, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		var m message
		if json.Unmarshal(b, &m) != nil {
			return
		}
		if len(m.ID) > 0 && m.Method != "" {
			// No autonomous approval authority in this synthetic-provider probe.
			if c.write(ctx, map[string]any{"id": m.ID, "error": map[string]any{"code": -32601, "message": "POC does not approve requests"}}) != nil {
				return
			}
			continue
		}
		if len(m.ID) > 0 {
			c.mu.Lock()
			ch := c.pending[string(m.ID)]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		select {
		case c.events <- m:
		default:
			return
		} // overflow fails the probe; no lost lifecycle claims
	}
}
func (c *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	key := fmt.Sprint(id)
	ch := make(chan message, 1)
	c.pending[key] = ch
	c.counts[method]++
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, key); c.mu.Unlock() }()
	if err := c.write(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		if len(m.Error) > 0 {
			return nil, fmt.Errorf("%s: %s", method, m.Error)
		}
		return m.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("%s: connection closed", method)
	}
}
func (c *client) wait(ctx context.Context, method, thread string) (message, error) {
	match := func(m message) bool {
		var p struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		return m.Method == method && (thread == "" || p.ThreadID == thread)
	}
	for i, m := range c.backlog {
		if match(m) {
			c.backlog = append(c.backlog[:i], c.backlog[i+1:]...)
			return m, nil
		}
	}
	for {
		select {
		case m := <-c.events:
			if match(m) {
				return m, nil
			}
			if len(c.backlog) >= 4096 {
				return message{}, fmt.Errorf("event backlog bound reached")
			}
			c.backlog = append(c.backlog, m)
		case <-ctx.Done():
			return message{}, ctx.Err()
		case <-c.done:
			return message{}, fmt.Errorf("event connection closed")
		}
	}
}
func turnID(m message) (string, string) {
	var p struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(m.Params, &p)
	return p.Turn.ID, p.Turn.Status
}
func (c *client) thread(ctx context.Context, dir string) (string, error) {
	b, err := c.call(ctx, "thread/start", map[string]any{"cwd": dir, "model": "poc", "modelProvider": "poc", "sandbox": "read-only", "approvalPolicy": "never", "ephemeral": false})
	if err != nil {
		return "", err
	}
	var r struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	err = json.Unmarshal(b, &r)
	if err == nil && r.Thread.ID == "" {
		err = fmt.Errorf("missing thread")
	}
	return r.Thread.ID, err
}
func (c *client) prompt(ctx context.Context, thread, text string) error {
	_, err := c.call(ctx, "turn/start", map[string]any{"threadId": thread, "input": []any{map[string]any{"type": "text", "text": text}}, "turnTrigger": "caelis_event_poc"})
	return err
}
