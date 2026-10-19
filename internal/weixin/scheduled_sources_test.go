package weixin

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestScheduledReplyUsesOriginalChannelAndTerminalEvidence(t *testing.T) {
	for _, scenario := range []string{"original", "newer_input", "restart", "missing", "other_runtime", "desktop", "mixed", "expired_window", "repaired_peer", "unknown_send"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			// The callback is already completed when the phone polls; global state now
			// belongs to a newer turn and the hidden activation has left the visible list.
			snapshot := api.Snapshot{RuntimeOwner: "runtime", Connection: "ready", Phase: "working", CurrentTurn: "newer-turn", ScheduledSources: []api.ScheduledSource{{RuntimeOwner: "runtime", TurnKey: "scheduled-turn", RequestID: "original-input", Phase: "completed"}}, Items: []api.Item{{ID: "result", Kind: "assistant", TurnKey: "scheduled-turn", Status: "completed", Text: "Scheduled result"}}}
			host := Host{Snapshot: func() api.Snapshot { return snapshot }}
			b, err := Open(root, host)
			if err != nil {
				t.Fatal(err)
			}
			b.state.OwnerID = "fixture-peer"
			freshWindow(b, "original-input")
			b.state.Inputs["original-input"] = "accepted"
			want := true
			switch scenario {
			case "newer_input":
				freshWindow(b, "newer-input")
			case "restart":
				if err := b.saveLocked(); err != nil {
					t.Fatal(err)
				}
				b, err = Open(root, host)
				if err != nil {
					t.Fatal(err)
				}
			case "missing":
				snapshot.ScheduledSources = nil
				want = false
			case "other_runtime":
				snapshot.ScheduledSources[0].RuntimeOwner = "other"
				want = false
			case "desktop":
				snapshot.ScheduledSources[0].RequestID = "desktop-input"
				want = false
			case "mixed":
				snapshot.InputSources = []api.InputSource{{RuntimeOwner: "runtime", TurnKey: "scheduled-turn", RequestID: "desktop-steer"}}
				want = false
			case "expired_window":
				b.state.Window.InputAt = time.Now().Add(-13 * time.Hour).UnixMilli()
				want = false
			case "repaired_peer":
				b.state = emptyDocument()
				b.state.OwnerID = "new-peer"
				freshWindow(b, "new-input")
				want = false
			}
			server, texts := sentTexts(t, func(string) string {
				if scenario == "unknown_send" {
					return `not-json`
				}
				return `{"ret":0}`
			})
			defer server.Close()
			p := &protocol{client: server.Client(), base: server.URL, token: "fixture-token"}
			for range 3 {
				b.output(t.Context(), p)
			}
			expected := 0
			if want {
				expected = 1
			}
			if len(*texts) != expected {
				t.Fatalf("sent %d, want %d", len(*texts), expected)
			}
			if want && !strings.Contains((*texts)[0], "Scheduled result") {
				t.Fatal(*texts)
			}
			raw, _ := json.Marshal(snapshot)
			if strings.Contains(string(raw), "original-input") || strings.Contains(string(raw), "ScheduledSources") {
				t.Fatal("private provenance leaked to renderer")
			}
		})
	}
}
