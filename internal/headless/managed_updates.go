package headless

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/managedupdate"
	"github.com/caelis-labs/caelis-bot/internal/websurface"
)

type managedHost struct {
	client *managedupdate.Client
	boot   managedupdate.Boot
	gate   sync.RWMutex
	ready  bool
	frozen bool
}

func openManaged(version string) (*managedHost, error) {
	root, boot := os.Getenv("CAELIS_BOT_MANAGED_ROOT"), os.Getenv("CAELIS_BOT_MANAGED_BOOT")
	if root == "" && boot == "" {
		exe, e := os.Executable()
		if e != nil {
			return nil, e
		}
		exe, e = filepath.EvalSymlinks(exe)
		if e != nil {
			return nil, e
		}
		if managedupdate.RequiresLauncher(exe) {
			return nil, errors.New("managed installation requires its launcher")
		}
		return nil, nil
	}
	if root == "" || boot == "" {
		return nil, errors.New("managed identity incomplete")
	}
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	exe, e = filepath.EvalSymlinks(exe)
	if e != nil {
		return nil, e
	}
	relative, e := filepath.Rel(filepath.Join(root, "versions"), exe)
	if e != nil || filepath.Base(relative) != "caelisbot" || filepath.Dir(filepath.Dir(relative)) != "." {
		return nil, errors.New("managed executable outside release directory")
	}
	if e = managedupdate.RetainChildLease(root); e != nil {
		return nil, e
	}
	c, e := managedupdate.NewClient(root)
	if e != nil {
		return nil, e
	}
	return &managedHost{client: c, boot: managedupdate.Boot{ID: boot, PID: os.Getpid(), Identity: managedupdate.Identity{Version: version, Source: managedupdate.Source, Kind: managedupdate.BuildKind}}}, nil
}
func (h *managedHost) handler(next http.Handler) http.Handler {
	if h == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.gate.RLock()
		if !h.ready || h.frozen {
			h.gate.RUnlock()
			w.Header().Set("Retry-After", "1")
			http.Error(w, "restarting", 503)
			return
		}
		// SSE must not hold admission for its entire connection lifetime.
		if r.Method == http.MethodGet {
			h.gate.RUnlock()
		} else {
			defer h.gate.RUnlock()
		}
		next.ServeHTTP(w, r)
	})
}
func (h *managedHost) markReady(core *app.Application) error {
	if h == nil {
		return nil
	}
	// Local durable state must load before a candidate becomes healthy. No
	// model, scheduler, channels or restored task execution starts here.
	if e := core.PreparePersonal(); e != nil {
		return e
	}
	for attempt := 0; attempt < 20; attempt++ {
		if e := h.client.Ready(h.boot); e == nil {
			h.gate.Lock()
			h.ready = true
			h.gate.Unlock()
			return nil
		}
		time.Sleep(50 * time.Millisecond) // Parent publishes exact child PID after Start.
	}
	return errors.New("managed health acknowledgement failed")
}
func (h *managedHost) watch(ctx context.Context, core *app.Application, server *websurface.Server, done chan<- error) {
	if h == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		request, e := h.client.Handoff(h.boot.ID)
		if e != nil || request.State != "requested" {
			continue
		}
		if !h.gate.TryLock() {
			continue
		}
		quiescent := server.UpdateQuiescent()
		var prepareErr error
		if quiescent {
			prepareErr = core.PrepareIdleUpdate()
		}
		if !quiescent || prepareErr != nil {
			if !quiescent {
				request.Reason = "operation_unconfirmed"
			} else if errors.Is(prepareErr, app.ErrManagedChannelHandoff) {
				request.Reason = "channel_handoff_required"
			}
			h.gate.Unlock()
			request.State = "busy"
			_ = h.client.Finish(request)
			continue
		}
		// No browser mutation can race this final admission/Close boundary.
		h.frozen = true
		if e = core.Close(); e != nil {
			request.State = "failed"
		} else {
			request.State = "committed"
		}
		ack := h.client.Finish(request)
		h.gate.Unlock()
		done <- errors.Join(e, ack)
		return
	}
}
