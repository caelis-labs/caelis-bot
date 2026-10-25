package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

// Verify the state-before-history path for one explicitly selected original
// task. Only GETs reach the native owner; all observation writes use TempDir.
func TestExistingWorkerConversationReadPath(t *testing.T) {
	profile, id := os.Getenv("CAELIS_BOT_WORKER_HISTORY_PROFILE"), os.Getenv("CAELIS_BOT_WORKER_HISTORY_TASK")
	if profile == "" || id == "" {
		t.Skip("set an existing profile and exact task for read-only acceptance")
	}
	b, err := loadBinding(filepath.Join(profile, "providers", "caelis", "application.json"))
	if err != nil {
		t.Fatal("cannot read original binding")
	}
	w, ok := b.Workers[id]
	if !ok || !w.Native || w.Binding.SessionId == "" || w.Start != nil || w.Submission != nil || b.Operations[w.PromptID].Outcome == "unknown" {
		t.Fatal("selected original worker is not eligible for this read-only probe")
	}
	d, token, err := Discover(api.RuntimeSettings{CaelisStore: b.StoreDirectory})
	if err != nil || d.InstanceID != b.InstanceID || d.PrincipalID != b.PrincipalID {
		t.Fatal("original Host identity unavailable")
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal("original owner client unavailable")
	}
	transport := c.http.Transport.(*http.Transport)
	path := "/api/control/v1/sessions/" + idPath(w.Binding.SessionId)
	// This preflight hook is also retained by the observation transport's
	// Clone. Always connect directly, and refuse anything beyond target GETs.
	transport.Proxy = func(in *http.Request) (*url.URL, error) {
		if in.Method != http.MethodGet || in.URL.Path != path+"/state" && in.URL.Path != path+"/reconnect" {
			return nil, errors.New("read-only probe refused a mutation or different native owner")
		}
		return nil, nil
	}
	defer transport.CloseIdleConnections()
	s := New(Options{Directory: filepath.Join(t.TempDir(), "private")})
	s.client, s.connected = c, true
	s.state.InstanceID, s.state.PrincipalID = b.InstanceID, b.PrincipalID
	// Keep only target ownership/status metadata. Do not copy cached chat,
	// other workers, account credentials or the live ledger to the probe.
	s.state.Workers[id] = worker{Native: true, Binding: wire.SessionBinding{SessionId: w.Binding.SessionId, PrincipalId: w.Binding.PrincipalId}, PromptID: w.PromptID, Task: api.Task{ID: id, Status: w.Task.Status, Outcome: w.Task.Outcome}}
	root := t.TempDir()
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
	if err != nil {
		t.Fatal("isolated task coordinator unavailable")
	}
	first, err := m.ReadTaskConversation(t.Context(), id)
	if err != nil {
		if err.Error() == "original worker activity unconfirmed" {
			t.Fatal("original worker activity unconfirmed (before history GET)")
		}
		t.Fatal("original native conversation read failed; no transcript logged")
	}
	second, err := m.ReadTaskConversation(t.Context(), id)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("original messages changed across read/reconnect")
	}
	roles := map[string]int{}
	for _, message := range first.Messages {
		roles[message.Role]++
	}
	if first.Status != "completed" || roles["user"] == 0 || roles["assistant"] == 0 {
		t.Fatal("selected completed worker has no completed exchange")
	}
	r, err := bot.New(filepath.Join(t.TempDir(), "bot.json"), nil)
	if err != nil || r.ConfigureTasks(m, nil) != nil {
		t.Fatal("isolated Bot task port unavailable")
	}
	last := first.Messages[len(first.Messages)-1]
	args, _ := json.Marshal(map[string]any{"request": map[string]any{"type": "read", "task": id, "messageId": last.ID}})
	out := r.CallTool(t.Context(), "bot_tasks", args)
	if out.IsError {
		t.Fatal("Bot could not read original final message; no transcript logged")
	}
	message, ok := out.StructuredContent["data"].(map[string]any)["message"].(api.TaskConversationMessage)
	if !ok || !reflect.DeepEqual(message, last) {
		t.Fatal("Bot final message differs from the shared Web task reader")
	}
	if turn, message := os.Getenv("CAELIS_BOT_WORKER_HISTORY_FINAL_TURN"), os.Getenv("CAELIS_BOT_WORKER_HISTORY_FINAL_MESSAGE"); turn != "" && message != "" {
		want := nativeWorkerMessageID(w.Binding.SessionId, turn, "assistant", message)
		last := first.Messages[len(first.Messages)-1]
		if last.ID != want || last.Role != "assistant" || last.Text == "" {
			t.Fatal("original final native assistant message is missing")
		}
	}
	t.Logf("exact original task: status=%s user=%d assistant=%d stable IDs after reconnect; no transcript logged", first.Status, roles["user"], roles["assistant"])
}

// Explicit, read-only acceptance against original completed workers. No model
// requests, receipts, live bindings or transcript content are written or logged.
func TestExistingWorkerConversations(t *testing.T) {
	profile := os.Getenv("CAELIS_BOT_WORKER_HISTORY_PROFILE")
	if profile == "" {
		t.Skip("set CAELIS_BOT_WORKER_HISTORY_PROFILE for read-only native acceptance")
	}
	b, err := loadBinding(filepath.Join(profile, "providers", "caelis", "application.json"))
	if err != nil {
		t.Fatal("cannot read original binding")
	}
	d, token, err := Discover(api.RuntimeSettings{CaelisStore: b.StoreDirectory})
	if err != nil || d.InstanceID != b.InstanceID || d.PrincipalID != b.PrincipalID {
		t.Fatal("original Host identity unavailable")
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.http.CloseIdleConnections()
	s := New(Options{Directory: t.TempDir()})
	s.client, s.connected, s.state = c, true, b
	count := 0
	for id, w := range b.Workers {
		if w.Binding.SessionId == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		started := time.Now()
		messages, readErr := s.ReadWorkConversation(ctx, id)
		if readErr != nil {
			// Sanitized delivery shape makes a protocol mismatch diagnosable
			// without printing the user's prompts, messages or native IDs.
			e := c.stream(ctx, "/sessions/"+idPath(w.Binding.SessionId)+"/reconnect?history_turns=64", "", func(f frame) error {
				var delivery wire.SessionFeedDelivery
				_ = json.Unmarshal(f.data, &delivery)
				t.Logf("event=%s kind=%s source=%s cursor=%t history=%t page=%d events=%d", f.event, delivery.Kind, delivery.Source, value(delivery.NextCursor) != "", value(delivery.HistoryBefore) != "", value(delivery.Page), len(delivery.Events))
				if delivery.Kind == "sync" {
					return historyComplete
				}
				return nil
			})
			if !errors.Is(e, historyComplete) {
				t.Logf("observation stopped (%T)", e)
			}
			t.Error(readErr)
		} else {
			roles := map[string]int{}
			for _, message := range messages {
				roles[message.Role]++
			}
			if roles["user"] == 0 || roles["assistant"] == 0 {
				t.Error("original worker has no completed exchange")
			}
			t.Logf("worker %d: user=%d assistant=%d elapsed=%s", count+1, roles["user"], roles["assistant"], time.Since(started).Round(time.Millisecond))
		}
		cancel()
		count++
	}
	if count == 0 {
		t.Fatal("no original workers to verify")
	}
}
