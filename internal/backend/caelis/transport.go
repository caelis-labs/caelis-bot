// Package caelis projects the versioned public Caelis Control HTTP/SSE API.
package caelis

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

type client struct {
	origin, token string
	http          *http.Client
}
type remoteError struct {
	Status       int
	Code         string
	path, detail string
}

func (e *remoteError) Error() string {
	return fmt.Sprintf("Caelis 请求未完成（HTTP %d，%s）", e.Status, e.Code)
}
func newClient(origin, token string) (*client, error) {
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("仅支持本机 Caelis Control 服务")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || token == "" {
		return nil, errors.New("Caelis 本机地址或认证无效")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.ResponseHeaderTimeout = 20 * time.Second
	return &client{origin: strings.TrimRight(origin, "/"), token: token, http: &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *client) request(ctx context.Context, method, path string, body any, op, revision string, lastEvent ...string) (*http.Response, error) {
	return c.requestMedia(ctx, method, path, body, op, revision, "", lastEvent...)
}
func (c *client) requestMedia(ctx context.Context, method, path string, body any, op, revision, accept string, lastEvent ...string) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		r = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.origin+"/api/control/v1"+path, r)
	if e != nil {
		return nil, e
	}
	// net/http treats Idempotency-Key as replayable on a reused connection.
	// The durable Control journal, not transport retry, owns mutation recovery.
	if method != "GET" {
		req.GetBody = nil
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if len(lastEvent) > 0 && lastEvent[0] != "" {
		req.Header.Set("Last-Event-ID", lastEvent[0])
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if op != "" {
		req.Header.Set("Idempotency-Key", op)
	}
	if revision != "" {
		req.Header.Set("If-Match", `"`+revision+`"`)
	}
	res, e := c.http.Do(req)
	if e != nil {
		return nil, errors.New("Caelis 连接中断，操作结果需要核对")
	}
	return res, nil
}
func (c *client) json(ctx context.Context, method, path string, body, out any, op, rev string) error {
	return c.jsonTimeout(ctx, method, path, body, out, op, rev, 30*time.Second)
}
func (c *client) jsonTimeout(ctx context.Context, method, path string, body, out any, op, rev string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, e := c.request(ctx, method, path, body, op, rev)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 64<<20+1))
	if e != nil || len(b) > 64<<20 {
		return errors.New("Caelis 响应不完整或超过限制")
	}
	// Typed outcomes remain authoritative even on HTTP 400/409/202.
	if op != "" {
		if target, ok := out.(*wire.CommandResult); ok {
			var result wire.CommandResult
			if json.Unmarshal(b, &result) == nil && result.OperationId == op && result.Outcome != "" {
				*target = result
				return nil
			}
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var err struct {
			Code    string `json:"code"`
			Message string `json:"error"`
		}
		_ = json.Unmarshal(b, &err)
		if len(err.Code) > 80 {
			err.Code = "request_failed"
		}
		return &remoteError{Status: res.StatusCode, Code: err.Code, path: path, detail: err.Message}
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(b, out) != nil {
		return errors.New("Caelis 响应格式不兼容")
	}
	return nil
}

type frame struct {
	event, id string
	data      []byte
}

func readFrame(scan *bufio.Scanner) (frame, error) {
	var f frame
	for scan.Scan() {
		l := scan.Text()
		if l == "" {
			if len(f.data) > 0 {
				return f, nil
			}
			continue
		}
		if strings.HasPrefix(l, "event:") {
			f.event = strings.TrimSpace(l[6:])
		}
		if strings.HasPrefix(l, "id:") {
			f.id = strings.TrimSpace(l[3:])
		}
		if strings.HasPrefix(l, "data:") {
			f.data = append(f.data, strings.TrimSpace(l[5:])...)
			f.data = append(f.data, '\n')
			if len(f.data) > 8<<20 {
				return f, errors.New("事件超过限制")
			}
		}
	}
	if scan.Err() != nil {
		return f, scan.Err()
	}
	return f, io.EOF
}
func (c *client) stream(ctx context.Context, path, cursor string, apply func(frame) error) error {
	res, e := c.request(ctx, "GET", path, nil, "", "", cursor)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	kind, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if res.StatusCode != 200 || kind != "text/event-stream" {
		return errors.New("Caelis 状态订阅不可用")
	}
	scan := bufio.NewScanner(res.Body)
	scan.Buffer(make([]byte, 65536), 8<<20)
	for {
		f, e := readFrame(scan)
		if e != nil {
			return e
		}
		if e = apply(f); e != nil {
			return e
		}
	}
}
func idPath(id string) string { return url.PathEscape(id) }
