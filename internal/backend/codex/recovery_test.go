package codex

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReviewFactsDoNotCreateApprovalAuthority(t *testing.T) {
	s := NewSession(SessionOptions{StateFile: filepath.Join(t.TempDir(), "binding.json")})
	s.binding.ThreadID = "root"
	s.children["worker"] = true
	apply := func(thread, status string) {
		s.applyEvent(Notification{Method: "item/autoApprovalReview/completed", Params: raw(map[string]any{
			"threadId": thread, "turnId": "turn", "reviewId": "review",
			"review": map[string]string{"status": status, "rationale": "synthetic rationale"},
			"action": map[string]string{"type": "command", "command": "synthetic command"},
		})})
	}
	apply("foreign", "denied")
	if len(s.state.Reviews) != 0 {
		t.Fatal("accepted an unrelated review")
	}
	apply("worker", "denied")
	apply("worker", "inProgress")
	if len(s.state.Reviews) != 1 || s.state.Reviews[0].Status != "denied" || len(s.prompts) != 0 || len(s.state.Approvals) != 0 {
		t.Fatal("review was regressed or converted into user permission")
	}
}

func TestExpiredAuthenticationUsesNativeCodeAndPreservesBinding(t *testing.T) {
	s := NewSession(SessionOptions{StateFile: filepath.Join(t.TempDir(), "binding.json")})
	s.binding.ThreadID = "root"
	s.state.Connection = "ready"
	s.applyTurn(nativeTurn{ID: "turn", Status: "failed", Error: &turnError{Message: "synthetic", Info: raw("unauthorized")}}, false)
	s.update()
	if v := s.Snapshot(); v.Connection != "login" || v.CanSend || s.binding.ThreadID != "root" {
		t.Fatal("authentication recovery lost identity or enabled dispatch")
	}
	s.state.Connection = "ready"
	s.applyFailure(turnError{Message: "unauthorized in generated text", Info: raw("other")})
	if s.state.Connection != "ready" {
		t.Fatal("classified prose as authentication authority")
	}
}

func TestFinderDiscoveryUsesInstalledNativePackage(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS discovery")
	}
	root := filepath.Join(t.TempDir(), "@openai", "codex")
	entry := filepath.Join(root, "bin", "codex.js")
	target := map[string]string{"arm64": "aarch64-apple-darwin", "amd64": "x86_64-apple-darwin"}[runtime.GOARCH]
	binary := filepath.Join(root, "vendor", target, "bin", "codex")
	for _, p := range []string{entry, binary} {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("synthetic"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"@openai/codex"}`), 0600); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	if installedBinary(entry) != want {
		t.Fatal("Finder still requires Node on PATH")
	}
}

func TestConnectionFailureIsActionable(t *testing.T) {
	s := NewSession(SessionOptions{StateFile: filepath.Join(t.TempDir(), "binding.json")})
	s.binding.Pending = &pendingSubmission{ID: "unknown-write"}
	_ = s.connectionError("generic", errRuntimeMissing)
	v := s.Snapshot()
	if v.ConnectionIssue != "runtime_missing" || v.Phase != "unknown" || v.CanSend || s.binding.Pending.ID != "unknown-write" {
		t.Fatal("failure lost pending work")
	}
}

func TestProtocolIncompatibilityPreservesPendingWork(t *testing.T) {
	for _, cause := range []error{ErrProtocol, &NativeError{Code: -32601}, &NativeError{Code: -32602}} {
		s := NewSession(SessionOptions{StateFile: filepath.Join(t.TempDir(), "binding.json")})
		s.binding.ThreadID = "root"
		s.binding.Pending = &pendingSubmission{ID: "unknown-write"}
		_ = s.connectionError("generic", cause)
		v := s.Snapshot()
		if v.ConnectionIssue != "runtime_protocol" || v.CanSend || v.Phase != "unknown" || s.binding.ThreadID != "root" || s.binding.Pending.ID != "unknown-write" {
			t.Fatal("protocol failure discarded identity or permitted replay")
		}
	}
	if incompatibleProtocol(errors.New("unsupported version")) || incompatibleProtocol(&NativeError{Code: -32603}) {
		t.Fatal("inferred incompatibility from prose or an internal error")
	}
}
