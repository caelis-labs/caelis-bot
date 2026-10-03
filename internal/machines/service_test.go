package machines

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type inert struct{}

func (inert) WorkAdmission(context.Context) error { return nil }
func (inert) WorkStates() []api.WorkState         { return nil }
func (inert) StartWork(context.Context, api.WorkStart) (api.Task, error) {
	panic("must not start local work")
}
func (inert) ReadWork(context.Context, string) (api.Task, error)          { return api.Task{}, nil }
func (inert) SendWork(context.Context, api.TaskMessage) (api.Task, error) { return api.Task{}, nil }
func (inert) StopWork(context.Context, string) (api.Task, error)          { return api.Task{}, nil }
func TestRejectedSSHInputsNeverExecute(t *testing.T) {
	base := api.MachineInput{Address: "example.test", Port: 22, User: "user", Authentication: "agent"}
	for _, host := range []string{"-oProxyCommand=bad", "a\ncommand", "user@host", "a; echo secret", "$(touch bad)"} {
		v := base
		v.Address = host
		if validate(v) == nil {
			t.Fatalf("accepted %q", host)
		}
	}
	v := base
	v.PrivateKey = "relative"
	v.Authentication = "key"
	if validate(v) == nil {
		t.Fatal("relative key")
	}
}
func TestMachineCredentialsAreNotSerialized(t *testing.T) {
	s, e := Open(t.TempDir(), inert{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	s.secrets["machine-test"] = "secret-canary"
	s.state.Profiles["machine-test"] = profile{View: api.Machine{ID: "machine-test", Name: "Test"}}
	if e = s.save(); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(s.root, "machines.json"))
	if string(b) == "" {
		t.Fatal("store missing")
	}
	if contains(b, []byte("secret-canary")) {
		t.Fatal("credential entered disk state")
	}
	again, e := Open(s.root, inert{}, nil)
	if e != nil || len(again.secrets) != 0 {
		t.Fatal("secret survived unencrypted")
	}
}
func contains(a, b []byte) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if string(a[i:i+len(b)]) == string(b) {
			return true
		}
	}
	return false
}
func TestOriginalMachineCannotBeRemoved(t *testing.T) {
	s, _ := Open(t.TempDir(), inert{}, nil)
	s.state.Routes["task-original"] = "machine-owned"
	s.state.Profiles["machine-owned"] = profile{View: api.Machine{ID: "machine-owned"}}
	if e := s.RemoveMachine(t.Context(), "machine-owned"); e == nil {
		t.Fatal("lost task owner")
	}
	if _, ok := s.state.Profiles["machine-owned"]; !ok {
		t.Fatal("profile removed")
	}
}

func TestOfflineMachineCanBeRemovedAndStaysRemoved(t *testing.T) {
	s, err := Open(t.TempDir(), inert{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const id = "machine-disposable-offline"
	s.state.Profiles[id] = profile{View: api.Machine{ID: id, State: "offline", SSHConfig: true}}
	s.secrets[id] = "disposable-secret"
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMachine(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if len(s.Machines()) != 0 || len(s.secrets) != 0 {
		t.Fatal("offline profile or in-memory secret retained")
	}
	again, err := Open(s.root, inert{}, nil)
	if err != nil || len(again.Machines()) != 0 {
		t.Fatal("deleted profile returned after reopen", err)
	}
}

func TestSSHDiagnosticIsBoundedAndDrainsStderr(t *testing.T) {
	var diagnostic sshDiagnostic
	for range 3 {
		p := make([]byte, 32<<10)
		if n, err := diagnostic.Write(p); err != nil || n != len(p) {
			t.Fatal("SSH stderr must be fully drained", n, err)
		}
	}
	if diagnostic.Len() != 8<<10 {
		t.Fatal("SSH stderr exceeded diagnostic bound")
	}
	// os/exec drains stderr through io.Copy. Do not accidentally expose a
	// ReaderFrom method that bypasses Write's bound.
	n, err := io.Copy(&diagnostic, io.LimitReader(strings.NewReader(strings.Repeat("x", 32<<10)), 32<<10))
	if err != nil || n != 32<<10 || diagnostic.Len() != 8<<10 {
		t.Fatal("stderr copy bypassed diagnostic bound", n, err)
	}
}

func TestSSHControlDirectoryRejectsRedirectedAndPublicDirectory(t *testing.T) {
	s, err := Open(t.TempDir(), inert{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := s.controlDirectory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dir) })
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = s.sshArgs(profile{}, false); err == nil {
		t.Fatal("accepted a public multiplexing socket directory")
	}
	if err = os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(t.TempDir(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err = s.sshArgs(profile{}, true); err == nil {
		t.Fatal("accepted a redirected terminal socket directory")
	}
}
