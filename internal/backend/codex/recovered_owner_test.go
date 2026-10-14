//go:build darwin || linux

package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// This runs in a separate process so the Unix peer PID and ordinary Close
// target a real, controlled process rather than a synthetic connection.
func TestRecoveredOwnerSocketChild(t *testing.T) {
	path := os.Getenv("CB_RECOVERED_OWNER_TEST_SOCKET")
	if path == "" {
		return
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "controlled socket listen:", err)
		os.Exit(2)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		conn := websocket.NetConn(r.Context(), ws, websocket.MessageText)
		defer conn.Close()
		decode, encode := json.NewDecoder(conn), json.NewEncoder(conn)
		for {
			var request wireMessage
			if decode.Decode(&request) != nil {
				return
			}
			if request.Method != "" {
				log, err := os.OpenFile(path+".calls", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if err == nil {
					_, _ = log.WriteString(request.Method + "\n")
					_ = log.Close()
				}
			}
			if len(request.ID) == 0 {
				continue
			}
			var result any = map[string]any{}
			switch request.Method {
			case "initialize":
				result = map[string]string{"userAgent": "controlled-owner"}
			case "account/read":
				result = map[string]any{"account": map[string]string{"type": "apiKey"}, "requiresOpenaiAuth": true}
			case "thread/start", "thread/resume", "thread/read":
				result = map[string]any{"thread": nativeThread{ID: "thread-native"}, "model": "native-default"}
			}
			if encode.Encode(wireMessage{ID: request.ID, Result: raw(result)}) != nil {
				return
			}
		}
	})}
	_ = server.Serve(l)
}

func controlledOwnerProcess(t *testing.T) (*exec.Cmd, string, ownerProcessIdentity) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "cb-owner-")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "owner.sock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRecoveredOwnerSocketChild$")
	cmd.Env = []string{"CB_RECOVERED_OWNER_TEST_SOCKET=" + path, "PATH=/usr/bin:/bin"}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.RemoveAll(dir)
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("controlled owner did not listen")
		}
		time.Sleep(10 * time.Millisecond)
	}
	tools := newOwnedTools(cmd.Process.Pid)
	id := tools.processIdentity()
	tools.releaseRecoveredHandles()
	if !id.valid() || privateOwnerLifecycle(id) != ownerAlive {
		t.Fatal("controlled owner identity unavailable", id)
	}
	return cmd, path, id
}

func TestRecoveredPrivateOwnerCloseStopsOriginalProcess(t *testing.T) {
	_, path, id := controlledOwnerProcess(t)
	dir := t.TempDir()
	opts := SessionOptions{Directory: dir, StateFile: filepath.Join(dir, "binding.json")}
	first := NewSession(opts)
	first.binding.OwnerEndpoint, first.binding.OwnerProcess = "unix://"+path, &id
	first.binding.OwnerClosing = true // Crash after intent, before the old process stop.
	first.binding.CapacityStopRequest = "original-stop-request"
	if err := first.save(); err != nil {
		t.Fatal(err)
	}
	first.cancelLife()
	loaded := NewSession(opts)
	if err := loaded.Connect(testContext(t)); err != nil {
		t.Fatal("original private owner did not reconnect", err)
	}
	if loaded.client.UsesSharedServer() || !loaded.client.recoveredPrivate || loaded.client.privateOwnerIdentity() == nil || *loaded.client.privateOwnerIdentity() != id {
		t.Fatal("verified private lifecycle ownership was lost")
	}
	if privateOwnerLifecycle(id) != ownerAlive {
		t.Fatal("reconnection stopped original work")
	}
	if err := loaded.Close(testContext(t)); err != nil && !strings.Contains(err.Error(), "后台工具") {
		t.Fatal("ordinary Close did not settle original owner", err)
	}
	if privateOwnerLifecycle(id) != ownerGone {
		t.Fatal("ordinary Close left recovered private owner running")
	}
	reloaded := NewSession(opts)
	defer reloaded.cancelLife()
	if reloaded.binding.CapacityStopRequest != "original-stop-request" || !reloaded.binding.OwnerUnavailable || reloaded.binding.OwnerProcess == nil || *reloaded.binding.OwnerProcess != id {
		t.Fatal("original stop fact or verified exit was not durable")
	}
}

