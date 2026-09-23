package bot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botmemory"
)

func TestPersonalToolsShareProductStoreAndKeepFixedInstructions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.json")
	first, err := NewForRuntime(path, "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := botmemory.Open(t.Context(), filepath.Join(dir, "personal"), first.State().ID)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = first.ConfigurePersonal(store); err != nil {
		t.Fatal(err)
	}
	second, err := NewForRuntime(path, "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.State().PersonalVersion != 1 {
		t.Fatal("capability transition not persisted")
	}
	if err = second.ConfigurePersonal(store); err != nil {
		t.Fatal(err)
	}
	bridge, err := Serve(first)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	before := bridge.Config("synthetic")
	invoke := func(r *Runtime, name, raw string) api.ToolResult {
		t.Helper()
		v := r.CallTool(t.Context(), name, json.RawMessage(raw))
		if v.IsError {
			t.Fatal(v)
		}
		return v
	}
	invoke(first, "bot_memory", `{"operation":"remember","requestId":"remember-1","text":"synthetic-memory-evidence"}`)
	got := invoke(second, "bot_memory", `{"operation":"recall","query":"synthetic-memory-evidence"}`)
	var recall api.PersonalMemory
	if json.Unmarshal([]byte(got.Content[0]["text"]), &recall) != nil || len(recall.Evidence) != 1 {
		t.Fatal("recall lost evidence or promoted model text")
	}
	original := recall.Evidence[0].ID
	invoke(second, "bot_memory", `{"operation":"correct","requestId":"correct-tool-1","id":"`+original+`","text":"revised-evidence"}`)
	got = invoke(first, "bot_memory", `{"operation":"recall","query":"revised-evidence"}`)
	if json.Unmarshal([]byte(got.Content[0]["text"]), &recall) != nil || len(recall.Evidence) != 1 {
		t.Fatal("Bot cannot correct shared evidence")
	}
	invoke(first, "bot_memory", `{"operation":"forget","requestId":"forget-tool-1","id":"`+recall.Evidence[0].ID+`"}`)
	got = invoke(second, "bot_memory", `{"operation":"recall"}`)
	if json.Unmarshal([]byte(got.Content[0]["text"]), &recall) != nil || len(recall.Evidence) != 0 {
		t.Fatal("Bot cannot forget shared evidence")
	}
	after := bridge.Config("synthetic")
	if before.Instructions != after.Instructions || strings.Contains(after.Instructions, "synthetic-private-context") || strings.Contains(after.WorkerInstructions, "synthetic-memory-evidence") {
		t.Fatal("personal edits rewrote prefix or leaked into worker")
	}
	for _, raw := range []string{
		`{"operation":"remember","requestId":"evil-role-1","text":"claim","role":"structured_confirmation"}`,
		`{"operation":"remember","requestId":"evil-scope-1","text":"claim","scope":"workspace"}`,
		`{"operation":"confirm","text":"claim"}`,
		`{"operation":"forget","id":"foreign"}`,
	} {
		if out := first.CallTool(t.Context(), "bot_memory", json.RawMessage(raw)); !out.IsError {
			t.Fatal("model controlled memory governance")
		}
	}
	if out := first.CallTool(t.Context(), "bot_notebook", json.RawMessage(`{"operation":"read","id":"../outside"}`)); !out.IsError {
		t.Fatal("model path escaped")
	}
	if denied := forward(before.Env["CAELIS_BOT_ENDPOINT"], toolRequest{Token: "wrong-instance", Name: "bot_memory", Arguments: json.RawMessage(`{"operation":"recall"}`)}); !denied.IsError {
		t.Fatal("foreign bridge gained personal data")
	}
	first.Close()
	if out := first.CallTool(t.Context(), "bot_memory", json.RawMessage(`{"operation":"recall"}`)); !out.IsError {
		t.Fatal("closed tools admitted request")
	}
}

func TestPersonalIdentityNeverDefaultsMissingOrFutureState(t *testing.T) {
	for _, raw := range []string{`{"version":1,"schedules":[]}`, `{"version":1,"id":"stable","personalVersion":2,"schedules":[]}`} {
		path := filepath.Join(t.TempDir(), "bot.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewForRuntime(path, "codex", nil); err == nil {
			t.Fatal("invalid identity accepted")
		}
		after, _ := os.ReadFile(path)
		if string(after) != raw {
			t.Fatal("invalid identity rewritten")
		}
	}
}
