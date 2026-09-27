package caelis

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestScreenCapabilityNegotiationAndCurrentModel(t *testing.T) {
	var reads atomic.Int32
	var selected atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/application/sessions/main/model-capabilities" || r.Header.Get("Authorization") != "Bearer SCOPED_TEST_TOKEN" {
			t.Error("lost read-only application scope", r.Method, r.URL.Path)
		}
		v := wire.ApplicationModelCapabilities{SessionId: "main", ConfigurationRevision: "18446744073709551615", Model: "custom/current"}
		if selected.Load() != 2 {
			v.ImageInput = pointer(selected.Load() == 1)
		}
		writeFixture(w, v)
	})
	if c, err := s.ImageInput(t.Context()); err != nil || c.State != "unknown" || reads.Load() != 0 {
		t.Fatal("old Host must remain usable without probing", c, err)
	}
	s.info.Capabilities = append(s.info.Capabilities, modelCapabilities)
	s.state.Configurations["main"] = wire.ApplicationConfiguration{Profile: wire.ApplicationProfile{Model: "stale/vision"}}
	for _, tc := range []struct {
		model int32
		state string
	}{{0, "unsupported"}, {1, "supported"}, {2, "unknown"}, {1, "supported"}} {
		selected.Store(tc.model)
		c, err := s.ImageInput(t.Context())
		if err != nil || c.State != tc.state || c.Model != "custom/current" {
			t.Fatal("must follow current selected model", c, err)
		}
	}
	s.connected = false
	if c, err := s.ImageInput(t.Context()); err != nil || c.State != "unknown" || reads.Load() != 4 {
		t.Fatal("disconnected capability", c, err)
	}
}

func TestScreenCapabilityRejectsUnboundOrUnavailableObservation(t *testing.T) {
	for _, body := range []string{
		`{"session_id":"foreign","configuration_revision":"1","model":"vision","image_input":true}`,
		`{"session_id":"main","configuration_revision":"0","model":"vision","image_input":true}`,
		`{"session_id":"main","configuration_revision":"18446744073709551616","model":"vision","image_input":true}`,
		`{"session_id":"main","configuration_revision":"1","model":"","image_input":true}`,
		`{"session_id":"main","model":"vision","image_input":true}`,
		`unavailable`,
	} {
		t.Run(body, func(t *testing.T) {
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if body == "unavailable" {
					w.WriteHeader(503)
				}
				_, _ = w.Write([]byte(body))
			})
			s.info.Capabilities = append(s.info.Capabilities, modelCapabilities)
			if c, err := s.ImageInput(t.Context()); err == nil || c.State != "unknown" {
				t.Fatal(c, err)
			}
		})
	}
}

func TestScreenSubmitRechecksModelAndPreservesUnknownReceipt(t *testing.T) {
	var enabled atomic.Bool
	var posts, reads atomic.Int32
	enabled.Store(true)
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			reads.Add(1)
			writeFixture(w, wire.ApplicationModelCapabilities{SessionId: "main", ConfigurationRevision: "2", Model: "current", ImageInput: pointer(enabled.Load())})
			return
		}
		posts.Add(1)
		var req wire.ApplicationPromptRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.SourceKind != "user" || len(req.ContentParts) != 2 || value(req.Input) != "screen context" {
			t.Error("lost screen input", req)
		}
		for _, p := range req.ContentParts {
			if p.Type != "image" || value(p.Data) == "" || !strings.HasPrefix(value(p.MimeType), "image/") {
				t.Error("lost image bytes", p)
			}
		}
		drop(w)
	})
	s.info.Capabilities = append(s.info.Capabilities, modelCapabilities)
	if c, err := s.ImageInput(t.Context()); err != nil || c.State != "supported" {
		t.Fatal(c, err)
	}
	enabled.Store(false)
	in := api.Submission{ID: "screen-fixture", Text: "screen context", ScreenInput: true}
	// Capability rejection must happen before attachment reads or preparation.
	if r, err := s.Submit(t.Context(), in, []api.InputFile{{Name: "missing.png", Path: "missing"}}); err != nil || r.Outcome != "rejected" || !strings.Contains(r.Message, "模型") || posts.Load() != 0 {
		t.Fatal(r, err)
	}
	enabled.Store(true)
	files := []api.InputFile{}
	for _, name := range []string{"selection.png", "context.jpg"} {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte("synthetic image bytes"), 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, api.InputFile{Name: name, Path: p})
	}
	r, err := s.Submit(t.Context(), in, files)
	if err != nil || r.Outcome != "unknown" || posts.Load() != 1 {
		t.Fatal(r, err)
	}
	observed := reads.Load()
	enabled.Store(false)
	r, err = s.Submit(t.Context(), in, files)
	if err != nil || r.Outcome != "unknown" || posts.Load() != 1 || reads.Load() != observed {
		t.Fatal("model switch must not replace original uncertain receipt or resend", r, err)
	}
}
