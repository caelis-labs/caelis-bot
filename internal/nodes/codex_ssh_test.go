package nodes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type noNativeSource struct{}

func (noNativeSource) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	return api.WorkDispatchSource{}, nil
}

func TestCodexSSHWorkerUsesStrictNativeProxyAndBoundedDetach(t *testing.T) {
	dir := t.TempDir()
	binary, argsFile := filepath.Join(dir, "ssh"), filepath.Join(dir, "args")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nfor arg do printf '%s\\n' \"$arg\"; done > "+sshQuote(argsFile)+"\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: "linux-node", Backend: "codex", Role: api.RoleWorker}, BotID: "origin-bot", SourceNode: "local", SourceBackend: "codex"}
	// Helper EOF is the deterministic detach barrier; the timeout is only a
	// failure bound, so slow race-instrumented process startup cannot kill it
	// before its argument capture is published.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if client, err := NewCodexSSHWorker(ctx, CodexSSHConfig{Destination: "fixture", Helper: "/fixture/node helper", Socket: "/fixture/private/worker.sock", Binary: binary, Pair: pair, Source: noNativeSource{}}); err == nil {
		client.Close()
		t.Fatal("missing paired handshake accepted")
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"StrictHostKeyChecking=yes", "BatchMode=yes", "ForwardAgent=no", "ClearAllForwardings=yes", "PermitLocalCommand=no", "proxy-worker --socket"} {
		if !strings.Contains(string(args), required) {
			t.Fatal("unsafe proxy invocation", required)
		}
	}
	if strings.Contains(string(args), "origin-bot") || strings.Contains(string(args), "native_activation") {
		t.Fatal("source/pair identity leaked into SSH argv")
	}
	for _, bad := range []string{"-oProxyCommand=bad", "fixture;command", "fixture\nother"} {
		if _, err := NewCodexSSHWorker(context.Background(), CodexSSHConfig{Destination: bad, Socket: "/private/worker.sock"}); err == nil {
			t.Fatal("SSH target injection accepted")
		}
	}
}
