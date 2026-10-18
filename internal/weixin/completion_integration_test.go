//go:build !windows

package weixin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
	"github.com/coder/websocket"
)

// This fixture exercises native projection -> activation filtering -> cached
// snapshots -> persisted chat assembly -> Weixin outbox. No pre-filter snapshot
// or hostNotice is injected into the channel, and no real account is contacted.
func TestCompletionThroughNativeSnapshotAndChat(t *testing.T) {
	for _, restart := range []bool{false, true} {
		name := "live"
		if restart {
			name = "restart_before_delivery"
		}
		t.Run(name, func(t *testing.T) {
			f := newCompletionRuntime(t)
			root := t.TempDir()
			opts := codex.SessionOptions{Socket: f.path, RequiredSocket: true, Binary: "/fixture/no-cli", Directory: filepath.Join(root, "work"), StateFile: filepath.Join(root, "binding.json")}
			s := codex.NewSession(opts)
			connect := func() {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				if err := s.Connect(ctx); err != nil {
					t.Fatal(err)
				}
			}
			connect()

			service := backend.NewService(s, nil, nil, nil, nil)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = s.Close(ctx)
				_ = service.Shutdown()
			})
			service.ConfigureChat(filepath.Join(root, "chat.sqlite"))
			control, err := textchannel.Open(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			host := Host{Snapshot: service.Snapshot, Submit: s.Submit, Recovery: func() api.RecoveryState { return api.RecoveryState{} }, TextControl: control, TaskForReport: func(id string) string {
				if id == "completion-report" {
					return "task-original"
				}
				return ""
			}}
			b, err := Open(root, host)
			if err != nil {
				t.Fatal(err)
			}
			b.state.OwnerID, b.state.BotID, b.state.BaseURL = "fixture-peer", "fixture-bot", defaultBase
			b.state.Window = sendWindow{OwnerID: "fixture-peer", InputID: "wx-request", ContextToken: "fixture-reply-context", InputAt: time.Now().UnixMilli()}
			b.state.Inbox = []inbound{{ID: "wx-request", ContextToken: "fixture-reply-context", Text: "Delegate fixture work"}}
			b.dispatch(t.Context())
			if b.state.Inputs["wx-request"] != "accepted" {
				t.Fatal("ingress was not accepted")
			}
			original := s.Snapshot()
			service.ObserveChat(original)
			service.ObserveChat(api.Snapshot{Items: []api.Item{{ID: "task-start:task-original", Kind: "assistant", TurnKey: original.CurrentTurn, Status: "completed", Text: "Worker started", Task: &api.TaskPresentation{ID: "task-original", Title: "Worker started", Status: "working"}}}})
			server, texts := sentTexts(t, nil)
			defer server.Close()
			p := &protocol{client: server.Client(), base: server.URL, token: "fixture-token"}
			b.output(t.Context(), p)
			f.complete(t, "wx-request", "Delegated")
			service.ObserveChat(awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" }))
			b.output(t.Context(), p)
			before := len(*texts)
			if before != 2 || (*texts)[1] != "Delegated" {
				t.Fatal("initial delegation route", *texts)
			}

			if receipt, err := s.SubmitReport(t.Context(), api.Submission{ID: "completion-report", Text: "PRIVATE internal worker notice"}); err != nil || receipt.Outcome != "accepted" {
				t.Fatal(receipt, err)
			}
			f.comment(t, "completion-report", "Checking completed work")
			live := awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool {
				for _, item := range v.Items {
					if item.Text == "Checking completed work" {
						return true
					}
				}
				return false
			})
			service.ObserveChat(live)
			assertHiddenCompletion(t, service.Snapshot())
			if !restart {
				b.output(t.Context(), p)
				if len(*texts) != before+1 || (*texts)[before] != "Checking completed work" {
					t.Fatal("commentary did not inherit source", *texts)
				}
			}
			f.complete(t, "completion-report", "Final completion result")
			terminal := awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
			service.ObserveChat(terminal)
			if restart {
				// Recover with no internal input in the native summary and a newer
				// desktop receipt/turn. Provenance and terminal phase must persist.
				if _, err := s.Submit(t.Context(), api.Submission{ID: "later-desktop", Text: "Unrelated"}, nil); err != nil {
					t.Fatal(err)
				}
				f.complete(t, "later-desktop", "Desktop result")
				awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
				if err := service.Shutdown(); err != nil {
					t.Fatal(err)
				}
				s = codex.NewSession(opts)
				connect()
				service = backend.NewService(s, nil, nil, nil, nil)
				service.ConfigureChat(filepath.Join(root, "chat.sqlite"))
				host.Snapshot = service.Snapshot
				b, err = Open(root, host)
				if err != nil {
					t.Fatal(err)
				}
			}
			assembled := service.Snapshot()
			deadline := time.Now().Add(5 * time.Second)
			for !strings.Contains(snapshotText(assembled), "Final completion result") && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
				assembled = service.Snapshot()
			}
			if !strings.Contains(snapshotText(assembled), "Final completion result") {
				t.Fatal("chat did not restore completion reply")
			}
			assertHiddenCompletion(t, assembled)
			b.output(t.Context(), p)
			for range 3 {
				b.output(t.Context(), p)
			}
			b, err = Open(root, host)
			if err != nil {
				t.Fatal(err)
			}
			b.output(t.Context(), p)
			countFinal, countProgress := 0, 0
			for _, body := range (*texts)[before:] {
				countFinal += strings.Count(body, "Final completion result")
				countProgress += strings.Count(body, "Checking completed work")
				if strings.Contains(body, "PRIVATE") || strings.Contains(body, "Desktop result") {
					t.Fatal("unrelated input leaked")
				}
			}
			if countFinal != 1 || countProgress != 1 {
				t.Fatalf("completion delivery counts final=%d commentary=%d", countFinal, countProgress)
			}
			origin, ok := control.OriginOf("wx-request")
			if !ok || origin.Channel != "weixin" || origin.Conversation != "fixture-peer\x00fixture-reply-context" || b.state.Window.OwnerID != "fixture-peer" || b.state.Window.ContextToken != "fixture-reply-context" {
				t.Fatal("original reply context changed")
			}
			for _, out := range b.state.Outputs {
				if out.Attempts > 0 && (out.ContextToken != "fixture-reply-context" || out.Attempts != 1 || out.State != "accepted") {
					t.Fatal("send context or dedupe changed")
				}
			}
		})
	}
}