func TestSharedOwnerClosePreservesExternalProcess(t *testing.T) {
	_, path, id := controlledOwnerProcess(t)
	dir := t.TempDir()
	s := NewSession(SessionOptions{Directory: dir, StateFile: filepath.Join(dir, "binding.json"), Socket: path, RequiredSocket: true})
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if !s.client.UsesSharedServer() {
		t.Fatal("external socket was granted private ownership")
	}
	if err := s.Close(testContext(t)); err != nil && !strings.Contains(err.Error(), "后台工具") {
		t.Fatal(err)
	}
	if privateOwnerLifecycle(id) != ownerAlive {
		t.Fatal("ordinary Close stopped external shared owner")
	}
}

func TestRecoveredPrivateOwnerDetachKeepsOriginalProcess(t *testing.T) {
	_, path, id := controlledOwnerProcess(t)
	dir := t.TempDir()
	opts := SessionOptions{Directory: dir, StateFile: filepath.Join(dir, "binding.json")}
	first := NewSession(opts)
	first.binding.OwnerEndpoint, first.binding.OwnerProcess = "unix://"+path, &id
	first.binding.CapacityStopRequest = "original-stop-request"
	if err := first.save(); err != nil {
		t.Fatal(err)
	}
	first.cancelLife()
	observer := NewSession(opts)
	if err := observer.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if err := observer.DetachForUpdate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if privateOwnerLifecycle(id) != ownerAlive {
		t.Fatal("update detach stopped original private owner")
	}
	next := NewSession(opts)
	if err := next.Connect(testContext(t)); err != nil {
		t.Fatal("detached original owner could not be reobserved", err)
	}
	if next.client.UsesSharedServer() || !next.client.recoveredPrivate {
		t.Fatal("update reconnect lost private lifecycle responsibility")
	}
	if err := next.Close(testContext(t)); err != nil && !strings.Contains(err.Error(), "后台工具") {
		t.Fatal(err)
	}
	if privateOwnerLifecycle(id) != ownerGone {
		t.Fatal("ordinary Close after update detach left private owner running")
	}
}

func TestRetainedPrivateOwnerReconnectChecksOriginalPeer(t *testing.T) {
	_, path, id := controlledOwnerProcess(t)
	dir := t.TempDir()
	opts := SessionOptions{Directory: dir, StateFile: filepath.Join(dir, "binding.json")}
	seed := NewSession(opts)
	seed.binding.OwnerEndpoint, seed.binding.OwnerProcess = "unix://"+path, &id
	seed.binding.OwnerClosing = true
	seed.binding.CapacityStopRequest = "original-stop-request"
	if err := seed.save(); err != nil {
		t.Fatal(err)
	}
	seed.cancelLife()
	s := NewSession(opts)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	s.reconnectDelay = func(int) time.Duration { return time.Hour }
	old := s.client
	old.rpc.fail(ErrClosed)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal("same-process original peer was rejected", err)
	}
	if s.client.privateOwnerIdentity() == nil || *s.client.privateOwnerIdentity() != id || s.client.UsesSharedServer() {
		t.Fatal("same-process reconnect lost original private owner")
	}
	_ = s.Close(testContext(t)) // An old tool effect may remain unknown.
	if privateOwnerLifecycle(id) != ownerGone {
		t.Fatal("same-process reattach lost private owner close responsibility")
	}
}

