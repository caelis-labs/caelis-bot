package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/runtimeinstall"
)

// One host-owned installation, shared by every surface. No browser connection
// owns its lifetime. Only explicit install/update calls start work; snapshots
// and restart recovery never replay an installation.
type codexInstallation struct {
	mu       sync.Mutex
	path     string
	progress api.SetupInstallProgress
	profile  api.RuntimeSettings
	cancel   context.CancelFunc
	done     chan struct{}
	closed   bool
}

func newCodexInstallation(root string) *codexInstallation {
	c := &codexInstallation{path: filepath.Join(root, "codex-installation.json")}
	if data, err := os.ReadFile(c.path); err == nil {
		if json.Unmarshal(data, &c.progress) != nil {
			c.progress = api.SetupInstallProgress{Runtime: "codex", ID: rand.Text(), Action: "install", State: "unknown", Error: "安装已中断，请重新检测程序后再试"}
		} else if c.progress.State == "running" {
			c.progress.State, c.progress.Error = "unknown", "安装结果尚未确认，请重新检测程序"
		}
	}
	return c
}

func (c *codexInstallation) snapshot() api.SetupInstallProgress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.progress
}
func (c *codexInstallation) runningState(v api.RuntimeSettings) (api.SetupState, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return api.SetupState{Settings: v, State: "installing", Models: []api.SetupChoice{}}, v.Runtime == "codex" && c.progress.State == "running"
}
func (c *codexInstallation) stop() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel == nil {
		return false
	}
	c.cancel()
	return true
}
func (c *codexInstallation) close() {
	c.mu.Lock()
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	done := c.done
	c.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (c *codexInstallation) start(ctx context.Context, r api.SetupRequest, run func(context.Context, api.SetupRequest) (api.SetupState, error)) (api.SetupState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return api.SetupState{}, errors.New("应用正在退出")
	}
	if c.progress.State == "unknown" {
		return api.SetupState{}, errors.New("安装结果尚未确认，请重新检测；不会重复安装")
	}
	if c.progress.State == "running" {
		if c.profile != r.Settings {
			return api.SetupState{}, errors.New("正在安装 Codex，请等待完成")
		}
		return api.SetupState{Settings: r.Settings, State: "installing", Models: []api.SetupChoice{}}, nil
	}
	// The existing typed ingress receipt records this accepted operation. A
	// durable local marker makes a process restart explicit, never a retry.
	p := api.SetupInstallProgress{Runtime: "codex", ID: rand.Text(), Action: r.Action, State: "running", Phase: "checking"}
	if err := localstate.Write(c.path, p); err != nil {
		return api.SetupState{}, errors.New("无法保存安装状态，请稍后重试")
	}
	c.progress, c.profile = p, r.Settings
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	c.cancel = cancel
	done := make(chan struct{})
	c.done = done
	go func() {
		defer close(done)
		defer cancel()
		ctx = runtimeinstall.WithProgress(ctx, func(phase string) {
			c.mu.Lock()
			c.progress.Phase = phase // No invented bytes or percentage.
			c.mu.Unlock()
		})
		_, err := run(ctx, r)
		c.mu.Lock()
		defer c.mu.Unlock()
		c.cancel = nil
		c.progress.State = "complete"
		if err != nil {
			c.progress.State, c.progress.Error = "failed", "安装未完成，请检查网络和安装目录后重试"
			if errors.Is(ctx.Err(), context.Canceled) {
				c.progress.State, c.progress.Error = "cancelled", "安装已取消"
			}
		} else {
			c.progress.Phase = "complete"
		}
		if localstate.Write(c.path, c.progress) != nil {
			c.progress.State, c.progress.Error = "unknown", "无法确认安装结果，请重新检测程序"
		}
	}()
	return api.SetupState{Settings: r.Settings, State: "installing", Models: []api.SetupChoice{}}, nil
}
