package notebooksync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

type Status struct {
	NodeID      string `json:"nodeId"`
	LastSuccess string `json:"lastSuccess,omitempty"`
	LastAttempt string `json:"lastAttempt,omitempty"`
	Error       string `json:"error,omitempty"`
	Phase       string `json:"phase"`
}
type State struct {
	SourceNodeID string   `json:"sourceNodeId"`
	Targets      []Status `json:"targets"`
}

// Hooks reuse native ownership/idle/stop/start ports. All checks must consult
// the concrete owner; connectivity loss is never proof of stopped ownership.
type Hooks struct {
	SourceActive   func(context.Context) error
	StandbyStopped func(context.Context, string) error
	StopSource     func(context.Context) error
	SourceStopped  func(context.Context) error
	StartFresh     func(context.Context, string) error
	Transfer       func(context.Context, string, bool) error
	Save           func(State) error
}
type Controller struct {
	op     chan struct{}
	mu     sync.Mutex
	state  State
	hooks  Hooks
	closed bool
}

func New(state State, h Hooks) (*Controller, error) {
	if state.SourceNodeID == "" || len(state.Targets) == 0 || h.SourceActive == nil || h.StandbyStopped == nil || h.Transfer == nil {
		return nil, errors.New("Notebook sync requires a source, stopped backups, and native checks")
	}
	seen := map[string]bool{state.SourceNodeID: true}
	state.Targets = append([]Status(nil), state.Targets...)
	for i := range state.Targets {
		s := &state.Targets[i]
		if s.NodeID == "" || seen[s.NodeID] {
			return nil, errors.New("invalid Notebook backup target")
		}
		seen[s.NodeID] = true
		if s.Phase == "" {
			s.Phase = "ready"
		}
	}
	c := &Controller{state: state, hooks: h, op: make(chan struct{}, 1)}
	c.op <- struct{}{}
	return c, nil
}
func AttemptID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func (c *Controller) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.op:
		return nil
	}
}
func (c *Controller) release() { c.op <- struct{}{} }

func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.state
	s.Targets = append([]Status(nil), s.Targets...)
	return s
}
func (c *Controller) status(id string) (Status, error) {
	for _, s := range c.State().Targets {
		if s.NodeID == id {
			return s, nil
		}
	}
	return Status{}, errors.New("backup node is not configured")
}
func (c *Controller) store(s Status) error {
	c.mu.Lock()
	for i := range c.state.Targets {
		if c.state.Targets[i].NodeID == s.NodeID {
			c.state.Targets[i] = s
		}
	}
	c.mu.Unlock()
	if c.hooks.Save != nil {
		return c.hooks.Save(c.State())
	}
	return nil
}
func (c *Controller) available() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("Notebook sync is closed")
	}
	for _, s := range c.state.Targets {
		if s.Phase != "ready" {
			return errors.New("Notebook switch needs native recovery; the old source will not be reused")
		}
	}
	return nil
}
func (c *Controller) fail(s Status, err error) error {
	s.Error = err.Error()
	return errors.Join(err, c.store(s))
}
func (c *Controller) Sync(ctx context.Context, id string) error {
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	if err := c.available(); err != nil {
		return err
	}
	s, err := c.status(id)
	if err != nil {
		return err
	}
	s.LastAttempt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = c.hooks.SourceActive(ctx); err != nil {
		return c.fail(s, err)
	}
	if err = c.hooks.StandbyStopped(ctx, id); err != nil {
		return c.fail(s, err)
	}
	if err = c.hooks.Transfer(ctx, id, false); err != nil {
		return c.fail(s, err)
	}
	s.LastSuccess = time.Now().UTC().Format(time.RFC3339Nano)
	s.Error = ""
	return c.store(s)
}

// Switch never retries a stop/start with an unknown result. Persisting intent
// before stopping also prevents a reopened APP from backing up a stale source.
// On failure the existing native recovery path must establish the actual owner.
func (c *Controller) Switch(ctx context.Context, id string) error {
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	if err := c.available(); err != nil {
		return err
	}
	s, err := c.status(id)
	if err != nil {
		return err
	}
	h := c.hooks
	if h.StopSource == nil || h.SourceStopped == nil || h.StartFresh == nil || h.Save == nil {
		return errors.New("native switch ports and durable intent are required")
	}
	if err = h.SourceActive(ctx); err != nil {
		return c.fail(s, err)
	}
	if err = h.StandbyStopped(ctx, id); err != nil {
		return c.fail(s, err)
	}
	s.Phase = "stopping"
	s.LastAttempt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = c.store(s); err != nil {
		return err
	}
	if err = h.StopSource(ctx); err != nil {
		return c.fail(s, err)
	}
	if err = h.SourceStopped(ctx); err != nil {
		return c.fail(s, err)
	}
	s.Phase = "stopped"
	if err = c.store(s); err != nil {
		return err
	}
	if err = h.StandbyStopped(ctx, id); err != nil {
		return c.fail(s, err)
	}
	if err = h.Transfer(ctx, id, true); err != nil {
		return c.fail(s, err)
	}
	s.LastSuccess = time.Now().UTC().Format(time.RFC3339Nano)
	s.Error = ""
	s.Phase = "starting"
	if err = c.store(s); err != nil {
		return err
	}
	if err = h.SourceStopped(ctx); err != nil {
		return c.fail(s, err)
	}
	if err = h.StartFresh(ctx, id); err != nil {
		return c.fail(s, err)
	}
	s.Phase = "switched"
	return c.store(s)
}

// Run belongs to the current APP lifetime. It installs no daemon or OS timer.
func (c *Controller) Run(ctx context.Context, interval time.Duration) error {
	if interval < time.Minute {
		return errors.New("Notebook sync interval must be at least one minute")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for _, s := range c.State().Targets {
				_ = c.Sync(ctx, s.NodeID)
			}
		}
	}
}
func (c *Controller) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	_ = c.acquire(context.Background())
	c.release()
}
