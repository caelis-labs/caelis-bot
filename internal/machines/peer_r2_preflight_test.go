package machines

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func TestPeerFilePreflightTransfersMetadataAndDispatchSendsBytesOnce(t *testing.T) {
	target := newDualPeerTarget(t)
	s, m, _, machine, _ := dualPeerConnect(t, target)
	var preflight, dispatch atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"call" {
			b, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(b))
			var q struct {
				Action string                `json:"action"`
				Files  []api.WorkFileContent `json:"files"`
			}
			if json.Unmarshal(b, &q) != nil {
				t.Error("invalid peer input")
			}
			for _, f := range q.Files {
				if q.Action == "validate_files" {
					preflight.Add(int64(len(f.Data)))
				}
				if q.Action == "start" || q.Action == "send" {
					dispatch.Add(int64(len(f.Data)))
				}
			}
		}
		target.peer.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	s.mu.Lock()
	p := s.state.Profiles[machine]
	p.Peer.Address = proxy.URL
	p.View.Address = proxy.URL
	s.state.Profiles[machine] = p
	s.mu.Unlock()
	data := []byte("column,total\nA,42\n")
	path := filepath.Join(t.TempDir(), "selected.csv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	v, err := m.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "one-file-create", Prompt: "read selected CSV", Files: []api.InputFile{{Name: "selected.csv", Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "one-file-continue", Prompt: "append this CSV", Files: []api.InputFile{{Name: "selected.csv", Path: path}}}); err != nil {
		t.Fatal(err)
	}
	if preflight.Load() != 0 || dispatch.Load() != 2*int64(len(data)) {
		t.Fatal("attachment bytes crossed the network twice", preflight.Load(), dispatch.Load())
	}
}
