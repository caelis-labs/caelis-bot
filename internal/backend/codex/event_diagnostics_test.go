package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func TestNonblockingNotificationsStayOutOfChatAndLeaveDiagnostics(t *testing.T) {
	s, f := sessionPair(t, "normal")
	dir := t.TempDir()
	s.mu.Lock()
	s.opts.Diagnostics = diagnosticlog.New(dir)
	s.children["worker"] = true
	s.mu.Unlock()
	if r := sendSynthetic(t, s, "event-diagnostics-proof"); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	for _, event := range []wireMessage{
		{Method: "mcpServer/startupStatus/updated", Params: json.RawMessage(`{"threadId":"worker","name":"context7","status":"failed","error":"No such file or directory; PRIVATE_SENTINEL"}`)},
		{Method: "mcpServer/oauthLogin/completed", Params: json.RawMessage(`{"success":false,"error":"PRIVATE_SENTINEL"}`)},
		{Method: "future/addon", Params: json.RawMessage(`{"error":"PRIVATE_SENTINEL","turn":7,"delta":{},"threadId":false}`)},
		{Method: "error", Params: json.RawMessage(`{"threadId":"thread-native","error":{"message":"PRIVATE_SENTINEL"},"willRetry":true}`)},
		{Method: "error", Params: json.RawMessage(`{"threadId":"worker","error":{"message":"PRIVATE_SENTINEL"},"willRetry":true}`)},
		{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"threadId":"thread-native","turnId":"run-native","itemId":"answer","delta":"done","error":"unused extension"}`)},
		{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread-native","turn":{"id":"run-native","status":"completed","items":[{"id":"reasoning","type":"reasoning","content":["PRIVATE_SENTINEL"]},{"id":"answer","type":"agentMessage","text":"done"}],"error":null}}`)},
	} {
		f.emit(event)
	}
	v := awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	if v.Message != "" || !v.CanSend || v.Connection != "ready" {
		t.Fatalf("nonblocking error escaped to UI: %+v", v)
	}
	b, err := os.ReadFile(filepath.Join(dir, "error.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "PRIVATE_SENTINEL") {
		t.Fatal("private payload in diagnostic log")
	}
	for _, want := range []string{"component_failed", "context7", "executable or file not found", "notification_ignored", "turn_error", "worker_error", "fingerprint", "worker"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing diagnostic %s", want)
		}
	}
}

func TestBlockingErrorsStillRequireUserAttention(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir(), StateFile: filepath.Join(t.TempDir(), "state.json"), Diagnostics: diagnosticlog.New(t.TempDir())})
	s.binding.ThreadID = "root"
	s.state.Connection = "ready"
	s.applyEvent(Notification{Method: "error", Params: json.RawMessage(`{"threadId":"root","willRetry":false,"error":{"message":"The requested work failed"}}`)})
	if s.state.Phase != "failed" || s.state.Message != "The requested work failed" {
		t.Fatal("blocking error hidden")
	}
	s.state.Message = ""
	s.state.Phase = "working"
	s.applyEvent(Notification{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"root","turn":{"id":"run","status":42}}`)})
	if s.state.Phase != "unknown" || s.state.Message == "" {
		t.Fatal("malformed lifecycle silently accepted")
	}
}

func TestEventDiagnosticsCorrelateNestedLifecycleTargets(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(SessionOptions{Directory: t.TempDir(), Diagnostics: diagnosticlog.New(dir)})
	s.binding.ThreadID = "root"
	s.applyEvent(Notification{Method: "turn/completed", Sequence: 9, Params: json.RawMessage(`{"threadId":"root","turn":{"id":"failed-run","status":"failed","error":{"message":"PRIVATE_SENTINEL"}}}`)})
	s.applyEvent(Notification{Method: "item/completed", Sequence: 10, Params: json.RawMessage(`{"threadId":"root","turnId":"failed-run","item":{"id":"failed-tool","type":"mcpToolCall","status":"failed"}}`)})
	b, err := os.ReadFile(filepath.Join(dir, "error.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d diagnostic records", len(lines))
	}
	for i, line := range lines {
		var record diagnosticlog.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record.Thread != "root" || record.Turn != "failed-run" || record.Sequence != uint64(9+i) || record.Fingerprint == "" {
			t.Fatalf("missing native correlation: %+v", record)
		}
		if i == 1 && record.Item != "failed-tool" {
			t.Fatal("missing nested tool id")
		}
	}
	if strings.Contains(string(b), "PRIVATE_SENTINEL") {
		t.Fatal("private native failure in diagnostic log")
	}
}

func TestNativeItemUnionIgnoresUnrelatedFieldsOnLiveAndReplay(t *testing.T) {
	for _, raw := range []string{
		`{"id":"r","type":"reasoning","content":["private reasoning"],"text":{}}`,
		`{"id":"future","type":"futureItem","content":"arbitrary","kind":{},"command":[]}`,
		`{"id":"a","type":"agentMessage","text":"answer","content":"future metadata","error":{}}`,
	} {
		var item nativeItem
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			t.Fatal(err)
		}
		var turn nativeTurn
		if err := json.Unmarshal([]byte(`{"id":"run","status":"completed","items":[`+raw+`]}`), &turn); err != nil {
			t.Fatal(err)
		}
	}
	var item nativeItem
	if err := json.Unmarshal([]byte(`{"id":"c","type":"commandExecution","command":[]}`), &item); err == nil {
		t.Fatal("malformed consumed field accepted")
	}
}
