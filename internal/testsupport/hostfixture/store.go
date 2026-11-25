// Package hostfixture owns ephemeral Stores and their native test processes.
package hostfixture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"github.com/caelis-labs/caelis-bot/internal/sharedruntime"
)

const cleanupTimeout = 45 * time.Second

type Store struct {
	Root, Path string
	Binary     string
	Env        []string
	foreground *Process
	observed   map[int]processIdentity
	forced     []int
}

func New(t *testing.T, binary, storeName string) *Store {
	t.Helper()
	if !filepath.IsLocal(storeName) || filepath.Base(storeName) != storeName || storeName == "." {
		t.Fatalf("invalid private Store name: %q", storeName)
	}
	root, err := os.MkdirTemp("", "bot-host-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(errors.Join(err, os.Remove(root)))
	}
	s := &Store{Root: resolved, Path: filepath.Join(resolved, storeName), Binary: binary, observed: map[int]processIdentity{}}
	// Avoid TempDir's unconditional removal when exit cannot be confirmed.
	t.Cleanup(func() { s.cleanup(t) })
	return s
}

// IsolateRecoveryEnvironment also scopes the CLI invoked by an in-process Bot
// reconnect, which inherits its environment from the test process.
func (s *Store) IsolateRecoveryEnvironment(t *testing.T) {
	t.Helper()
	home, temp := filepath.Join(s.Root, "home"), filepath.Join(s.Root, "tmp")
	for _, path := range []string{home, temp} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{
		"HOME": home, "USERPROFILE": home, "APPDATA": home, "LOCALAPPDATA": home,
		"XDG_CONFIG_HOME": home, "XDG_CACHE_HOME": home, "XDG_DATA_HOME": home,
		"TMPDIR": temp, "TEMP": temp, "TMP": temp,
	} {
		t.Setenv(key, value)
	}
	s.Env = runtimeenv.Clean(os.Environ())
}

func (s *Store) StartForeground(ctx context.Context) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.foreground != nil {
		return nil, errors.New("foreground Host already started")
	}
	// The fixture owns shutdown even when ctx is canceled before readiness.
	cmd := exec.Command(s.Binary, "serve", "--store-dir", s.Path, "--listen", "127.0.0.1:0")
	cmd.Dir, cmd.Env = s.Root, s.Env
	runtimeenv.BackgroundCommand(cmd)
	log, err := os.OpenFile(filepath.Join(s.Root, "host.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = log, log
	s.foreground, err = Start(cmd)
	return s.foreground, errors.Join(err, log.Close())
}

func (s *Store) WaitForeground(ctx context.Context) error {
	if s.foreground == nil {
		return errors.New("foreground Host was not started")
	}
	err := s.foreground.Wait(ctx)
	if s.foreground.Exited() {
		s.foreground = nil
	}
	return err
}

func (s *Store) StopForeground(ctx context.Context) error {
	if s.foreground == nil {
		return nil
	}
	_, scanErr := s.capture(ctx)
	err := s.foreground.Stop(ctx)
	if s.foreground.Exited() {
		s.foreground = nil
	}
	return errors.Join(scanErr, err)
}

func (s *Store) cleanup(t *testing.T) {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	err := s.stop(ctx)
	if len(s.forced) > 0 {
		t.Logf("forced exit requested for private processes %v", s.forced)
	}
	if err != nil {
		t.Errorf("private Host cleanup after %s, Store retained at %s: %v", time.Since(started).Round(time.Millisecond), s.Root, err)
		return
	}
	pids := make([]int, 0, len(s.observed))
	for pid := range s.observed {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	if out := os.Getenv("CAELIS_BOT_TEST_EVIDENCE_DIR"); t.Failed() && out != "" {
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Errorf("create failure evidence directory (fixture retained at %s): %v", s.Root, err)
			return
		}
		target := filepath.Join(out, filepath.Base(s.Root))
		if err := os.Rename(s.Root, target); err != nil {
			t.Errorf("preserve failure evidence (fixture retained at %s): %v", s.Root, err)
			return
		}
		t.Logf("verified exit of private processes %v in %s, retained evidence at %s", pids, time.Since(started).Round(time.Millisecond), target)
		return
	}
	if err := os.RemoveAll(s.Root); err != nil {
		t.Errorf("remove private fixture %s: %v", s.Root, err)
		return
	}
	t.Logf("verified exit of private processes %v and removed %s in %s", pids, s.Root, time.Since(started).Round(time.Millisecond))
}

func (s *Store) stop(ctx context.Context) error {
	foreground, cancel := context.WithTimeout(ctx, 5*time.Second)
	foregroundErr := s.StopForeground(foreground)
	cancel()
	// Recovery starts with its own budget and may outlive a closed Session.
	// Wait before checking the Store so a late detached Host cannot escape.
	drain, cancel := context.WithTimeout(ctx, 33*time.Second)
	drainErr := sharedruntime.Shared.Wait(drain, "caelis:"+s.Path)
	cancel()
	_, scanErr := s.capture(ctx)
	var stopErr error
	if _, err := os.Stat(filepath.Join(s.Path, "runtime", "service", "discovery.json")); err == nil {
		stopCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		cmd := exec.CommandContext(stopCtx, s.Binary, "service", "stop", "--store-dir", s.Path, "--format", "json")
		cmd.Dir, cmd.Env = s.Root, s.Env
		cmd.WaitDelay = time.Second
		runtimeenv.BackgroundCommand(cmd)
		if err := cmd.Run(); err != nil {
			stopErr = fmt.Errorf("private service stop: %w", err)
		}
		cancel()
	} else if !errors.Is(err, os.ErrNotExist) {
		stopErr = err
	}

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	grace := time.Now().Add(time.Second)
	// Check again even when discovery was removed: Host children can still
	// be exiting, and a failed start can leave no discovery record at all.
	for {
		live, err := s.capture(ctx)
		if err != nil {
			return errors.Join(foregroundErr, drainErr, scanErr, stopErr, err)
		}
		if len(live) == 0 {
			return errors.Join(foregroundErr, drainErr, scanErr, stopErr)
		}
		if !time.Now().Before(grace) {
			for _, p := range live {
				if slices.Contains(s.forced, p.pid) {
					continue
				}
				if err := p.kill(ctx); err != nil && !errors.Is(err, os.ErrProcessDone) {
					return errors.Join(foregroundErr, drainErr, scanErr, stopErr, err)
				}
				s.forced = append(s.forced, p.pid)
			}
		}
		select {
		case <-ctx.Done():
			return errors.Join(foregroundErr, drainErr, scanErr, stopErr, fmt.Errorf("private processes still live: %v: %w", live, ctx.Err()))
		case <-ticker.C:
		}
	}
}

func (s *Store) recordDescendants(all, live map[int]processIdentity) []processIdentity {
	for added := true; added; {
		added = false
		for pid, p := range all {
			if _, owned := live[pid]; !owned && live[p.parent].pid != 0 {
				live[pid] = p
				added = true
			}
		}
	}
	result := make([]processIdentity, 0, len(live))
	for pid, p := range live {
		s.observed[pid] = p
		result = append(result, p)
	}
	return result
}

// ActivePIDs snapshots the scope used by cleanup for lifecycle assertions.
func (s *Store) ActivePIDs(ctx context.Context) ([]int, error) {
	live, err := s.capture(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(live))
	for _, p := range live {
		ids = append(ids, p.pid)
	}
	sort.Ints(ids)
	return ids, nil
}
