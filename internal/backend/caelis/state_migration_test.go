package caelis

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestMigratedUnknownOperationReadsOriginalCoreReceiptWithoutReplay(t *testing.T) {
	const receipt = "retired-app-op-1f4af84426d67661384c072f4e068488c1a1bebbb2ec1869b716da0e71c92d3e"
	reads := 0
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/operations/"+receipt {
			t.Errorf("original operation redispatched or misrouted: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		reads++
		writeFixture(w, wire.CommandResult{OperationId: receipt, Outcome: "accepted", Target: &wire.CommandTarget{TurnId: pointer("original-turn")}})
	})
	s.state.Version = 1
	s.state.Operations["same"] = journal{Path: "/application/sessions/main/prompt", Outcome: "unknown", Digest: "original-digest", Body: json.RawMessage(`{"operation_id":"same","input":"private unchanged"}`), Source: wire.SessionSource{Kind: "user", OperationId: "same"}}
	raw, _ := json.Marshal(s.state)
	var legacy map[string]json.RawMessage
	_ = json.Unmarshal(raw, &legacy)
	legacy["connection"] = json.RawMessage(`{"principal_id":"owner","application_id":"app","connection_id":"connection"}`)
	if err := privateWrite(s.path, legacy); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	restored.client = s.client
	if restored.loadErr != nil {
		t.Fatal(restored.loadErr)
	}
	untouched, _ := os.ReadFile(s.path)
	if !bytes.Equal(before, untouched) {
		t.Fatal("read-only load wrote migration")
	}
	if err := restored.recoverOperations(t.Context()); err != nil {
		t.Fatal(err)
	}
	j := restored.state.Operations["same"]
	if reads != 1 || j.ReceiptID != receipt || j.Outcome != "accepted" || j.TurnID != "original-turn" || j.Source.OperationId != "same" {
		t.Fatalf("original identity lost: %+v, reads=%d", j, reads)
	}
	archive, err := os.ReadFile(s.path + ".v1-retired.json")
	if err != nil || !bytes.Contains(archive, []byte("application_id")) || !bytes.Contains(archive, []byte("private unchanged")) {
		t.Fatalf("retired intent not preserved: %v", err)
	}
	again := New(Options{Directory: filepath.Dir(s.path)})
	if again.loadErr != nil || again.state.Version != 2 || again.state.Operations["same"].ReceiptID != receipt {
		t.Fatalf("migration repeated or lost receipt: %v", again.loadErr)
	}
}

func TestConnectUsesHostIdentityWithoutEnrollmentOrRenewal(t *testing.T) {
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer HOST_SETUP_TOKEN" || r.Method != "GET" {
			t.Errorf("invented private authorization: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 400)
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("setup-instance"), Capabilities: required})
		case "/sessions/main/profile":
			writeFixture(w, wire.SessionBinding{SessionId: "main", PrincipalId: "local-owner", Profile: wire.SessionProfile{Execution: "workspace-write"}})
		default:
			t.Errorf("obsolete route: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	s := New(Options{Directory: filepath.Join(t.TempDir(), "bot"), Settings: settings})
	s.retainedWorkers = true
	s.state.Session.SessionId = "main"
	for range 2 {
		if err := s.connect(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	defer s.client.http.CloseIdleConnections()
	if s.state.PrincipalID != "local-owner" {
		t.Fatal("Host principal was replaced")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.path), "application-credential.json")); !os.IsNotExist(err) {
		t.Fatal("obsolete private credential created", err)
	}
}

func TestLostUploadUsesOriginalCommonReceiptWithoutSecondUpload(t *testing.T) {
	data := []byte("synthetic resource")
	descriptor := wire.SessionResource{Id: "original-resource", SessionId: "main", Name: "input.txt", MediaType: "text/plain", Size: len(data), Sha256: digest(data)}
	posts, reads := 0, 0
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/control/v1/sessions/main/resources":
			posts++
			drop(w)
		case r.Method == "GET" && r.URL.Path == "/api/control/v1/operations/upload-original":
			reads++
			writeFixture(w, wire.CommandResult{OperationId: "upload-original", Outcome: "committed", SessionId: pointer("main"), SessionResource: &descriptor})
		default:
			t.Errorf("unexpected recovery: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	if _, err := s.UploadResource(t.Context(), "upload-original", "input.txt", "text/plain", data); err == nil {
		t.Fatal("lost upload treated as committed")
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	restored.client = s.client
	got, err := restored.UploadResource(t.Context(), "upload-original", "input.txt", "text/plain", data)
	if err != nil || got.Id != descriptor.Id || posts != 1 || reads != 1 {
		t.Fatalf("upload replay or lost identity: %+v, %v; %d posts/%d reads", got, err, posts, reads)
	}
	if len(restored.state.Typed["upload-original"].Body) != 0 {
		t.Fatal("confirmed upload retained file bytes")
	}
	if _, err := restored.UploadResource(t.Context(), "upload-original", "input.txt", "text/plain", []byte("changed")); err == nil || posts != 1 {
		t.Fatal("changed intent reused original ID")
	}
}

func TestTypedCommandRejectionRemainsKnownAcrossRestart(t *testing.T) {
	posts := 0
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		posts++
		w.WriteHeader(http.StatusConflict)
		writeFixture(w, wire.CommandResult{OperationId: "rejected-update", Outcome: "conflicted", ErrorCode: "conflict"})
	})
	patch := map[string]any{"instructions": "same intent"}
	if _, err := s.UpdateConfiguration(t.Context(), "rejected-update", "1", patch); err == nil {
		t.Fatal("conflict was treated as committed")
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	restored.client = s.client
	if restored.state.Typed["rejected-update"].Outcome != "rejected" {
		t.Fatal("structured rejection became unknown")
	}
	if _, err := restored.UpdateConfiguration(t.Context(), "rejected-update", "1", patch); err == nil || posts != 1 {
		t.Fatal("rejected intent was redispatched")
	}
}
