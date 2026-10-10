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
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

func TestSharedRecoveryRediscoversOriginalStoreAndReadsOriginalReceipt(t *testing.T) {
	for _, outcome := range []wire.Outcome{"unknown", "accepted"} {
		t.Run(string(outcome), func(t *testing.T) {
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
					writeFixture(w, wire.ApplicationOperation{OperationId: "original-request", Outcome: outcome, Result: &wire.CommandResult{OperationId: "original-request", Outcome: outcome}})
				default:
					t.Errorf("unexpected recovery route %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			// Keep a stale, originally-bound discovery. CLI start publishes the
			// replacement endpoint only after public status confirms stopped.
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
			log := filepath.Join(settings.CaelisStore, "calls")
			settings.CLIPath = filepath.Join(settings.CaelisStore, "caelis")
			t.Setenv("RECOVERY_LOG", log)
			t.Setenv("RECOVERY_DISCOVERY", file)
			t.Setenv("RECOVERY_FRESH", string(fresh))
			script := "#!/bin/sh\numask 077\nprintf '%s\\n' \"$*\" >> \"$RECOVERY_LOG\"\ncase \"$1 $2\" in\n'service status') rm \"$RECOVERY_DISCOVERY\"; printf '{\"state\":\"stopped\"}\\n';;\n'service start') printf '%s' \"$RECOVERY_FRESH\" > \"$RECOVERY_DISCOVERY\";;\n*) exit 90;;\nesac\n"
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
			if reads != 1 || s.state.Operations["original-request"].Outcome != string(outcome) || s.state.Session.SessionId != binding.SessionId || s.state.InstanceID != "setup-instance" {
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

func TestExplicitSharedStopAndBotCloseDoNotStartHost(t *testing.T) {
	dir := t.TempDir()
	log, bin := filepath.Join(dir, "calls"), filepath.Join(dir, "caelis")
	t.Setenv("RECOVERY_LOG", log)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$RECOVERY_LOG\"\n[ \"$1 $2\" = 'service status' ] || exit 90\nprintf '{\"state\":\"stopped\"}\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Directory: filepath.Join(dir, "bot"), Settings: api.RuntimeSettings{CLIPath: bin, CaelisStore: dir}})
	s.state.StoreID, s.state.PrincipalID = "original-store", "original-owner"
	if err := s.connectWithRecovery(t.Context()); !errors.Is(err, caelisruntime.ErrServiceStopped) {
		t.Fatal(err)
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.connectWithRecovery(context.Background()); err == nil {
		t.Fatal("closed Bot restarted service")
	}
	b, err := os.ReadFile(log)
	if err != nil || strings.Count(string(b), "service status") != 1 || strings.Contains(string(b), "service start") {
		t.Fatal(string(b), err)
	}
}
