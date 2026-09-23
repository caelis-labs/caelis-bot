package caelis

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

func TestIdleHeadWithoutTargetRefreshesNotebookOnce(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/control/v1/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: required})
		case "/api/control/v1/sessions/main/state":
			// Real idle heads retain the terminal status but release active targets.
			head := wire.SessionState{SessionId: "main"}
			head.Run.Status = pointer("completed")
			writeFixture(w, head)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	s.state.StoreID = "store"
	s.state.Connection.ExpiresAt = time.Now().Add(time.Hour)
	vault, err := notebook.OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	if err = os.WriteFile(filepath.Join(vault.Path(), "new-note.md"), []byte("# Native note"), 0600); err != nil {
		t.Fatal(err)
	}
	count := 0
	s.tools = &api.ToolConnection{FinishTurn: func() {
		count++
		if err := vault.Refresh(context.Background(), time.Now()); err != nil {
			t.Error(err)
		}
	}}
	v := s.state.Views["main"]
	v.Items = []api.Item{{Kind: "assistant", TurnKey: "native-turn", Text: "Done"}, {Kind: "tool", Text: "unscoped info"}}
	for range 2 {
		if err = s.refresh(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(vault.Path(), "INDEX.md"))
	if err != nil || !strings.Contains(string(raw), "new-note.md") || count != 1 {
		t.Fatalf("completion hook/index missing or duplicated: count=%d error=%v", count, err)
	}
	if s.Snapshot().CurrentTurn != "native-turn" {
		t.Fatal("lost canonical turn identity")
	}
}

func TestIdleWorkerReportsOnlyLatestCanonicalTurn(t *testing.T) {
	s := New(Options{Directory: filepath.Join(t.TempDir(), "bot")})
	s.state.Workers["worker"] = worker{Task: api.Task{ID: "worker"}, Binding: wire.ApplicationBinding{SessionId: "child"}}
	head := wire.SessionState{SessionId: "child"}
	head.Run.Status = pointer("completed")
	s.state.Views["child"] = &view{State: head, Items: []api.Item{{Kind: "assistant", TurnKey: "old", Text: "old result"}, {Kind: "assistant", TurnKey: "new", Text: "new result"}}}
	tasks := s.WorkStates()
	if len(tasks) != 1 || tasks[0].ExecutionKey != "new" || tasks[0].Task.Result != "new result" {
		t.Fatal("idle worker mixed previous turn into completion")
	}
}
