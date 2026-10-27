package machines

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Opt-in browser fixture uses actual MachineSettings callbacks and local service.
// No production profile or external Runtime is involved.
func TestDeleteUIService(t *testing.T) {
	control := os.Getenv("CAELIS_DELETE_UI")
	if control == "" {
		t.Skip("explicit UI acceptance")
	}
	root := t.TempDir()
	path := filepath.Join(root, "machines.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"profileId":"isolated-ui","profiles":{"fixture-original":{"view":{"id":"fixture-original","name":"fedora","address":"fedora"}}},"routes":{"historical-task":"fixture-original"},"runtimes":{"historical-task":"codex"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	peer, err := workerpeer.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(peer)
	defer remote.Close()
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var in struct {
			Method string            `json:"method"`
			Args   []json.RawMessage `json:"args"`
		}
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&in) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		decode := func(n int, v any) {
			if n < len(in.Args) {
				json.Unmarshal(in.Args[n], v)
			}
		}
		var value any
		var failure error
		switch in.Method {
		case "Fault":
			var enabled bool
			decode(0, &enabled)
			s.registry = filepath.Join(root, "machines-v2.json")
			if enabled {
				s.registry = filepath.Join(path, "blocked.json")
			}
			value = true
		case "Machines":
			value = s.Machines()
		case "InspectMachine":
			var id string
			decode(0, &id)
			value, failure = s.InspectMachine(r.Context(), id, "")
		case "ManageMachine":
			var v api.MachineMutation
			decode(0, &v)
			value = s.ManageMachine(v)
		case "MachineMutation":
			var id string
			decode(0, &id)
			value = s.MachineMutation(id)
		case "ConnectBot":
			var v api.BotConnectionInput
			decode(0, &v)
			value, failure = s.ConnectBot(r.Context(), v)
		case "Restart":
			s.Close()
			s, failure = Open(root, nil)
			value = s.Machines()
		case "State":
			value = map[string]any{"machines": s.Machines(), "originalAbsent": s.state.Profiles["fixture-original"].View.ID == "", "originalTaskRoute": s.state.Routes["historical-task"], "target": remote.URL}
		default:
			http.Error(w, "unsupported", 400)
			return
		}
		out := map[string]any{"value": value}
		if failure != nil {
			out["error"] = failure.Error()
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer server.Close()
	if err = os.WriteFile(filepath.Join(control, "ready.json"), []byte(`{"url":"`+server.URL+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(20 * time.Minute); time.Now().Before(deadline); {
		if _, err = os.Stat(filepath.Join(control, "stop")); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("UI fixture not stopped")
}
