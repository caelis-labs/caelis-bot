package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
)

func TestNativeIdleResidentRenewal(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_BINARY") == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for isolated external Host acceptance")
	}
	for _, channel := range []string{"desktop", "telegram"} {
		t.Run(channel, func(t *testing.T) { testNativeIdleResidentRenewal(t, channel) })
	}
}

func testNativeIdleResidentRenewal(t *testing.T, channel string) {
	t.Helper()
	root := t.TempDir()
	archive := filepath.Join(root, "ChatHistory")
	now := time.Now()
	history := filepath.Join(archive, filepath.FromSlash(now.Format("2006/01/02")+".log"))
	var service *backend.Service
	ctx, s, resident, model := nativeResidentFixture(t, func(r *bot.Runtime) {
		r.ConfigureIdleConversation(func(ctx context.Context, id string) (time.Time, bool, error) {
			return service.BotConversationActivity(ctx, id, nil)
		})
	})
	service = backend.NewService(s, func([]string) ([]api.InputFile, error) { return nil, nil }, nil, nil, nil)
	service.ConfigureChat(filepath.Join(root, "chat.sqlite"))
	defer service.Shutdown()
	service.SetUserSubmitter(resident.SubmitUser)
	requestID := channel + ":synthetic:1"
	service.ObserveChat(api.Snapshot{Items: []api.Item{
		{ID: "original-input", RequestID: "case_idle_old", Kind: "user", Text: "Original fixture user message", Status: "accepted", SeenAt: time.Now().Add(-49 * time.Hour).UnixMicro()},
		{ID: "test-history", Kind: "assistant", Text: "CORE_NATIVE_HISTORY_FOUND", Status: "completed", SeenAt: now.UnixMicro()},
	}})
	service.StartBotHistoryArchive(archive, nil)
	waitAcceptance(t, ctx, func() bool {
		body, _ := os.ReadFile(history)
		return strings.Contains(string(body), "CORE_NATIVE_HISTORY_FOUND")
	})
	if err := os.WriteFile(filepath.Join(s.tools.NotebookDirectory, "MEMORY.md"), []byte("CORE_NATIVE_SHARED_MEMORY"), 0600); err != nil {
		t.Fatal(err)
	}
	submitAcceptance(t, ctx, s, "CASE_IDLE_OLD")
	original := s.ConversationState()
	wake := api.SystemMessage{ID: "idle-history-synthetic-wake", Source: api.SystemWorkerResult, Text: "CASE_IDLE_BACKGROUND"}
	if receipt, err := resident.SubmitSystem(ctx, wake); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("native system input did not reach the original resident", receipt, err)
	}
	waitTurn(t, ctx, s, original.Turn)
	if s.ConversationState().Session != original.Session {
		t.Fatal("background input replaced the resident")
	}
	// The App ingress regression exercises the handler that writes this receipt.
	service.ObserveChat(api.Snapshot{Items: []api.Item{
		{ID: "control-input:desktop-status", RequestID: "desktop-status", Kind: "user", Text: "/status", Status: "accepted"},
	}})
	submit := service.Submit
	if channel == "telegram" {
		submit = func(ctx context.Context, in api.Submission) (api.Receipt, error) {
			service.ObserveChat(api.Snapshot{Items: []api.Item{
				{ID: "channel-input:" + in.ID, RequestID: in.ID, Kind: "user", Text: in.Text, Status: "received"},
			}})
			return backend.SubmitRemote(ctx, service, in, nil)
		}
	}
	args, _ := json.Marshal(map[string]string{"path": history})
	model.set("CASE_IDLE_NEW", modelStep{Name: "js_exec", Args: map[string]any{
		"code": "await tools.search({name:'Read'}); return await tools.call('Read'," + string(args) + ");",
	}})
	before := s.Snapshot().CurrentTurn
	input := api.Submission{ID: requestID, Text: "CASE_IDLE_NEW"}
	if receipt, err := submit(ctx, input); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("native foreground input did not reach the replacement resident", receipt, err)
	}
	waitTurn(t, ctx, s, before)
	next := s.ConversationState()
	s.mu.Lock()
	oldPath := s.state.Operations[wake.ID].Path
	newPath := s.state.Operations[requestID].Path
	s.mu.Unlock()
	if next.Session == original.Session || newPath != "/sessions/"+idPath(next.Session)+"/prompt" ||
		oldPath != "/sessions/"+idPath(original.Session)+"/prompt" {
		t.Fatal("native receipts did not preserve distinct owners after 49 idle hours", oldPath, newPath)
	}
	calls := model.seen("CASE_IDLE_NEW")
	first, _ := json.Marshal(calls[0])
	result := fmt.Sprint(dtwLastToolOutput(calls))
	if !strings.Contains(string(first), "CORE_NATIVE_SHARED_MEMORY") {
		t.Fatal("replacement did not load MEMORY.md")
	}
	if strings.Contains(string(first), "CORE_NATIVE_HISTORY_FOUND") || strings.Contains(string(first), "CASE_IDLE_OLD") {
		t.Fatal("replacement received old chat in its initial model input")
	}
	if !strings.Contains(result, "CORE_NATIVE_HISTORY_FOUND") {
		t.Fatal("native Read did not retrieve the SQLite projection", result)
	}
	if receipt, err := submit(ctx, input); err != nil || receipt.Outcome != "accepted" ||
		s.ConversationState().Session != next.Session || len(model.seen("CASE_IDLE_NEW")) != len(calls) {
		t.Fatal("retry did not retain the original native result", receipt, err)
	}
	fresh := api.SystemMessage{ID: "idle-history-after-rotation", Source: api.SystemWorkerResult, Text: "CASE_IDLE_FRESH_BACKGROUND"}
	if receipt, err := resident.SubmitSystem(ctx, fresh); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("post-rotation Worker result did not reach the resident", receipt, err)
	}
	waitTurn(t, ctx, s, next.Turn)
	if receipt, err := resident.SubmitSystem(ctx, wake); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("pre-rotation Worker notice lost its receipt", receipt, err)
	}
	s.mu.Lock()
	retainedPath := s.state.Operations[wake.ID].Path
	freshPath := s.state.Operations[fresh.ID].Path
	s.mu.Unlock()
	if retainedPath != oldPath || freshPath != newPath || len(model.seen(wake.Text)) != 1 || len(model.seen(fresh.Text)) != 1 {
		t.Fatal("Worker notices replayed or crossed resident ownership", retainedPath, freshPath)
	}
	t.Logf("%s ingress after 49 idle hours and a desktop control receipt used the next native Session once; original and later Worker notices retained distinct Session receipts; native Read retrieved projected history", channel)
}

