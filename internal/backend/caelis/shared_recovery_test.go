//go:build !windows

package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestSharedRecoveryRediscoversOriginalStoreAndReadsOriginalReceipt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome wire.Outcome
		removed bool
	}{
		{"crashed unknown", "unknown", false},
		{"crashed accepted", "accepted", false},
		{"stopped unknown", "unknown", true},
		{"stopped accepted", "accepted", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			life := wire.ApplicationConnection{ApplicationId: "original-app", ConnectionId: "original-connection", PrincipalId: "local-owner", ExpiresAt: time.Now().Add(time.Hour)}
			binding := wire.ApplicationBinding{ApplicationId: life.ApplicationId, ConnectionId: life.ConnectionId, PrincipalId: life.PrincipalId, SessionId: "original-worker", Profile: wire.ApplicationProfile{Execution: "workspace-write"}}
			reads := 0
			settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("recovery replayed native mutation: %s %s", r.Method, r.URL.Path)
					http.Error(w, "mutation", 500)
					return
				}
				switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
				case "/initialize":
					writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("original-store"), InstanceId: pointer("setup-instance"), Capabilities: required})
				case "/application/connection":
					writeFixture(w, life)
				case "/application/sessions/original-worker":
					writeFixture(w, binding)
				case "/application/operations/original-request":
					reads++
					writeFixture(w, wire.ApplicationOperation{OperationId: "original-request", Outcome: tc.outcome, Result: &wire.CommandResult{OperationId: "original-request", Outcome: tc.outcome}})
				default:
					t.Errorf("unexpected recovery route %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			// Exercise both stale crash discovery and discovery removed by an
			// explicit service stop. Start publishes the same Store's new endpoint.
			file := filepath.Join(settings.CaelisStore, "runtime/service/discovery.json")
			fresh, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var d discovery
			if err = json.Unmarshal(fresh, &d); err != nil {
				t.Fatal(err)
			}
			d.Endpoint, d.InstanceID = "http://127.0.0.1:1", "original-instance"
			stale, _ := json.Marshal(d)
			if err = os.WriteFile(file, stale, 0600); err != nil {
				t.Fatal(err)
			}
			if tc.removed {
				if err = os.Remove(file); err != nil {
					t.Fatal(err)
				}
			}
			log := filepath.Join(settings.CaelisStore, "calls")
			settings.CLIPath = filepath.Join(settings.CaelisStore, "caelis")
			t.Setenv("RECOVERY_LOG", log)
			t.Setenv("RECOVERY_DISCOVERY", file)
			t.Setenv("RECOVERY_FRESH", string(fresh))
			script := "#!/bin/sh\numask 077\nprintf '%s\\n' \"$*\" >> \"$RECOVERY_LOG\"\ncase \"$1 $2\" in\n'service status') rm -f \"$RECOVERY_DISCOVERY\"; printf '{\"state\":\"stopped\"}\\n';;\n'service start') printf '%s' \"$RECOVERY_FRESH\" > \"$RECOVERY_DISCOVERY\";;\n*) exit 90;;\nesac\n"
			if err = os.WriteFile(settings.CLIPath, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "bot")
			s := New(Options{Directory: dir, Settings: settings})
			s.retainedWorkers = true
			s.state.StoreID, s.state.PrincipalID, s.state.InstanceID = "original-store", "local-owner", "original-instance"
			s.state.StoreDirectory = settings.CaelisStore
			s.state.Session, s.state.Connection = binding, life
			s.state.Operations["original-request"] = journal{Path: "/application/sessions/original-worker/prompt", Digest: "original-digest", Outcome: "unknown", Body: json.RawMessage(`{"input":"PRIVATE_ORIGINAL_INPUT"}`)}
			if err = privateWrite(secretPath(s.path), credential{StoreID: "original-store", PrincipalID: "local-owner", OperationID: "original-registration", Token: "original-token"}); err != nil {
				t.Fatal(err)
			}
			if err = s.saveLocked(); err != nil {
				t.Fatal(err)
			}
			if err = s.connectWithRecovery(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer s.client.http.CloseIdleConnections()
			if err = s.recoverOperations(t.Context()); err != nil {
				t.Fatal(err)
			}
			if reads != 1 || s.state.Operations["original-request"].Outcome != string(tc.outcome) || s.state.Session.SessionId != binding.SessionId || s.state.InstanceID != "setup-instance" {
				t.Fatal("recovery lost original identity or uncertainty", s.state)
			}
			b, err := os.ReadFile(log)
			want := "service status --store-dir " + settings.CaelisStore + " --format json\nservice start --store-dir " + settings.CaelisStore + " --format json\n"
			if err != nil || string(b) != want {
				t.Fatal("wrong store or duplicated lifecycle", string(b), err)
			}
			// Reopening with changed defaults still targets the saved original Store.
			changed := settings
			changed.CaelisStore = t.TempDir()
			loaded := New(Options{Directory: dir, Settings: changed})
			if loaded.settings.CaelisStore != settings.CaelisStore {
				t.Fatal("original Store replaced by current settings")
			}
			loaded.loadErr = errors.New("damaged binding")
			loaded.settings.CaelisStore = changed.CaelisStore
			if err := loaded.reloadOriginal(); err != nil || loaded.settings.CaelisStore != settings.CaelisStore {
				t.Fatal("repaired original binding did not restore its Store", err)
			}
		})
	}
}

func TestUnboundOrClosedBotDoesNotStartHost(t *testing.T) {
	dir := t.TempDir()
	log, bin := filepath.Join(dir, "calls"), filepath.Join(dir, "caelis")
	t.Setenv("RECOVERY_LOG", log)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$RECOVERY_LOG\"\nprintf '{\"state\":\"stopped\"}\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Directory: filepath.Join(dir, "bot"), Settings: api.RuntimeSettings{CLIPath: bin, CaelisStore: dir}})
	s.state.StoreID, s.state.PrincipalID = "original-store", "original-owner"
	// An old or incomplete binding has no saved original Store to maintain.
	if err := s.connectWithRecovery(t.Context()); err == nil {
		t.Fatal("incomplete binding started a Host")
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.connectWithRecovery(context.Background()); err == nil {
		t.Fatal("closed Bot restarted service")
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unbound or closed Bot ran shared lifecycle", err)
	}
}

func TestRunningHostHandshakeFailureDoesNotStartService(t *testing.T) {
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusUnauthorized)
	})
	log, bin := filepath.Join(settings.CaelisStore, "calls"), filepath.Join(settings.CaelisStore, "caelis")
	t.Setenv("RECOVERY_LOG", log)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$RECOVERY_LOG\"\n[ \"$1 $2\" = 'service status' ] || exit 90\nprintf '{\"state\":\"running\"}\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	settings.CLIPath = bin
	s := New(Options{Directory: filepath.Join(t.TempDir(), "bot"), Settings: settings})
	s.state.StoreID, s.state.PrincipalID, s.state.StoreDirectory = "original-store", "local-owner", settings.CaelisStore
	if err := s.connectWithRecovery(t.Context()); err == nil {
		t.Fatal("bad handshake was treated as a recovered connection")
	}
	b, err := os.ReadFile(log)
	if err != nil || string(b) != "service status --store-dir "+settings.CaelisStore+" --format json\n" {
		t.Fatal("handshake failure started a running Host", string(b), err)
	}
}