func snapshotText(snapshot api.Snapshot) string {
	var text string
	for _, item := range snapshot.Items {
		text += item.Text
	}
	return text
}

func assertHiddenCompletion(t *testing.T, snapshot api.Snapshot) {
	t.Helper()
	if len(snapshot.ReportSources) != 1 || snapshot.ReportSources[0].ReportID != "completion-report" || snapshot.ReportSources[0].RuntimeOwner != snapshot.RuntimeOwner {
		t.Fatal("private provenance missing from assembled snapshot")
	}
	for _, item := range snapshot.Items {
		if item.Kind == "hostNotice" || item.RequestID == "completion-report" || strings.Contains(item.Text, "PRIVATE internal") {
			t.Fatal("internal input is visible")
		}
	}
	data, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(data), "ReportSources") || strings.Contains(string(data), "reportSources") || strings.Contains(string(data), snapshot.RuntimeOwner) {
		t.Fatal("private provenance leaked into renderer JSON")
	}
}

func awaitCompletionSnapshot(t *testing.T, s *codex.Session, match func(api.Snapshot) bool) api.Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if v := s.Snapshot(); match(v) {
			return v
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("native fixture snapshot did not converge")
	return api.Snapshot{}
}

type completionRuntime struct {
	path   string
	mu     sync.Mutex
	ws     *websocket.Conn
	turn   map[string]any
	thread string
	starts int
}

func newCompletionRuntime(t *testing.T) *completionRuntime {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "wx-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	f := &completionRuntime{path: filepath.Join(dir, "s.sock"), thread: "fixture-thread"}
	l, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		f.mu.Lock()
		f.ws = ws
		f.mu.Unlock()
		for {
			_, data, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params struct {
					ClientID string `json:"clientUserMessageId"`
					ThreadID string `json:"threadId"`
				} `json:"params"`
			}
			if json.Unmarshal(data, &req) != nil {
				return
			}
			if len(req.ID) == 0 {
				continue
			}
			result := map[string]any{}
			f.mu.Lock()
			switch req.Method {
			case "initialize":
				result["userAgent"] = "synthetic"
			case "account/read":
				result["requiresOpenaiAuth"] = true
				result["account"] = map[string]string{"type": "apiKey"}
			case "thread/start":
				if f.starts > 0 {
					f.thread += "-next"
				}
				f.starts++
				f.turn = nil
				result["thread"] = map[string]any{"id": f.thread, "turns": []any{}}
			case "thread/resume", "thread/read":
				result["thread"] = map[string]any{"id": req.Params.ThreadID, "turns": []any{}}
			case "thread/turns/list":
				result["data"] = []any{}
				if f.turn != nil {
					result["data"] = []any{f.turn}
				}
			case "turn/start":
				body := "User fixture request"
				if req.Params.ClientID == "completion-report" {
					body = "PRIVATE internal worker notice"
				}
				f.turn = map[string]any{"id": req.Params.ClientID, "status": "inProgress", "items": []any{map[string]any{"id": "input", "type": "userMessage", "clientId": req.Params.ClientID, "content": []any{map[string]string{"type": "text", "text": body}}}}}
				result["turn"] = f.turn
			case "model/list", "skills/list", "thread/backgroundTerminals/list":
				result["data"] = []any{}
			}
			out, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
			f.mu.Unlock()
			if ws.Write(r.Context(), websocket.MessageText, out) != nil {
				return
			}
		}
	})}
	go func() { _ = server.Serve(l) }()
	t.Cleanup(func() { _ = server.Close() })
	return f
}

