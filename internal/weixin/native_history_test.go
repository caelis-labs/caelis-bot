package weixin

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// The only seam supplies an online connection, without launching a real Host.
// All source metadata and user history come from the actual restored adapter.
type onlineCaelisFixture struct{ *caelis.Session }

func (s onlineCaelisFixture) Snapshot() api.Snapshot {
	v := s.Session.Snapshot()
	v.Connection = "ready"
	return v
}

func TestNativePastSessionCannotAuthorizeCompletion(t *testing.T) {
	for _, withScope := range []bool{false, true} {
		name := "missing_original_scope"
		if withScope {
			name = "preserved_original_scope"
		}
		t.Run(name, func(t *testing.T) { testNativePastSessionCannotAuthorizeCompletion(t, withScope) })
	}
}
func testNativePastSessionCannotAuthorizeCompletion(t *testing.T, withScope bool) {
	root := t.TempDir()
	nativeDir := filepath.Join(root, "native")
	state := map[string]any{
		"version": 2, "projectionVersion": 9, "instanceID": "fixture-instance", "principalID": "fixture-owner",
		"session":      wire.SessionBinding{SessionId: "current-session", PrincipalId: "fixture-owner"},
		"pastSessions": []string{"old-session"}, "calls": map[string]any{},
		"operations": map[string]any{"report": map[string]any{"path": "/sessions/current-session/prompt", "turnID": "completion", "outcome": "accepted", "source": wire.SessionSource{Kind: "application_summary"}}},
		"views": map[string]any{
			"old-session": map[string]any{"state": wire.SessionState{SessionId: "old-session"}, "seen": map[string]bool{}, "items": []api.Item{
				{ID: "old-user", Kind: "user", Status: "completed", TurnKey: "old-turn", RequestID: "old-wx-request", Text: "Fixture historical request"},
			}},
			"current-session": map[string]any{"state": wire.SessionState{SessionId: "current-session"}, "seen": map[string]bool{}, "turns": map[string]string{"completion": "completed"}, "items": []api.Item{
				{ID: "completion-answer", Kind: "assistant", Status: "completed", TurnKey: "completion", Text: "Fixture completion result"},
			}},
		},
	}
	if withScope {
		state["views"].(map[string]any)["old-session"].(map[string]any)["inputScopes"] = map[string]any{"old-user": map[string]string{"instanceID": "original-instance", "sessionID": "old-session", "turnID": "old-turn"}}
	}
	// Use the production private-state writer: POSIX mode bits alone do not
	// establish the protected per-user DACL required by the Windows adapter.
	if err := localstate.Write(filepath.Join(nativeDir, "application.json"), state); err != nil {
		t.Fatal(err)
	}
	native := caelis.New(caelis.Options{Directory: nativeDir, Settings: api.RuntimeSettings{CaelisStore: filepath.Join(root, "isolated-store")}})
	service := backend.NewService(onlineCaelisFixture{native}, nil, nil, nil, nil)
	service.ConfigureChat(filepath.Join(root, "chat.sqlite"))
	t.Cleanup(func() { _ = service.Shutdown() })
	service.ObserveChat(api.Snapshot{Items: []api.Item{{ID: "task-start:task", Kind: "assistant", Status: "completed", TurnKey: "old-turn", Text: "Fixture task", Task: &api.TaskPresentation{ID: "task", Title: "Fixture task"}}}})
	service.ObserveChat(native.Snapshot())
	snapshot := service.Snapshot()
	// Native user input waits for the local history merge. Native provenance
	// remains independently checked below and never comes from visible text.
	deadline := time.Now().Add(5 * time.Second)
	for len(snapshot.Items) < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		snapshot = service.Snapshot()
	}
	wantSources := 0
	if withScope {
		wantSources = 1
	}
	if len(snapshot.InputSources) != wantSources || len(snapshot.ReportSources) != 1 || len(snapshot.Items) != 3 {
		t.Fatalf("fixture failed to assemble native sources and chat: %+v", snapshot)
	}
	host := Host{Snapshot: service.Snapshot, TaskForReport: func(id string) string {
		if id == "report" {
			return "task"
		}
		return ""
	}}
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "fixture-peer"
	freshWindow(b, "current-unrelated-request")
	b.state.Inputs["old-wx-request"] = "accepted"
	b.state.TaskTurns["task"] = "old-turn"
	// No origin/owner was observed for that historical turn before restart.
	b.state.Outputs["item:task-start:task"] = outbound{State: "skip"}
	if err := b.saveLocked(); err != nil {
		t.Fatal(err)
	}
	b, err = Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	server, texts := sentTexts(t, nil)
	defer server.Close()
	for range 2 {
		for range 3 {
			b.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture-token"})
		}
		if len(b.state.Outputs) != 1 || len(b.state.Progress) != 0 || b.state.Window.Used != 0 {
			t.Fatal("unproven history reached outbox")
		}
		b, err = Open(root, host)
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(*texts) != 0 || b.state.TurnOwners["old-turn"] != "" || b.state.TurnOwners["completion"] != "" {
		t.Fatalf("native historical input authorized completion: sends=%d owners=%v", len(*texts), b.state.TurnOwners)
	}
}
