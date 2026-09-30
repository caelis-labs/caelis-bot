package caelis

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Synthetic owner Host fixture; its management credential remains target-side.
func TestTargetBootstrapReadOnlyProbeAndDurableScopedEnrollment(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "store")
	var posts atomic.Int32
	var modelAuth atomic.Int32
	ownerToken := "OWNER_FIXTURE_SECRET_NEVER_EXPORTED"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+ownerToken {
			t.Error("bootstrap lacks target-local owner scope")
		}
		switch r.URL.Path {
		case "/api/control/v1/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: workerRequired})
		case "/api/control/v1/completion/slash-arguments":
			reported := pointer(false)
			if modelAuth.Load() == 1 {
				reported = nil
			}
			if modelAuth.Load() == 2 {
				reported = pointer(true)
			}
			writeFixture(w, []wire.SlashArgCandidate{{Value: "fixture-model", NoAuth: reported, ModelSelection: &wire.ModelSelection{Current: pointer(true), Effort: "medium"}}})
		case "/api/control/v1/status":
			writeFixture(w, wire.StatusSnapshot{})
		case "/api/control/v1/applications/register":
			posts.Add(1)
			var req wire.ApplicationRegistration
			json.NewDecoder(r.Body).Decode(&req)
			file := filepath.Join(root, "runtime", "bot-worker-enrollments", digest([]byte(req.OperationId))+".json")
			if _, err := privateRead(file, 65536); err != nil {
				t.Error("registration preceded durable target-side intent")
			}
			writeFixture(w, wire.ApplicationConnection{ApplicationId: "app", ConnectionId: "connection", PrincipalId: "owner", ExpiresAt: time.Now().Add(time.Hour)})
		default:
			t.Error("unexpected target Host operation", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	service := filepath.Join(root, "runtime/service")
	if err := os.MkdirAll(service, 0700); err != nil {
		t.Fatal(err)
	}
	if err := privateWrite(filepath.Join(service, "discovery.json"), discovery{Schema: "caelis.control.service-discovery/v1", Endpoint: server.URL, InstanceID: "instance", PrincipalID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service, "auth.token"), []byte(ownerToken), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(req WorkerBootstrapRequest) (WorkerBootstrapResult, string, error) {
		raw, _ := json.Marshal(req)
		var output bytes.Buffer
		err := RunWorkerBootstrap(context.Background(), bytes.NewReader(raw), &output)
		var result WorkerBootstrapResult
		json.Unmarshal(output.Bytes(), &result)
		return result, output.String(), err
	}
	probe, raw, err := invoke(WorkerBootstrapRequest{Action: "probe", Store: root})
	if err != nil || strings.Contains(raw, ownerToken) || posts.Load() != 0 || probe.OS == "" || probe.Arch == "" || !probe.ModelConfigured || probe.ModelAuth != "reported_ready" || probe.Execution.Model != "fixture-model" {
		t.Fatal("probe mutation or credential exposure", err)
	}
	modelAuth.Store(1)
	unknown, _, err := invoke(WorkerBootstrapRequest{Action: "probe", Store: root})
	if err != nil || unknown.ModelAuth != "unknown" || !unknown.ModelConfigured {
		t.Fatal("configuration promoted to authentication")
	}
	modelAuth.Store(2)
	missing, _, err := invoke(WorkerBootstrapRequest{Action: "probe", Store: root})
	if err != nil || missing.ModelAuth != "reported_missing" {
		t.Fatal("missing model credentials hidden")
	}
	modelAuth.Store(0)
	if _, err = os.Stat(filepath.Join(root, "runtime", "bot-worker-enrollments")); !os.IsNotExist(err) {
		t.Fatal("probe created enrollment state")
	}
	req := WorkerBootstrapRequest{Action: "enroll", Store: root, StoreID: probe.StoreID, InstanceID: probe.InstanceID, PrincipalID: probe.PrincipalID, OperationID: "fixture-op", AppCredential: strings.Repeat("SCOPED_FIXTURE", 4)}
	result, raw, err := invoke(req)
	if err != nil || result.Connection == nil || strings.Contains(raw, req.AppCredential) || strings.Contains(raw, ownerToken) || posts.Load() != 1 {
		t.Fatal("enrollment scope output", err)
	}
	req.AppCredential = strings.Repeat("CHANGED", 6)
	if _, _, err = invoke(req); err == nil || posts.Load() != 1 {
		t.Fatal("changed credential reused enrollment intent")
	}
	req.InstanceID = "other"
	if _, _, err = invoke(req); err == nil || posts.Load() != 1 {
		t.Fatal("Host identity change admitted")
	}
	workspaceBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(workspaceBase, "workspaces")
	wr := WorkerBootstrapRequest{Action: "resolve_workspace", Store: root, StoreID: probe.StoreID, InstanceID: probe.InstanceID, PrincipalID: probe.PrincipalID, WorkspaceRoot: workspaceRoot, TaskID: "task"}
	result, _, err = invoke(wr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(result.Workspace); !os.IsNotExist(err) {
		t.Fatal("resolution allocated before ledger")
	}
	wr.Action = "prepare_workspace"
	wr.Workspace = result.Workspace
	result, _, err = invoke(wr)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(result.Workspace); err != nil || !info.IsDir() {
		t.Fatal("workspace not prepared")
	}
	link := filepath.Join(workspaceRoot, "redirected")
	if err = os.Symlink(result.Workspace, link); err != nil {
		t.Fatal(err)
	}
	wr.Selected = true
	wr.Workspace = link
	wr.Action = "resolve_workspace"
	if _, _, err = invoke(wr); err == nil {
		t.Fatal("redirected workspace admitted")
	}
}

func TestConcurrentEnrollmentIntentCannotOverwriteScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "intent.json")
	first := credential{StoreID: "store", PrincipalID: "owner", OperationID: "same-operation", Token: "first-synthetic-credential"}
	second := first
	second.Token = "second-synthetic-credential"
	// Both complete intents reach the publication boundary before either is linked.
	// This reproduces the missing-file race deterministically, rather than relying
	// on filesystem timing or hoping two independent helpers overlap their reads.
	reached, release := make(chan struct{}, 2), make(chan struct{})
	link := func(old, new string) error { reached <- struct{}{}; <-release; return os.Link(old, new) }
	results := make(chan error, 2)
	go func() { results <- publishEnrollmentIntent(path, first, link) }()
	go func() { results <- publishEnrollmentIntent(path, second, link) }()
	for range 2 {
		select {
		case <-reached:
		case <-time.After(3 * time.Second):
			t.Fatal("writers did not reach exclusive publication barrier")
		}
	}
	close(release)
	successes := 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				successes++
			}
		case <-time.After(3 * time.Second):
			t.Fatal("exclusive publication blocked")
		}
	}
	if successes != 1 {
		t.Fatal("different scopes did not select exactly one durable intent")
	}
	raw, err := privateRead(path, 65536)
	var winner credential
	if err != nil || json.Unmarshal(raw, &winner) != nil || winner != first && winner != second {
		t.Fatal("durable intent lost or mixed")
	}
	loser := first
	if winner == first {
		loser = second
	}
	if err = persistEnrollmentIntent(path, loser); err == nil {
		t.Fatal("different scope overwrote the winner")
	}
	if err = persistEnrollmentIntent(path, winner); err != nil {
		t.Fatal("exact original scope could not replay", err)
	}
	again, err := privateRead(path, 65536)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("immutable intent bytes changed")
	}
}
