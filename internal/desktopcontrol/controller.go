package desktopcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

type turnKey struct{}

func WithTurn(ctx context.Context, turn string) context.Context {
	return context.WithValue(ctx, turnKey{}, turn)
}

type client interface {
	BeginTurn(context.Context, string) error
	EndTurn(context.Context, string) error
	Grant(context.Context, string, dw.Ref) error
	Declare(context.Context, string, string, string) error
	Revoke(context.Context, string, dw.Ref) error
	RevokeGrant(context.Context, string, string) error
	Grants(context.Context, string) (host.GrantStatus, error)
	Call(context.Context, string, string, string, any) (host.Reply, error)
	Reconcile(context.Context, string, string) (host.Reply, error)
	Close()
}
type request struct {
	turn, op, body string
	done           chan struct{}
	reply          host.Reply
	err            error
}
type Controller struct {
	// Control never queues behind a native data Call.
	life       sync.Mutex
	mu         sync.Mutex
	start      func(context.Context) (client, dw.Epoch, error)
	client     client
	epoch      dw.Epoch
	turn       string
	ended      map[string]bool
	requests   map[string]*request
	apps       map[dw.Ref]string
	closed     bool
	assets     string
	ownsAssets bool
}

func New(executable, assets string) *Controller {
	c := &Controller{assets: assets, ended: map[string]bool{}, requests: map[string]*request{}, apps: map[dw.Ref]string{}}
	c.start = func(ctx context.Context) (client, dw.Epoch, error) {
		h, err := host.Start(ctx, host.Options{Executable: executable, AssetsDir: assets,
			InputMode: dw.InputModeCooperative, InputPolicy: dw.InputShared})
		if err != nil {
			return nil, "", err
		}
		return h, h.Hello.Environment.Epoch, nil
	}
	return c
}
func Bundled() *Controller {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	helper := filepath.Join(filepath.Dir(exe), "..", "Resources", "DesktopWorld", "bin", "dtw")
	if info, err := os.Stat(helper); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	assets, err := os.MkdirTemp("", "caelis-desktop-world-")
	if err != nil {
		return nil
	}
	c := New(helper, assets)
	c.ownsAssets = true
	return c
}
func (c *Controller) Definitions() []api.ToolDefinition { return Definitions() }
func (c *Controller) ensure(ctx context.Context, turn string, observe bool) (client, dw.Epoch, error) {
	c.life.Lock()
	defer c.life.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if turn == "" || c.closed || c.ended[turn] || ctx.Err() != nil {
		return nil, "", errors.New("desktop turn unavailable; wait for a new user request")
	}
	if c.client == nil {
		if !observe {
			return nil, "", errors.New("observe the desktop before requesting input")
		}
		h, epoch, err := c.start(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("desktop helper startup failed: %w", err)
		}
		c.client, c.epoch = h, epoch
	}
	if c.turn != turn {
		if c.turn != "" {
			return nil, "", errors.New("previous desktop turn has not ended")
		}
		if err := c.client.BeginTurn(ctx, turn); err != nil {
			return nil, "", err
		}
		c.turn = turn
	}
	return c.client, c.epoch, nil
}

// EndTurn uses an independent control deadline, including when idle or canceled.
func (c *Controller) EndTurn(turn string) {
	if turn == "" {
		return
	}
	c.life.Lock()
	defer c.life.Unlock()
	c.mu.Lock()
	c.ended[turn] = true
	h, active := c.client, c.turn == turn
	if active {
		c.turn = ""
		c.apps = map[dw.Ref]string{}
	}
	c.mu.Unlock()
	if h != nil && active {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = h.EndTurn(ctx, turn) // SDK closes if revocation cannot be confirmed.
	}
}

