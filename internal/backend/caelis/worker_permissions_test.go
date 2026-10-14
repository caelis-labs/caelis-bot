package caelis

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"net/http"
	"strings"
	"testing"
)

func TestWorkerResumeChecksPinnedNativePermissionBeforePrompt(t *testing.T) {
	prompts := 0
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/prompt") {
			prompts++
			t.Fatal("permission mismatch dispatched original prompt")
		}
		if !strings.HasSuffix(r.URL.Path, "/sessions/worker/state") {
			t.Fatal("unexpected route", r.URL.Path)
		}
		writeFixture(w, wire.SessionState{SessionId: "worker", WorkerPermissions: &wire.WorkerPermissions{Mode: "workspace-write", ApprovalMode: "manual"}})
	})
	w := worker{Native: true, Binding: wire.ApplicationBinding{SessionId: "worker", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner"}, Task: api.Task{ID: "task", Workspace: t.TempDir()}, PromptID: "original-prompt", Start: &workerStart{Prompt: "original input", Permissions: &wire.WorkerPermissions{Mode: "danger-full-access", ApprovalMode: "auto-review"}}}
	s.state.Workers[w.Task.ID] = w
	if _, err := s.advanceWorker(t.Context(), w); err == nil || prompts != 0 || s.state.Workers[w.Task.ID].Start == nil {
		t.Fatal("mismatched native mode lost original intent", err, prompts)
	}
}