func TestRetainedPrivateOwnerRejectsReplacedSocketPeer(t *testing.T) {
	_, originalPath, originalID := controlledOwnerProcess(t)
	_, replacementPath, replacementID := controlledOwnerProcess(t)
	dir := t.TempDir()
	opts := SessionOptions{Directory: dir, StateFile: filepath.Join(dir, "binding.json")}
	seed := NewSession(opts)
	seed.binding.OwnerEndpoint, seed.binding.OwnerProcess = "unix://"+originalPath, &originalID
	seed.binding.OwnerClosing = true
	seed.binding.CapacityStopRequest = "original-stop-request"
	if err := seed.save(); err != nil {
		t.Fatal(err)
	}
	seed.cancelLife()
	s := NewSession(opts)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	s.reconnectDelay = func(int) time.Duration { return time.Hour }
	if err := os.Remove(originalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, originalPath); err != nil {
		t.Fatal(err)
	}
	s.client.rpc.fail(ErrClosed)
	if err := s.Connect(testContext(t)); err == nil || !strings.Contains(err.Error(), "身份无法核实") {
		t.Fatal("replacement peer was accepted for the retained owner", err)
	}
	if privateOwnerLifecycle(originalID) != ownerAlive || privateOwnerLifecycle(replacementID) != ownerAlive {
		t.Fatal("failed reattach stopped an owner")
	}
	reloaded := NewSession(opts)
	defer reloaded.cancelLife()
	if reloaded.binding.OwnerProcess == nil || *reloaded.binding.OwnerProcess != originalID || reloaded.binding.CapacityStopRequest != "original-stop-request" || len(reloaded.binding.LostOwners) != 0 {
		t.Fatal("failed reattach changed original owner or stop receipt")
	}
	calls, err := os.ReadFile(replacementPath + ".calls")
	if err != nil || strings.Contains(string(calls), "thread/resume") || strings.Contains(string(calls), "thread/read") || strings.Contains(string(calls), "turn/start") {
		t.Fatal("replacement peer received original work or recovery reads", string(calls), err)
	}
	if err := s.DetachForUpdate(testContext(t)); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownPrivateIdentityDoesNotStopSocketPeer(t *testing.T) {
	_, path, peer := controlledOwnerProcess(t)
	_, _, other := controlledOwnerProcess(t)
	dir := t.TempDir()
	opts := SessionOptions{Directory: dir, StateFile: filepath.Join(dir, "binding.json")}
	first := NewSession(opts)
	first.binding.OwnerEndpoint, first.binding.OwnerProcess = "unix://"+path, &other
	first.binding.OwnerClosing = true
	first.binding.CapacityStopRequest = "original-stop-request"
	if err := first.save(); err != nil {
		t.Fatal(err)
	}
	first.cancelLife()
	loaded := NewSession(opts)
	if err := loaded.Connect(testContext(t)); err == nil || !strings.Contains(err.Error(), "身份") {
		t.Fatal("mismatched Unix peer was granted private ownership", err)
	}
	if err := loaded.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if privateOwnerLifecycle(peer) != ownerAlive || privateOwnerLifecycle(other) != ownerAlive {
		t.Fatal("identity mismatch signalled an unowned process")
	}
	reloaded := NewSession(opts)
	defer reloaded.cancelLife()
	if reloaded.binding.OwnerProcess == nil || *reloaded.binding.OwnerProcess != other || reloaded.binding.CapacityStopRequest != "original-stop-request" || len(reloaded.binding.LostOwners) != 0 {
		t.Fatal("unknown original identity was overwritten")
	}
}

func TestMissingPrivateBirthDoesNotStopSocketPeer(t *testing.T) {
	_, path, peer := controlledOwnerProcess(t)
	dir := t.TempDir()
	opts := SessionOptions{Directory: dir, StateFile: filepath.Join(dir, "binding.json")}
	first := NewSession(opts)
	first.binding.OwnerEndpoint = "unix://" + path
	first.binding.OwnerProcess = &ownerProcessIdentity{PID: peer.PID}
	first.binding.OwnerClosing = true
	if err := first.save(); err != nil {
		t.Fatal(err)
	}
	first.cancelLife()
	loaded := NewSession(opts)
	if err := loaded.Connect(testContext(t)); err == nil || loaded.Snapshot().ConnectionIssue != "owner_lifecycle_unknown" {
		t.Fatal("missing process birth escaped isolation", err)
	}
	if err := loaded.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if privateOwnerLifecycle(peer) != ownerAlive {
		t.Fatal("missing identity stopped an unverified socket peer")
	}
}
