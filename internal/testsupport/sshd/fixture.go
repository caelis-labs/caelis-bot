//go:build !windows

// Package sshd supplies an opt-in, loopback-only native OpenSSH test fixture.
// All keys and trust files are generated under the test's disposable directory.
package sshd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type Fixture struct {
	Root, Identity, KnownHosts string
	Port                       int
	cmd                        *exec.Cmd
	done                       chan error
}

func New(t testing.TB) *Fixture {
	t.Helper()
	if os.Getenv("CAELIS_BOT_TEST_SSHD") != "1" {
		t.Skip("opt-in isolated native OpenSSH fixture")
	}
	f := &Fixture{Root: t.TempDir()}
	f.Identity, f.KnownHosts = filepath.Join(f.Root, "identity"), filepath.Join(f.Root, "known_hosts")
	host := filepath.Join(f.Root, "hostkey")
	for _, path := range []string{f.Identity, host} {
		if err := exec.Command("/usr/bin/ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", path).Run(); err != nil {
			t.Fatal("generate isolated key", err)
		}
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.Port = l.Addr().(*net.TCPAddr).Port
	l.Close()
	public, err := os.ReadFile(host + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		f.KnownHosts:                         fmt.Sprintf("[127.0.0.1]:%d %s", f.Port, public),
		filepath.Join(f.Root, "sshd_config"): fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %q\nPidFile %q\nAuthorizedKeysFile %q\nStrictModes no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nAllowUsers %s\n", f.Port, host, filepath.Join(f.Root, "pid"), f.Identity+".pub", os.Getenv("USER")),
	}
	for path, data := range files {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(f.Stop)
	if err := f.Start(); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *Fixture) Config(alias string) string {
	return fmt.Sprintf("Host %s\n HostName 127.0.0.1\n Port %d\n User %s\n IdentityFile %q\n IdentitiesOnly yes\n StrictHostKeyChecking yes\n UserKnownHostsFile %q\n", alias, f.Port, os.Getenv("USER"), f.Identity, f.KnownHosts)
}

func (f *Fixture) Start() error {
	if f.cmd != nil {
		return fmt.Errorf("fixture already running")
	}
	f.cmd = exec.Command("/usr/sbin/sshd", "-D", "-e", "-f", filepath.Join(f.Root, "sshd_config"))
	if err := f.cmd.Start(); err != nil {
		f.cmd = nil
		return err
	}
	f.done = make(chan error, 1)
	cmd, done := f.cmd, f.done
	go func() { done <- cmd.Wait() }()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		select {
		case err := <-done:
			done <- err
			return fmt.Errorf("isolated sshd exited: %w", err)
		default:
		}
		conn, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", f.Port), 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("isolated sshd did not listen")
}

func (f *Fixture) Stop() {
	if f.cmd != nil {
		_ = f.cmd.Process.Kill()
		<-f.done
		f.cmd = nil
	}
}