var requestID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func decode(raw []byte, out any) error {
	// SDK rejects duplicate keys/trailing JSON; outer Bot envelopes use camelCase.
	var unique map[string]any
	if err := protocol.Decode(raw, &unique); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func failure(code, message, id string) api.ToolResult {
	b, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": message}, "original_request_id": id})
	var v map[string]any
	_ = json.Unmarshal(b, &v)
	return api.ToolResult{IsError: true, Content: []map[string]string{{"type": "text", "text": string(b)}}, StructuredContent: v}
}
func content(reply host.Reply, id string) api.ToolResult {
	reply.ID = id
	p := host.Content(reply)
	var v map[string]any
	_ = json.Unmarshal(p.StructuredContent, &v)
	if e, ok := v["error"].(map[string]any); ok && e["code"] == "model_output_budget" {
		// The full original receipt stays in the host. Retain bounded step
		// delivery/verification facts so a partial plan is never mistaken for
		// a no-effect failure merely because its evidence exceeded the budget.
		var receipt dw.Receipt
		if protocol.Decode(reply.Result, &receipt) == nil && len(receipt.Steps) <= 16 {
			steps := make([]map[string]any, 0, len(receipt.Steps))
			for _, s := range receipt.Steps {
				if len(s.ID) > 128 || len(s.Target) > 512 || len(s.Channel) > 64 || len(s.State) > 64 || len(s.Delivery) > 64 || len(s.Verification) > 64 {
					steps = nil
					break
				}
				step := map[string]any{"id": s.ID, "target": s.Target, "channel": s.Channel, "state": s.State,
					"delivery": s.Delivery, "verification": s.Verification}
				if s.AcceptedInputEvents != nil {
					step["accepted_input_events"] = *s.AcceptedInputEvents
				}
				if s.RequestedInputEvents != nil {
					step["requested_input_events"] = *s.RequestedInputEvents
				}
				if s.Fault != nil && len(s.Fault.Code) <= 128 && len(s.Fault.RetryClass) <= 64 {
					step["fault"] = map[string]any{"code": s.Fault.Code, "retry_class": s.Fault.RetryClass}
				}
				steps = append(steps, step)
			}
			if steps != nil {
				v["steps"] = steps
				body, _ := json.Marshal(v)
				if 2*len(body)+1024 <= 32<<10 {
					p.Content[0].Text = string(body)
				} else {
					delete(v, "steps")
				}
			}
		}
	}
	out := api.ToolResult{IsError: p.IsError, StructuredContent: v}
	for _, b := range p.Content {
		out.Content = append(out.Content, map[string]string{"type": b.Type, "text": b.Text})
	}
	return out
}
func (c *Controller) CallTool(ctx context.Context, name string, raw json.RawMessage) api.ToolResult {
	op := strings.TrimPrefix(name, Prefix)
	if op == "authorize" && name == Prefix+op {
		return c.authorize(ctx, raw)
	}
	var in struct {
		RequestID string          `json:"requestId"`
		Args      json.RawMessage `json:"args,omitempty"`
	}
	if decode(raw, &in) != nil || !requestID.MatchString(in.RequestID) {
		return failure("invalid_request", "Supply a stable requestId and documented arguments.", "")
	}
	if op == "reconcile" && name == Prefix+op {
		if len(in.Args) != 0 {
			return failure("invalid_request", "Reconcile accepts only requestId.", in.RequestID)
		}
		return c.reconcile(ctx, in.RequestID)
	}
	if name != Prefix+op || !strings.Contains("|observe|read|sync|act|capture|get|cancel|", "|"+op+"|") {
		return failure("invalid_request", "Unsupported Desktop World operation.", in.RequestID)
	}
	var args map[string]any
	if decode(in.Args, &args) != nil || args == nil {
		return failure("invalid_request", "args must be an object.", in.RequestID)
	}
	body, _ := json.Marshal(args)
	turn, _ := ctx.Value(turnKey{}).(string)
	c.mu.Lock()
	previous, exists := c.requests[in.RequestID]
	c.mu.Unlock()
	if exists {
		if previous.op != op || previous.body != string(body) {
			return failure("request_conflict", "This requestId belongs to different arguments.", in.RequestID)
		}
		return c.reconcile(ctx, in.RequestID)
	}
	if op == "act" {
		if err := cooperativePlan(in.Args); err != nil {
			out := failure("invalid_request", err.Error(), in.RequestID)
			out.StructuredContent["outcome"] = "rejected"
			b, _ := json.Marshal(out.StructuredContent)
			out.Content[0]["text"] = string(b)
			return out
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	h, _, err := c.ensure(ctx, turn, op == "observe")
	if err != nil {
		return failure("desktop_unavailable", err.Error(), in.RequestID)
	}
	if op == "observe" {
		if _, ok := args["fields"]; !ok {
			args["fields"] = []string{"name", "role", "app", "window"}
		}
		if _, ok := args["budget"]; !ok {
			args["budget"] = map[string]any{"max_results": 32, "max_output_bytes": 8192}
		}
	}
	if op == "sync" {
		if _, ok := args["max_output_bytes"]; !ok {
			args["max_output_bytes"] = 8192
		}
	}
	if op == "capture" {
		for _, key := range []string{"max_pixel_width", "max_pixel_height"} {
			v, ok := args[key].(float64)
			if !ok || v == 0 {
				args[key] = 1000
			} else if v > 1000 {
				return failure("invalid_request", "Capture dimensions must be at most 1000 pixels.", in.RequestID)
			}
		}
	}
	if op == "act" {
		if _, ok := args["epoch"]; ok {
			return failure("invalid_request", "The host supplies epoch.", in.RequestID)
		}
		if _, ok := args["request_id"]; ok {
			return failure("invalid_request", "Use outer requestId; host supplies plan identity.", in.RequestID)
		}
		// The managed helper supplies epoch and request_id from its stable
		// envelope (turn + SDK wire ID); supplying them here is rejected.
	}
	c.mu.Lock()
	if c.ended[turn] || c.closed {
		c.mu.Unlock()
		return failure("turn_ended", "Desktop turn ended before dispatch.", in.RequestID)
	}
	if len(c.requests) >= 4096 {
		c.mu.Unlock()
		return failure("session_limit", "Desktop session request limit reached; no automatic restart.", in.RequestID)
	}
	if previous, exists = c.requests[in.RequestID]; exists {
		c.mu.Unlock()
		if previous.op != op || previous.body != string(body) {
			return failure("request_conflict", "This requestId belongs to different arguments.", in.RequestID)
		}
		return c.reconcile(ctx, in.RequestID)
	}
	r := &request{turn: turn, op: op, body: string(body), done: make(chan struct{})}
	c.requests[in.RequestID] = r
	c.mu.Unlock()
	r.reply, r.err = h.Call(ctx, turn, in.RequestID, op, args)
	close(r.done)
	if r.err != nil {
		c.EndTurn(turn)
		return failure("desktop_result_unknown", "Original request retained; turn revoked. Reconcile same requestId; never replay with a new ID. "+r.err.Error(), in.RequestID)
	}
	c.rememberApps(turn, op, r.reply)
	return c.project(r.reply, in.RequestID, op)
}
func (c *Controller) reconcile(ctx context.Context, id string) api.ToolResult {
	c.mu.Lock()
	r, ok := c.requests[id]
	h := c.client
	c.mu.Unlock()
	if !ok || h == nil {
		return failure("receipt_unavailable", "Original request not known to this host; no replay. Missing history does not prove no effect.", id)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case <-r.done:
	case <-ctx.Done():
		return failure("receipt_pending", "Original request still pending; no replay.", id)
	}
	reply, err := r.reply, r.err
	if err != nil {
		reply, err = h.Reconcile(ctx, r.turn, id)
	}
	if err != nil {
		return failure("receipt_unavailable", "Original result unconfirmed; no replay. "+err.Error(), id)
	}
	c.rememberApps(r.turn, r.op, reply)
	return c.project(reply, id, "reconcile")
}
func (c *Controller) rememberApps(turn, op string, reply host.Reply) {
	var objects []dw.Object
	if op == "observe" {
		var o dw.Observation
		if protocol.Decode(reply.Result, &o) == nil {
			objects = o.Objects
		}
	}
	if op == "sync" {
		var s dw.ChangeSet
		if protocol.Decode(reply.Result, &s) == nil {
			objects = s.Upserts
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.turn != turn || c.ended[turn] {
		return
	}
	for _, o := range objects {
		if o.Kind == dw.KindApplication && o.Lifecycle == dw.LifeLive && o.Name.Status == dw.FactKnown && o.Name.Value != nil {
			c.apps[o.Ref] = *o.Name.Value
		}
	}
}
func (c *Controller) authorize(ctx context.Context, raw json.RawMessage) api.ToolResult {
	var in struct {
		Application dw.Ref `json:"application"`
		Name        string `json:"name"`
		Purpose     string `json:"purpose"`
		Operation   string `json:"operation"`
		WindowTitle string `json:"windowTitle"`
		GrantID     string `json:"grantId"`
	}
	if decode(raw, &in) != nil || len(in.Name) > 300 || len(in.WindowTitle) > 512 || strings.TrimSpace(in.Purpose) == "" || len(in.Purpose) > 2000 {
		return failure("invalid_request", "Use one exact grant, declaration or revocation with a task purpose.", "")
	}
	if in.Operation == "grant" {
		return failure("invalid_request", "Use the exact observed Ref/name grant form without an operation field.", "")
	}
	if in.Operation == "" {
		in.Operation = "grant"
	}
	switch in.Operation {
	case "grant":
		if in.Application == "" || in.Name == "" || in.WindowTitle != "" || in.GrantID != "" {
			return failure("invalid_request", "Grant requires exact observed application Ref and name.", "")
		}
	case "declare":
		if in.Application != "" || in.GrantID != "" || (in.Name == "") == (in.WindowTitle == "") {
			return failure("invalid_request", "Declare requires one exact app name or window title.", "")
		}
	case "revoke":
		if in.Name != "" || in.WindowTitle != "" || (in.Application == "") == (in.GrantID == "") {
			return failure("invalid_request", "Revoke requires one application Ref or returned grant ID.", "")
		}
	default:
		return failure("invalid_request", "Unsupported authorization operation.", "")
	}
	turn, _ := ctx.Value(turnKey{}).(string)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	h, _, err := c.ensure(ctx, turn, false)
	if err != nil {
		return failure("desktop_unavailable", err.Error(), "")
	}
	var controlErr error
	switch in.Operation {
	case "grant":
		c.mu.Lock()
		observed := c.apps[in.Application]
		c.mu.Unlock()
		if observed != in.Name {
			return failure("application_not_observed", "Approval must match application Ref and name observed in this turn.", "")
		}
		controlErr = h.Grant(ctx, turn, in.Application)
	case "declare":
		controlErr = h.Declare(ctx, turn, in.Name, in.WindowTitle)
	case "revoke":
		if in.GrantID != "" {
			controlErr = h.RevokeGrant(ctx, turn, in.GrantID)
		} else {
			controlErr = h.Revoke(ctx, turn, in.Application)
		}
	}
	if controlErr != nil {
		return failure("grant_refused", controlErr.Error(), "")
	}
	// A declaration can remain pending, ambiguous or unresolved. Never call it
	// authorized merely because the helper accepted the selector.
	status, err := h.Grants(ctx, turn)
	if err != nil {
		return failure("grant_status_unknown", "Control mutation may have taken effect; inspect grants in this turn. "+err.Error(), "")
	}
	b, _ := protocol.Marshal(status)
	return content(host.Reply{Result: b}, "")
}

// GrantStatus is read-only and never starts a helper or creates a turn.
func (c *Controller) GrantStatus(ctx context.Context) api.ToolResult {
	turn, _ := ctx.Value(turnKey{}).(string)
	c.mu.Lock()
	h, active := c.client, c.turn == turn && !c.ended[turn] && !c.closed
	c.mu.Unlock()
	if !active || h == nil || ctx.Err() != nil {
		return failure("desktop_unavailable", "No active desktop turn for grant status.", "")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	status, err := h.Grants(ctx, turn)
	if err != nil {
		return failure("grant_status_unknown", err.Error(), "")
	}
	b, _ := protocol.Marshal(status)
	return content(host.Reply{Result: b}, "")
}
func (c *Controller) Close() {
	c.life.Lock()
	defer c.life.Unlock()
	c.mu.Lock()
	c.closed = true
	h := c.client
	c.mu.Unlock()
	if h != nil {
		h.Close()
	}
	// Assets is a host-selected disposable directory, never a model path.
	if c.ownsAssets && c.assets != "" {
		_ = os.RemoveAll(c.assets)
	}
}
