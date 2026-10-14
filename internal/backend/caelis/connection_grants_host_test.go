package caelis

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Opt-in contract acceptance against an externally built Core binary. The
// Store, HOME and Host credential all live under one disposable test root;
// this creates no model connection, conversation or real user grant.
func TestHostConnectionGrantIntegration(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY to an isolated Core build")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := newExecutionFixture(t, root)
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	cmd := exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	cmd.Dir, cmd.Env = root, fixture.env
	log, err := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	log.Close()
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
	waitAcceptance(t, ctx, func() bool { _, _, err := Discover(settings); return err == nil })
	d, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	host, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	defer host.http.CloseIdleConnections()
	info, err := initialize(ctx, host)
	if err != nil {
		t.Fatal(err)
	}
	credential := "app-client-" + strings.Repeat("a", 64)
	register := "register-disposable-grant"
	var life wire.ApplicationConnection
	if err := host.json(ctx, "POST", "/applications/register", wire.ApplicationRegistration{OperationId: register, Name: "Bot grant fixture", Credential: credential}, &life, register, ""); err != nil {
		t.Fatal(err)
	}
	appClient, err := newClient(d.Endpoint, credential)
	if err != nil {
		t.Fatal(err)
	}
	defer appClient.http.CloseIdleConnections()
	appClient.schema.Store(host.schema.Load())
	private := filepath.Join(root, "bot-private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Directory: private, Settings: settings})
	s.client, s.info, s.connected = appClient, info, true
	s.state.StoreID, s.state.InstanceID, s.state.PrincipalID, s.state.Endpoint, s.state.Connection = value(info.StoreId), d.InstanceID, d.PrincipalID, d.Endpoint, life
	for _, scope := range []string{"bot", "worker"} {
		if err := s.AuthorizeFullAccess(ctx, scope); err != nil {
			t.Fatalf("%s Host grant failed: %v", scope, err)
		}
	}
	path := "/applications/connections/" + idPath(life.ConnectionId)
	var hostRead, appRead wire.ApplicationConnection
	if err := host.json(ctx, "GET", path, nil, &hostRead, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := appClient.json(ctx, "GET", "/application/connection", nil, &appRead, "", ""); err != nil {
		t.Fatal(err)
	}
	if hostRead.ConnectionId != life.ConnectionId || appRead.ConnectionId != life.ConnectionId || !value(hostRead.AllowUnreviewedApplicationExecution) || !value(hostRead.AllowWorkerFullAccess) || !value(appRead.AllowUnreviewedApplicationExecution) || !value(appRead.AllowWorkerFullAccess) {
		t.Fatal("temporary Host/App readback disagreed with original connection grant")
	}
	// This is an empty synthetic Worker with no model turn. Its immutable
	// creation permission and native state must agree after the Host grant.
	permissions := wire.WorkerPermissions{Mode: "danger-full-access", ApprovalMode: "auto-review"}
	workerOp := "create-disposable-full-worker"
	var worker wire.CommandResult
	if err := appClient.json(ctx, "POST", "/application/workers", wire.CreateWorkerRequest{OperationId: &workerOp, Cwd: root, Title: pointer("grant contract fixture"), Permissions: &permissions}, &worker, workerOp, ""); err != nil {
		t.Fatal("temporary full Worker creation failed", err)
	}
	if worker.OperationId != workerOp || worker.SessionId == nil || *worker.SessionId == "" || worker.WorkerPermissions == nil || *worker.WorkerPermissions != permissions {
		t.Fatal("temporary Worker receipt did not pin requested authority", worker)
	}
	var workerState wire.SessionState
	if err := appClient.json(ctx, "GET", "/sessions/"+idPath(*worker.SessionId)+"/state", nil, &workerState, "", ""); err != nil || workerState.SessionId != *worker.SessionId || workerState.WorkerPermissions == nil || *workerState.WorkerPermissions != permissions {
		t.Fatal("temporary Worker native state disagreed with creation receipt", err, workerState)
	}
	falseValue := false
	attempt := connectionGrantRequest{OperationID: "application-must-not-grant", ExpectedAllowUnreviewedApplicationExecution: pointer(true), ExpectedAllowWorkerFullAccess: pointer(true), AllowWorkerFullAccess: &falseValue}
	var rejected connectionGrantReceipt
	if err := appClient.json(ctx, "POST", path+"/grants", attempt, &rejected, attempt.OperationID, ""); !isRemoteStatus(err, 403) {
		t.Fatal("application credential raised or changed Host grant", err)
	}
	if err := host.json(ctx, "GET", path, nil, &hostRead, "", ""); err != nil || !value(hostRead.AllowWorkerFullAccess) || hostRead.ConnectionId != life.ConnectionId {
		t.Fatal("application attempt changed temporary Host grant", err)
	}
}