func TestNativeRetainedBackgroundGrant(t *testing.T) {
	ctx, s, _, model := nativeResidentFixture(t, func(*bot.Runtime) {})
	const authorization = "CASE_GRANT_AUTHORIZATION"
	model.set(authorization, modelStep{Reply: "Synthetic original authorization"})
	submitAcceptance(t, ctx, s, authorization)
	exerciseRetainedBackgroundGrant(t, ctx, s, model, strings.ToLower(authorization))
}

func exerciseRetainedBackgroundGrant(t *testing.T, ctx context.Context, s *Session, model *acceptanceModel, authorization string) {
	t.Helper()
	sid := s.ConversationState().Session
	// Pre-MCP schedules retain the authorization of their original user
	// operation. Seed the real Core grant before exercising that old path.
	grant, err := s.createGrant(ctx, sid, "fixture-reminder", authorization)
	if err != nil {
		t.Fatal("seed legacy Core grant", err)
	}
	s.mu.Lock()
	s.state.Grants["fixture-reminder"] = grantRecord{Grant: grant, Fingerprint: "original-user-schedule"}
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot().CurrentTurn
	in := api.Submission{ID: "wake-fixture", Text: "CASE_BACKGROUND", Scheduled: true}
	r, e := s.SubmitSystem(ctx, api.SystemMessage{Source: api.SystemReminder, ID: in.ID, Text: in.Text, OriginRequests: in.OriginRequests, Grants: []string{"fixture-reminder"}})
	if e != nil || r.Outcome != "accepted" {
		t.Fatal(r, e)
	}
	waitTurn(t, ctx, s, before)
	s.mu.Lock()
	wake := s.state.Operations[in.ID]
	s.mu.Unlock()
	if wake.Path != "/sessions/"+idPath(sid)+"/prompt" || wake.Source.Kind != "authorized_background" || value(wake.Source.GrantId) != grant.Id {
		t.Fatal("wake did not exercise its original Core grant", wake.Path, wake.Source.Kind)
	}
	r, e = s.SubmitSystem(ctx, api.SystemMessage{Source: api.SystemReminder, ID: in.ID, Text: in.Text, OriginRequests: in.OriginRequests, Grants: []string{"fixture-reminder"}})
	if e != nil || r.Outcome != "accepted" || len(model.seen("CASE_BACKGROUND")) != 1 {
		t.Fatal("background duplicated", e)
	}
	for _, item := range s.Snapshot().Items {
		if item.Kind == "user" && strings.Contains(item.Text, "CASE_BACKGROUND") {
			t.Fatal("scheduled prompt exposed by canonical feed")
		}
	}
	if !s.Snapshot().Scheduled {
		t.Fatal("native feed lost scheduled origin")
	}
	model.set("CASE_BACKGROUND_SKIP", modelStep{Reply: api.SilentReminder})
	before = s.Snapshot().CurrentTurn
	skipped, e := s.SubmitSystem(ctx, api.SystemMessage{Source: api.SystemReminder, ID: "wake-silent-fixture", Text: "CASE_BACKGROUND_SKIP", Grants: []string{"fixture-reminder"}})
	if e != nil || skipped.Outcome != "accepted" {
		t.Fatal(skipped, e)
	}
	waitTurn(t, ctx, s, before)
	quiet := s.Snapshot()
	if !quiet.Quiet {
		t.Fatal("silent final was not quiet")
	}
	for _, item := range quiet.Items {
		if item.TurnKey == quiet.CurrentTurn {
			t.Fatal("silent final appeared in transcript")
		}
	}
	if e = s.RevokeBackground(ctx, "fixture-reminder"); e != nil {
		t.Fatal(e)
	}
	var revoked wire.SessionBackgroundGrant
	path := "/sessions/" + idPath(sid) + "/background-grants/" + idPath(grant.Id)
	if e = s.client.json(ctx, "GET", path, nil, &revoked, "", ""); e != nil || !revoked.Revoked || revoked.Id != grant.Id ||
		revoked.SessionId != sid || revoked.AuthorizationOperationId != authorization {
		t.Fatal("original native grant was not revoked", e)
	}
	r, _ = s.SubmitSystem(ctx, api.SystemMessage{Source: api.SystemReminder, ID: "wake-revoked", Text: "CASE_REVOKED", Grants: []string{"fixture-reminder"}})
	if r.Outcome != "rejected" || len(model.seen("CASE_REVOKED")) != 0 {
		t.Fatal("revoked grant activated")
	}
	t.Log("real legacy grant accepted its original wake once; native revocation read back and further wake rejected")
}
