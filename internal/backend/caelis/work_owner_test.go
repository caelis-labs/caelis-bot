package caelis

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestRetainedOwnerPreservesResidentConfigurationWithoutRebinding(t *testing.T) {
	reads := 0
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/application/sessions/main" {
			t.Errorf("inactive owner attempted resident mutation or configuration access: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		reads++
		writeFixture(w, wire.ApplicationBinding{SessionId: "main", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: wire.ApplicationProfile{Execution: "workspace-write", Instructions: "original-bot-instructions", ToolsVersion: "original-bot-tools"}})
	})
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	w, err := NewRetainedWorkOwner(Options{Directory: filepath.Dir(s.path)})
	if err != nil {
		t.Fatal(err)
	}
	w.engine.client = s.client
	if err = w.engine.ensureSession(t.Context(), s.client); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || w.engine.state.Session.Profile.ToolsVersion != "original-bot-tools" || w.engine.state.Session.Profile.Instructions != "original-bot-instructions" || len(w.engine.catalog) != 0 {
		t.Fatal("inactive Bot configuration or catalog changed")
	}
}
