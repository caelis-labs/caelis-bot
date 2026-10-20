package managedupdate

import (
	"context"
	"crypto/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type LaunchOptions struct {
	Root         string
	Platform     string
	Trust        Trust
	ReadyTimeout time.Duration
}

// Launch is a stable supervisor. Its PID/unit stays alive while its one Bot
// child is replaced; it never restarts/kills a shared Runtime or another unit.
func Launch(ctx context.Context, opt LaunchOptions) error {
	lease, e := acquireLease(opt.Root)
	if e != nil {
		return e
	}
	defer lease.Close()
	var install Installation
	if e = readJSON(filepath.Join(opt.Root, "installation.json"), &install); e != nil {
		return e
	}
	if install.Schema != 1 || install.QA != opt.Trust.QA {
		return problem("installation_identity_mismatch")
	}
	if e = privateDirectory(install.Profile); e != nil {
		return e
	}
	m, e := Open(ctx, opt.Root, opt.Platform, opt.Trust)
	if e != nil {
		return e
	}
	defer m.Close()
	sock := filepath.Join(opt.Root, "control.sock")
	// The held launcher lock proves the previous socket has no launcher owner.
	if i, e := os.Lstat(sock); e == nil {
		if i.Mode()&os.ModeSocket == 0 {
			return problem("control_path_conflict")
		}
		if e = os.Remove(sock); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	listener, e := net.Listen("unix", sock)
	if e != nil {
		return e
	}
	defer listener.Close()
	if e = os.Chmod(sock, 0600); e != nil {
		return e
	}
	server := &http.Server{Handler: m.handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	defer server.Close()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	timeout := opt.ReadyTimeout
	if timeout == 0 {
		timeout = 45 * time.Second
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	recoveredOld := false
	for {
		id, e := selected(opt.Root)
		if e != nil {
			return e
		}
		o, e := loadOffer(opt.Root, id, opt.Trust, opt.Platform)
		if e != nil {
			return e
		}
		cmd := exec.Command(filepath.Join(opt.Root, "versions", id, "caelisbot"), append([]string{"serve", "--data-dir", install.Profile}, install.Args...)...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		cmd.ExtraFiles = []*os.File{lease}
		boot := Boot{ID: rand.Text(), Identity: o.Manifest.Identity()}
		for _, env := range os.Environ() {
			if !isManagedEnvironment(env) {
				cmd.Env = append(cmd.Env, env)
			}
		}
		cmd.Env = append(cmd.Env, "CAELIS_BOT_MANAGED_ROOT="+opt.Root, "CAELIS_BOT_MANAGED_BOOT="+boot.ID)
		if e = cmd.Start(); e != nil {
			m.mu.Lock()
			tx := m.state.Transaction
			if tx != nil && tx.Phase == "switched" {
				e = m.rollback()
				m.mu.Unlock()
				if e == nil {
					continue
				}
			} else {
				m.mu.Unlock()
			}
			return e
		}
		boot.PID = cmd.Process.Pid
		m.mu.Lock()
		m.boot = boot
		m.ready = false
		m.state.Status.BootID = boot.ID
		m.state.Status.Current = boot.Identity
		m.mu.Unlock()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		health := time.NewTimer(timeout)
		childEnded := false
		var childError error
		for !childEnded {
			select {
			case <-ctx.Done():
				_ = cmd.Process.Signal(os.Interrupt)
				// Explicit supervisor stop never force-kills unknown native work.
				select {
				case <-done:
					return nil
				case <-time.After(30 * time.Second):
					return problem("child_shutdown_unconfirmed")
				}
			case e := <-serverDone:
				_ = cmd.Process.Signal(os.Interrupt)
				<-done
				return e
			case childError = <-done:
				childEnded = true
			case <-health.C:
				m.mu.Lock()
				ready := m.ready
				m.mu.Unlock()
				if !ready {
					_ = cmd.Process.Kill()
				} // This child has not admitted work.
			case <-tick.C:
				m.tick()
			}
		}
		health.Stop()
		m.mu.Lock()
		tx := m.state.Transaction
		switch {
		case tx != nil && tx.Phase == "committed":
			e = m.switchRelease()
		case tx != nil && tx.Phase == "switched":
			e = m.rollback()
		case tx != nil && tx.Phase == "rolling_back":
			m.state.Status.State = "unknown"
			m.state.Status.Reason = "rollback_unhealthy"
			tx.Phase = "unknown"
			_ = m.save()
			e = problem("rollback_unhealthy")
		case tx != nil && (tx.Phase == "requested" || tx.Phase == "cancelled" || tx.Phase == "unknown") && !recoveredOld:
			// A cancelled handoff or failed Close can leave the old process
			// stopped. Relaunch it once without changing selection or receipts.
			recoveredOld = true
			if tx.Phase == "requested" {
				tx.Phase = "waiting"
				m.state.Status.State = "waiting_idle"
			}
			e = m.save()
		default:
			e = childError
			if e == nil {
				e = problem("child_stopped")
			}
		}
		m.mu.Unlock()
		if e != nil {
			return e
		}
	}
}

func (m *Manager) Ready(b Boot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b.ID != m.boot.ID || b.PID != m.boot.PID || b.Identity != m.boot.Identity || b.PID <= 0 || b.ID == "" {
		return problem("boot_mismatch")
	}
	if m.ready && m.state.Status.Runtime == b.Runtime {
		return nil
	}
	m.state.Status.Runtime = b.Runtime
	if b.Runtime.State == "failed" && !m.ready {
		m.state.Status.Reason = "runtime_readback_failed"
		if b.Runtime.Reason == "runtime_start_failed" {
			m.state.Status.Reason = b.Runtime.Reason
		}
		if err := m.save(); err != nil {
			return err
		}
		return problem(m.state.Status.Reason)
	}
	tx := m.state.Transaction
	if tx != nil {
		switch tx.Phase {
		case "switched":
			tx.Phase = "healthy"
			m.state.Status.State = "installed"
			m.state.Status.Scheduled = false
			m.state.Status.Reason = ""
		case "rolling_back":
			tx.Phase = "rolled_back"
			m.state.Status.State = "rolled_back"
			m.state.Status.Scheduled = false
		}
	}
	m.state.Status.Current = b.Identity
	m.state.Status.BootID = b.ID
	if e := m.save(); e != nil {
		return e
	}
	m.ready = true
	return nil
}

func isManagedEnvironment(s string) bool {
	const prefix = "CAELIS_BOT_MANAGED_"
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
