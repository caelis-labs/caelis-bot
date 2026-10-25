package caelis

import (
	"encoding/json"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestBotObservationChunksSurviveReadOnlyNativeRevisionChanges(t *testing.T) {
	head := nativeWorkHead("original-turn")
	reads := 0
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/state") {
			t.Errorf("observation changed native state: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		reads++
		writeFixture(w, head)
	})
	addServiceWorker(s, "task", "work", head)
	for i := 0; i < 12; i++ {
		s.state.Views["work"].Items = append(s.state.Views["work"].Items, api.Item{ID: fmt.Sprint("original-", i), Kind: "assistant", Status: "completed", Text: strings.Repeat("原文<&>", 300)})
	}
	root := t.TempDir()
	manager, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Stop()
	if err = runtime.ConfigureTasks(manager, nil); err != nil {
		t.Fatal(err)
	}
	cursor, digest, firstRevision := "", "", ""
	var original strings.Builder
	for range 300 {
		args, _ := json.Marshal(map[string]any{"request": map[string]any{"type": "observe", "task": "task", "cursor": cursor}})
		out := runtime.CallTool(t.Context(), "bot_tasks", args)
		if out.IsError {
			t.Fatal("stable observation cannot finish original chunks", out.StructuredContent)
		}
		data := out.StructuredContent["data"].(map[string]any)
		if digest == "" {
			digest = data["sha256"].(string)
			firstRevision = out.StructuredContent["nativeRevision"].(string)
		}
		if digest != data["sha256"] {
			t.Fatal("mixed original observation")
		}
		original.WriteString(data["json"].(string))
		cursor, _ = data["nextCursor"].(string)
		if cursor == "" {
			if data["complete"] != true || out.StructuredContent["nativeRevision"] == firstRevision || reads < 2 {
				t.Fatal("fixture did not advance native read revision")
			}
			var decoded struct {
				Items []api.Item `json:"items"`
			}
			if json.Unmarshal([]byte(original.String()), &decoded) != nil || len(decoded.Items) != 12 {
				t.Fatal("original observation lost items")
			}
			return
		}
	}
	t.Fatal("original observation did not finish")
}
