package machines

import (
	"bytes"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Opt-in production component -> real Service -> real peer handler regression.
// The only writable roots are test temp directories; no Runtime is configured.
func TestRemoteReviewUIService(t *testing.T) {
	root := os.Getenv("CAELIS_REMOTE_REVIEW_UI")
	if root == "" {
		t.Skip("explicit UI acceptance")
	}
	type scenario struct {
		s        *Service
		a, b     *httptest.Server
		original profile
		counts   map[string]int
	}
	var mu sync.Mutex
	scenarios := map[string]*scenario{}
	for _, name := range []string{"pending", "unknown"} {
		s, err := Open(t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		peer, err := workerpeer.Open(t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		item := &scenario{s: s, counts: map[string]int{}}
		handler := func(endpoint string) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				read := false
				if r.URL.Path == workerpeer.Prefix+"pair" {
					body, _ := io.ReadAll(r.Body)
					r.Body = io.NopCloser(bytes.NewReader(body))
					var request workerpeer.PairingRequest
					_ = json.Unmarshal(body, &request)
					read = request.Read
				}
				key := endpoint + ":" + r.Method + ":" + r.URL.Path
				if read {
					key += ":read"
				}
				mu.Lock()
				item.counts[key]++
				mu.Unlock()
				if name == "unknown" && endpoint == "A" && !read && r.Method == "POST" && r.URL.Path == workerpeer.Prefix+"pair" {
					rec := httptest.NewRecorder()
					peer.ServeHTTP(rec, r)
					conn, _, e := w.(http.Hijacker).Hijack()
					if e == nil {
						conn.Close()
					}
					return
				}
				peer.ServeHTTP(w, r)
			})
		}
		item.a = httptest.NewServer(handler("A"))
		defer item.a.Close()
		item.b = httptest.NewServer(handler("B"))
		defer item.b.Close()
		_, err = s.ConnectBot(t.Context(), api.BotConnectionInput{ID: name, Name: "review-" + name, Address: item.a.URL})
		if name == "pending" && err != nil {
			t.Fatal(err)
		}
		item.original = s.state.Profiles[name]
		if item.original.View.PairingStatus != name {
			t.Fatal("fixture did not reproduce wire state", name, item.original.View.PairingStatus, err)
		}
		if r := s.ManageMachine(api.MachineMutation{ID: "save-" + name, MachineID: name, Action: "save", Name: "review-" + name, Address: item.b.URL}); r.Status != "succeeded" {
			t.Fatal(r)
		}
		scenarios[name] = item
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in struct {
			Scenario string            `json:"scenario"`
			Method   string            `json:"method"`
			Args     []json.RawMessage `json:"args"`
		}
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&in) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		item := scenarios[in.Scenario]
		if item == nil {
			http.Error(w, "invalid", 400)
			return
		}
		decode := func(i int, v any) {
			if i < len(in.Args) {
				json.Unmarshal(in.Args[i], v)
			}
		}
		var value any
		var err error
		switch in.Method {
		case "State":
			item.s.mu.Lock()
			p := item.s.state.Profiles[in.Scenario]
			item.s.mu.Unlock()
			mu.Lock()
			defer mu.Unlock()
			value = map[string]any{"machine": machineView(p), "candidate": item.b.URL, "originalAddress": item.a.URL, "sameProof": p.Peer.Token == item.original.Peer.Token && p.Peer.Peer == item.original.Peer.Peer && p.PairingEpoch == item.original.PairingEpoch, "counts": item.counts}
		case "Machines":
			value = item.s.Machines()
		case "ConnectBot":
			var v api.BotConnectionInput
			decode(0, &v)
			value, err = item.s.ConnectBot(r.Context(), v)
		case "InspectMachine":
			var id string
			decode(0, &id)
			value, err = item.s.InspectMachine(r.Context(), id, "")
		case "ManageMachine":
			var v api.MachineMutation
			decode(0, &v)
			value = item.s.ManageMachine(v)
		case "MachineMutation":
			var id string
			decode(0, &id)
			value = item.s.MachineMutation(id)
		default:
			http.Error(w, "unsupported", 400)
			return
		}
		out := map[string]any{"value": value}
		if err != nil {
			out["error"] = err.Error()
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer server.Close()
	if err := os.WriteFile(filepath.Join(root, "ready.json"), []byte(`{"url":"`+server.URL+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, "stop")); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("UI fixture not stopped")
}
