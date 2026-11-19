package caelis

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestDefaultNotebookTrustPreservesExplicitDecision(t *testing.T) {
	for _, level := range []wire.WorkspaceTrustLevel{wire.WorkspaceTrustLevelTrusted, wire.WorkspaceTrustLevelUntrusted} {
		t.Run(string(level), func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/status") || r.URL.Query().Get("cwd") != dir {
					t.Errorf("unexpected trust request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				writeFixture(w, wire.StatusSnapshot{Configuration: wire.StatusConfiguration{Revision: "10", WorkspaceTrust: level}})
			})
			s.tools = &api.ToolConnection{NotebookDirectory: dir}
			if err := s.defaultNotebookTrust(t.Context(), s.client); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnknownNotebookTrustOnlyReadsOriginalReceipt(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 3)
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		requests <- r.Method + " " + path
		switch path {
		case "/status":
			writeFixture(w, wire.StatusSnapshot{Configuration: wire.StatusConfiguration{Revision: "10", WorkspaceTrust: wire.WorkspaceTrustLevelUnknown}})
		case workspaceTrustPath:
			var req wire.WorkspaceTrustRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Cwd != dir || req.WorkspaceKey != dir || value(req.ExpectedRevision) != "10" || req.TrustLevel != wire.WorkspaceTrustLevelTrusted {
				t.Errorf("invalid versioned Notebook trust request: %+v (%v)", req, err)
			}
			drop(w)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s.tools = &api.ToolConnection{NotebookDirectory: dir}
	if err := s.defaultNotebookTrust(t.Context(), s.client); err == nil {
		t.Fatal("lost trust response was confirmed")
	}
	if snap := s.Snapshot(); !snap.CanSend {
		t.Fatal("optional trust write blocked chat")
	}
	if err := s.defaultNotebookTrust(t.Context(), s.client); err != nil {
		t.Fatal(err)
	}
	op := "notebook-trust-" + digest([]byte(dir+"\x0010"))
	for _, want := range []string{"GET /status", "POST " + workspaceTrustPath, "GET /operations/" + op} {
		select {
		case got := <-requests:
			if got != want {
				t.Fatalf("request = %q, want %q", got, want)
			}
		default:
			t.Fatalf("missing %s", want)
		}
	}
}