func (f *completionRuntime) comment(t *testing.T, turn, body string) {
	t.Helper()
	f.mu.Lock()
	item := map[string]any{"id": "comment", "type": "agentMessage", "text": body, "phase": "commentary"}
	f.turn["items"] = append(f.turn["items"].([]any), item)
	data, _ := json.Marshal(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": f.thread, "turnId": turn, "item": item}})
	ws := f.ws
	f.mu.Unlock()
	if err := ws.Write(t.Context(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func (f *completionRuntime) complete(t *testing.T, turn, body string) {
	t.Helper()
	f.mu.Lock()
	f.turn["items"] = append(f.turn["items"].([]any), map[string]any{"id": "final", "type": "agentMessage", "text": body, "phase": "final_answer"})
	f.turn["status"] = "completed"
	data, _ := json.Marshal(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": f.thread, "turn": f.turn}})
	ws := f.ws
	f.mu.Unlock()
	if err := ws.Write(t.Context(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

// Accepted ingress without a previously observed turn owner must not inherit
// the next thread's scope through retained native history and a task card.
func TestCodexRenewalHistoryCannotAuthorizeCompletion(t *testing.T) {
	f := newCompletionRuntime(t)
	root := t.TempDir()
	s := codex.NewSession(codex.SessionOptions{Socket: f.path, RequiredSocket: true, Binary: "/fixture/no-cli", Directory: filepath.Join(root, "work"), StateFile: filepath.Join(root, "binding.json")})
	if err := s.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	service := backend.NewService(s, nil, nil, nil, nil)
	service.ConfigureChat(filepath.Join(root, "chat.sqlite"))
	t.Cleanup(func() { _ = service.Shutdown() })
	host := Host{Snapshot: service.Snapshot, Submit: s.Submit, Recovery: func() api.RecoveryState { return api.RecoveryState{} }, TaskForReport: func(id string) string {
		if id == "completion-report" {
			return "task-original"
		}
		return ""
	}}
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "fixture-peer"
	b.state.Window = sendWindow{OwnerID: "fixture-peer", InputID: "old-wx-request", ContextToken: "fixture-context", InputAt: time.Now().UnixMilli()}
	b.state.Inbox = []inbound{{ID: "old-wx-request", Text: "Fixture old work", ContextToken: "fixture-context"}}
	b.dispatch(t.Context())
	original := s.Snapshot()
	service.ObserveChat(original)
	service.ObserveChat(api.Snapshot{Items: []api.Item{{ID: "task-start:task-original", Kind: "assistant", Status: "completed", TurnKey: original.CurrentTurn, Text: "Fixture task", Task: &api.TaskPresentation{ID: "task-original", Title: "Fixture task"}}}})
	f.complete(t, "old-wx-request", "Old result")
	service.ObserveChat(awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" }))
	if receipt, err := s.SubmitDream(t.Context(), api.Submission{ID: "renewal-dream", Text: "Fixture maintenance"}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	f.complete(t, "renewal-dream", "Fixture maintenance result")
	awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	if err := s.RenewConversation(t.Context(), "renewal-dream", s.ConversationState().Session); err != nil {
		t.Fatal(err)
	}
	if receipt, err := s.SubmitReport(t.Context(), api.Submission{ID: "completion-report", Text: "PRIVATE internal worker notice"}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	f.complete(t, "completion-report", "Wrong-scope completion")
	service.ObserveChat(awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" }))
	assembled := service.Snapshot()
	assertHiddenCompletion(t, assembled)
	if assembled.RuntimeOwner == original.RuntimeOwner {
		t.Fatal("fixture did not renew")
	}
	if len(assembled.InputSources) != 1 || assembled.InputSources[0].RuntimeOwner != original.RuntimeOwner {
		t.Fatal("retained native input lost original scope")
	}
	if b.state.Inputs["old-wx-request"] != "accepted" || len(b.state.TurnOwners) != 0 {
		t.Fatal("fixture must start with accepted input and no observed turn scope")
	}
	if err := b.saveLocked(); err != nil {
		t.Fatal(err)
	}
	b, err = Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	server, texts := sentTexts(t, nil)
	defer server.Close()
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	for range 3 {
		b.output(t.Context(), p)
	}
	if len(*texts) != 0 || len(b.state.Outputs) != 0 || len(b.state.TurnOwners) != 0 {
		t.Fatal("retained prior-thread input authorized completion")
	}
	// A genuinely new input in the new native thread still establishes its own
	// source and delivers once, even alongside all the refused historical items.
	b.state.Window = sendWindow{OwnerID: "fixture-peer", InputID: "new-wx-request", ContextToken: "fixture-context", InputAt: time.Now().UnixMilli()}
	b.state.Inbox = []inbound{{ID: "new-wx-request", Text: "Fixture new work", ContextToken: "fixture-context"}}
	b.dispatch(t.Context())
	f.complete(t, "new-wx-request", "New request result")
	service.ObserveChat(awaitCompletionSnapshot(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" }))
	for range 3 {
		b.output(t.Context(), p)
	}
	b, err = Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.output(t.Context(), p)
	if len(*texts) != 1 || (*texts)[0] != "New request result" || b.state.TurnOwners[original.CurrentTurn] != "" {
		t.Fatal("new input failed or historical output replayed", *texts)
	}
}
