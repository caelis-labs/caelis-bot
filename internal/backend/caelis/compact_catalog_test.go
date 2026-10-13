package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
)

func TestCompactCatalogRestoresOriginalLegacyBinding(t *testing.T) {
	root := t.TempDir()
	r, e := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if e != nil {
		t.Fatal(e)
	}
	old := New(Options{Directory: filepath.Join(root, "binding")})
	original := r.LegacyToolConnection()
	if e = old.ConfigureBotTools(original); e != nil {
		t.Fatal(e)
	}
	version := old.profile.ToolsVersion
	if len(old.profile.Tools) != len(r.LegacyDefinitions()) {
		t.Fatal("historical fixture changed")
	}
	previous := New(Options{Directory: filepath.Join(root, "previous-binding")})
	historical := r.HistoricalToolConnections()[0]
	if e = previous.ConfigureBotTools(historical); e != nil {
		t.Fatal(e)
	}
	previousVersion := previous.profile.ToolsVersion
	restored := New(Options{Directory: filepath.Join(root, "binding")})
	if e = restored.ConfigureBotTools(&api.ToolConnection{Host: r, ApprovedTools: botpolicy.ApprovedTools()}); e != nil {
		t.Fatal(e)
	}
	if len(restored.profile.Tools) != len(r.Definitions()) || restored.profile.ToolsVersion == version {
		t.Fatal("new model catalog not replaced")
	}
	for _, d := range restored.profile.Tools {
		if d.Name == "bot_clock" || d.Name == "bot_task_start" || d.Name == "bot_memory" {
			t.Fatal("legacy tool advertised")
		}
	}
	dream := false
	for _, d := range restored.profile.Tools {
		dream = dream || d.Name == "bot_dream"
	}
	if !dream || restored.catalog["bot_memory"] != nil || restored.catalogs[version]["bot_memory"] != nil {
		t.Fatal("current or legacy Caelis catalog has wrong memory/Dream scope")
	}
	if previousVersion == version || restored.catalogs[previousVersion]["bot_clock"] == nil {
		t.Fatal("pre-retirement non-memory callback version was not restored")
	}
	if out := restored.catalogs[previousVersion]["bot_clock"].CallTool(t.Context(), "bot_clock", json.RawMessage(`{}`)); out.IsError {
		t.Fatal("pre-retirement clock callback lost")
	}
	if out := restored.catalogs[previousVersion]["bot_memory"].CallTool(t.Context(), "bot_memory", json.RawMessage(`{"operation":"recall"}`)); !out.IsError {
		t.Fatal("pre-retirement memory callback still executed")
	}
	handler := restored.catalogs[version]["bot_clock"]
	if handler == nil {
		t.Fatal("original version handler missing")
	}
	if out := handler.CallTool(t.Context(), "bot_clock", json.RawMessage(`{}`)); out.IsError {
		t.Fatal("original clock receipt not recoverable")
	}
	if out := r.CallTool(t.Context(), "bot_clock", json.RawMessage(`{}`)); !out.IsError {
		t.Fatal("new calls bypassed current catalog")
	}
	oldTasks := restored.catalogs[version]["bot_tasks"]
	newTasks := restored.catalogs[restored.profile.ToolsVersion]["bot_tasks"]
	if oldTasks == nil || newTasks == nil || oldTasks == newTasks {
		t.Fatal("same name merged original argument contract")
	}
}

// This digest was produced by ConfigureBotTools in the untouched 5903254d
// checkout. A pending callback from that catalog must bind to its old schema
// after an upgrade, including names whose schemas stayed superficially similar.
const base590CompactVersion = "cba748e6c1a88424908265e11bc6651ae0e12aff3a66eae13930b6c632731142"

type baseCompactTaskFixture struct{ starts atomic.Int32 }

func (*baseCompactTaskFixture) ListTasks() []api.Task { return nil }
func (f *baseCompactTaskFixture) StartTask(_ context.Context, in api.TaskStart) (api.Task, error) {
	f.starts.Add(1)
	return api.Task{ID: "original-task", Status: "working", Outcome: "accepted", Title: in.Title}, nil
}
func (*baseCompactTaskFixture) ReadTask(context.Context, string) (api.Task, error) {
	return api.Task{}, nil
}
func (*baseCompactTaskFixture) SendTask(context.Context, api.TaskMessage) (api.Task, error) {
	return api.Task{}, nil
}
func (*baseCompactTaskFixture) StopTask(context.Context, string) (api.Task, error) {
	return api.Task{}, nil
}

