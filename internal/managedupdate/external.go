package managedupdate

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"
)

// External keeps the update surface visible for unmanaged/image installations.
// Checking is read-only and uses exactly the production release source. It
// cannot download, choose a destination, install, or restart anything.
type External struct {
	mu     sync.Mutex
	status Status
	ctx    context.Context
	cancel context.CancelFunc
	busy   bool
}

func Containerized() bool {
	if os.Getenv("container") != "" {
		return true
	}
	for _, name := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(name); err == nil {
			return true
		}
	}
	return false
}

func NewExternal(current Identity) *External {
	ctx, cancel := context.WithCancel(context.Background())
	mode, reason := "external", "external_installation"
	if Containerized() {
		mode, reason = "image", "image_managed"
	}
	return &External{ctx: ctx, cancel: cancel, status: Status{Protocol: Protocol, Mode: mode,
		State: "idle", Reason: reason, Current: current}}
}
func (e *External) Close()                          { e.cancel() }
func (e *External) Status() Status                  { e.mu.Lock(); defer e.mu.Unlock(); return e.status }
func (e *External) Receipt(string) (Receipt, error) { return Receipt{}, problem("read_only_check") }
func (e *External) Command(c Command) (Receipt, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !ingressID.MatchString(c.ID) {
		return Receipt{}, problem("request_id_required")
	}
	if c.Action != "check" {
		return Receipt{}, problem(e.status.Reason)
	}
	if !e.busy {
		e.busy = true
		e.status.State = "checking"
		e.status.Revision++
		current := e.status.Current.Version
		go func() {
			ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
			defer cancel()
			o, err := ProductionTrust().Check(ctx, current, "linux-"+runtime.GOARCH)
			e.mu.Lock()
			defer e.mu.Unlock()
			e.busy = false
			e.status.State = "no_compatible_update"
			e.status.Latest = ""
			if err != nil {
				e.status.State = "check_failed"
			} else if o != nil {
				e.status.State = "available"
				e.status.Latest = o.Manifest.Version
			}
			e.status.Revision++
		}()
	}
	// Repeating a check is safe; this is explicitly not a durable install receipt.
	return Receipt{ID: c.ID, State: "read_only"}, nil
}
