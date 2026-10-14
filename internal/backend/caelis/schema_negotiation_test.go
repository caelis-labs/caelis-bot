package caelis

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"net/http"
	"slices"
	"testing"
)

func TestControlSchemaNegotiatesEveryRequestAndRejectsMissingEcho(t *testing.T) {
	calls := 0
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		caps := append(slices.Clone(required), "display-replacements-v1")
		info := wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: caps}
		switch r.URL.Path {
		case "/api/control/v1/initialize":
			if r.Header.Get("X-Caelis-Control-Schema") != "" {
				t.Fatal("legacy discovery declared schema")
			}
			writeFixture(w, info)
		case "/api/control/v1/schema/initialize":
			if r.Header.Get("X-Caelis-Control-Schema") != "1.0" || r.Header.Get("X-Caelis-Control-Client-Capabilities") != "display-replacements-v1" {
				t.Fatal("negotiated declaration missing")
			}
			w.Header().Set("X-Caelis-Control-Schema", "1.0")
			writeFixture(w, info)
		case "/api/control/v1/schema/status":
			calls++
			if r.Header.Get("X-Caelis-Control-Schema") != "1.0" {
				t.Fatal("later request lost declaration")
			}
			writeFixture(w, map[string]any{})
		default:
			t.Fatalf("unexpected route %s", r.URL.Path)
		}
	})
	if _, err := initialize(t.Context(), s.client); err != nil || !s.client.schema.Load() {
		t.Fatal("schema was not negotiated", err)
	}
	var out map[string]any
	if err := s.client.json(t.Context(), "GET", "/status", nil, &out, "", ""); err == nil || calls != 1 {
		t.Fatal("missing success echo was accepted", err)
	}
}
