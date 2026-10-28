package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type sourceAdmissionFixture struct{ *workerAdmissionFixture }

func (f *sourceAdmissionFixture) WorkAdmission(ctx context.Context) error {
	return f.Session.WorkAdmission(ctx)
}

// Exercise the native source predicate through Manager and bot_delegate. Native
// work creation is a counted fixture; no real model or remote Worker is invoked.
func TestDelegateContinuesAuthorizedWorkFromReportContext(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected native request") })
	f := &sourceAdmissionFixture{&workerAdmissionFixture{Session: s}}
	root := t.TempDir()
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", f, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	r, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureTasks(m, nil); err != nil {
		t.Fatal(err)
	}
	invoke := func(kind, id string) api.ToolResult {
		ctx := context.WithValue(t.Context(), invocationKey{}, wire.SessionToolCall{SessionId: "main", PrincipalId: "owner", TurnId: "turn-" + kind, Source: wire.SessionSource{Kind: kind, OperationId: "operation-" + kind}})
		ctx = api.WithToolInvocation(ctx, api.ToolInvocation{Provider: "caelis", Session: "main", Turn: "turn-" + kind, CallID: id})
		args, _ := json.Marshal(map[string]string{"prompt": "synthetic assignment", "workspace": root})
		return r.CallTool(ctx, "bot_delegate", args)
	}
	for _, id := range []string{"first", "second"} {
		if out := invoke("user", id); out.IsError {
			t.Fatal(out)
		}
	}
	for _, kind := range []string{"application_summary", "external_material"} {
		if out := invoke(kind, "continue-plan"); out.IsError {
			t.Fatal("report provenance disabled normal delegation", out)
		}
	}

	// A new real user or validated scheduled source can still start independent work.
	for _, kind := range []string{"user", "authorized_background"} {
		if out := invoke(kind, "later"); out.IsError {
			t.Fatal(out)
		}
	}
	if f.starts != 6 {
		t.Fatal("incorrect dispatch count", f.starts)
	}
}
