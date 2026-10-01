//go:build darwin || linux

package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
	"golang.org/x/sys/unix"
)

func startOwnedHost(ctx context.Context, o OwnedHostOptions) (*ownedHost, error) {
	return startOwnedHostWithStore(ctx, o, false)
}

func startOwnedHostWithStore(ctx context.Context, o OwnedHostOptions, requireExisting bool) (*ownedHost, error) {
	if o.NodeID == "" || !filepath.IsAbs(o.Store) {
		return nil, errors.New("owned Caelis requires a designated absolute node store")
	}
	store := filepath.Clean(o.Store)
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if store == filepath.Join(home, ".caelis") {
		return nil, errors.New("shared default Caelis store cannot be owned")
	}
	for p := store; p != filepath.Dir(p); p = filepath.Dir(p) {
		if f, e := os.Lstat(p); e == nil && f.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("owned Caelis store must not traverse symlinks")
		}
	}
	marker := filepath.Join(store, ".caelis-bot-node-owner.json")
	if _, err = os.Lstat(store); errors.Is(err, os.ErrNotExist) {
		if requireExisting {
			return nil, errors.New("owned Caelis store requires explicit preparation")
		}
		if err = os.Mkdir(store, 0700); err != nil {
			return nil, err
		}
		b, _ := json.Marshal(struct {
			NodeID string `json:"nodeId"`
		}{o.NodeID})
		if err = os.WriteFile(marker, b, 0600); err != nil {
			return nil, err
		}
	} else {
		f, e := os.Lstat(store)
		if e != nil || !f.IsDir() || f.Mode().Perm()&0077 != 0 {
			return nil, errors.New("owned Caelis store is not private")
		}
		markerInfo, e := os.Lstat(marker)
		if e != nil || !markerInfo.Mode().IsRegular() || markerInfo.Mode().Perm()&0077 != 0 {
			return nil, errors.New("owned Caelis store marker is not private")
		}
		b, e := os.ReadFile(marker)
		var m struct {
			NodeID string `json:"nodeId"`
		}
		if e != nil || json.Unmarshal(b, &m) != nil || m.NodeID != o.NodeID {
			return nil, errors.New("existing Caelis store is not this node's owned store")
		}
	}
	lockPath := filepath.Join(store, ".caelis-bot-node.lock")
	if info, e := os.Lstat(lockPath); e == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("owned Caelis store lock is not private")
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("Caelis store already has an owned controller")
	}
	fail := func(err error) (*ownedHost, error) { lock.Close(); return nil, err }
	if _, err = os.Lstat(filepath.Join(store, "runtime/service/discovery.json")); !errors.Is(err, os.ErrNotExist) {
		return fail(errors.New("existing Caelis discovery cannot be adopted as an owned host"))
	}
	binary, err := caelisruntime.Find(o.Binary)
	if err != nil {
		return fail(err)
	}
	privateHome := filepath.Join(store, ".native-home")
	if info, e := os.Lstat(privateHome); e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0) {
		return fail(errors.New("owned Caelis native home is not private"))
	}
	if err = os.MkdirAll(privateHome, 0700); err != nil {
		return fail(err)
	}
	p, err := codex.StartOwnedForeground(ctx, o.WatchdogHelper, binary, store, store)
	if err != nil {
		return fail(err)
	}
	h := &ownedHost{process: p, settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: binary, CaelisStore: store}, lock: lock}
	ready, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if !p.Live() {
			stop, finish := context.WithTimeout(context.Background(), 4*time.Second)
			stopErr := h.stop(stop)
			finish()
			return nil, errors.Join(errors.New("owned Caelis foreground exited before readiness"), stopErr)
		}
		d, _, e := Discover(h.settings)
		if e == nil {
			c, e := setupClient(ready, h.settings)
			if e == nil {
				c.http.CloseIdleConnections()
				h.instance = d.InstanceID
				return h, nil
			}
		}
		select {
		case <-ready.Done():
			stop, finish := context.WithTimeout(context.Background(), 4*time.Second)
			stopErr := h.stop(stop)
			finish()
			return nil, errors.Join(errors.New("owned Caelis foreground readiness unconfirmed"), ready.Err(), stopErr)
		case <-tick.C:
		}
	}
}
func (h *ownedHost) check(ctx context.Context) error {
	if !h.process.Live() {
		return errors.New("owned Caelis foreground is not live")
	}
	d, _, err := Discover(h.settings)
	if err != nil {
		return err
	}
	if d.InstanceID != h.instance {
		return errors.New("Caelis foreground discovery changed")
	}
	return ctx.Err()
}
func (h *ownedHost) ready(ctx context.Context, model string) error {
	out, err := inspectOwnedReadiness(ctx, h, model)
	if err != nil {
		return err
	}
	if !out.Ready {
		return errors.New(out.Reason)
	}
	return nil
}

func (h *ownedHost) stop(ctx context.Context) error {
	h.once.Do(func() {
		h.stopErr = h.process.Stop(ctx)
		if h.stopErr == nil {
			d, _, err := Discover(h.settings)
			if err == nil && d.InstanceID == h.instance {
				h.stopErr = os.Remove(filepath.Join(h.settings.CaelisStore, "runtime/service/discovery.json"))
			}
		}
		_ = h.lock.Close()
	})
	return h.stopErr
}