func TestBaseCompactPendingCallbackRecoversWithExactVersion(t *testing.T) {
	root := t.TempDir()
	r, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	worker := &baseCompactTaskFixture{}
	if err := r.ConfigureTasks(worker, nil); err != nil {
		t.Fatal(err)
	}
	var claims, results atomic.Int32
	call := wire.ApplicationCall{Id: "base590-pending-call", CallId: "model-call", SessionId: "main", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", TurnId: "turn", ItemId: "item", Name: "bot_delegate", ToolsVersion: base590CompactVersion, ConfigurationRevision: "1", Source: wire.ApplicationSource{Kind: "user", OperationId: "original-prompt"}, State: "pending", Arguments: map[string]any{"request": map[string]any{"type": "start", "requestId": "old-original-request", "title": "Original task", "prompt": "Synthetic assignment"}}}
	s := fixtureSession(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/claim") {
			claims.Add(1)
			claimed := call
			claimed.State = "claimed"
			writeFixture(w, claimed)
			return
		}
		if strings.HasSuffix(req.URL.Path, "/result") {
			results.Add(1)
			var receipt wire.ApplicationCallResult
			if err := json.NewDecoder(req.Body).Decode(&receipt); err != nil || receipt.Outcome != "succeeded" {
				t.Errorf("old callback lost: %+v %v", receipt, err)
			}
			writeFixture(w, struct{}{})
			return
		}
		t.Errorf("unexpected route %s", req.URL.Path)
	})
	if err := s.ConfigureBotTools(r.HistoricalToolConnections()[1]); err != nil {
		t.Fatal(err)
	}
	if s.profile.ToolsVersion != base590CompactVersion || len(s.profile.Tools) != 8 {
		t.Fatalf("historical catalog drifted from 5903254d: %s (%d tools)", s.profile.ToolsVersion, len(s.profile.Tools))
	}
	s.state.Operations[call.Source.OperationId] = journal{Path: "/application/sessions/main/prompt", Outcome: "accepted", Source: call.Source}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	upgraded := New(Options{Directory: filepath.Dir(s.path)})
	if err := upgraded.ConfigureBotTools(&api.ToolConnection{Host: r, ApprovedTools: botpolicy.ApprovedTools()}); err != nil {
		t.Fatal(err)
	}
	if upgraded.profile.ToolsVersion == base590CompactVersion || upgraded.catalogs[base590CompactVersion]["bot_delegate"] == nil || upgraded.catalogs[base590CompactVersion]["bot_schedule_update"] == nil {
		t.Fatal("old compact callback catalog not restored at its original digest")
	}
	upgraded.client = s.client
	if err := upgraded.handleCall(t.Context(), s.client, upgraded.state.Session, upgraded.state.Connection, call); err != nil {
		t.Fatal(err)
	}
	restarted := New(Options{Directory: filepath.Dir(s.path)})
	if err := restarted.ConfigureBotTools(&api.ToolConnection{Host: r, ApprovedTools: botpolicy.ApprovedTools()}); err != nil {
		t.Fatal(err)
	}
	restarted.client = s.client
	call.State = "claimed"
	if err := restarted.handleCall(t.Context(), s.client, restarted.state.Session, restarted.state.Connection, call); err != nil {
		t.Fatal(err)
	}
	if claims.Load() != 1 || results.Load() != 2 || worker.starts.Load() != 1 {
		t.Fatalf("persisted result was replayed or not delivered: claims=%d results=%d starts=%d", claims.Load(), results.Load(), worker.starts.Load())
	}
	if out := restarted.catalogs[base590CompactVersion]["bot_memory"].CallTool(t.Context(), "bot_memory", json.RawMessage(`{"operation":"recall"}`)); !out.IsError {
		t.Fatal("retired memory executed from historical compact binding")
	}
}
