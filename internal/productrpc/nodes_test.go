package productrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
)

type wireNodes struct {
	api.NodeManagementController
	catalog api.NodeCatalog
	adds    int
}

func (n *wireNodes) NodeCatalog(context.Context) (api.NodeCatalog, error) { return n.catalog, nil }
func (n *wireNodes) AddNode(_ context.Context, in api.NodeAddRequest) (api.NodeAddResult, error) {
	if in.ExpectedRevision != n.catalog.Revision {
		return api.NodeAddResult{}, errors.New("stale catalog")
	}
	n.adds++
	node := api.NodeInfo{ID: "NODE-A", Label: in.Label, Join: in.Join, Runtimes: []api.NodeRuntime{{Backend: api.NodeCodex, Health: api.NodeHealthy}}}
	n.catalog.Nodes = append(n.catalog.Nodes, node)
	n.catalog.Revision = "2"
	return api.NodeAddResult{OperationID: in.OperationID, Outcome: "committed", Node: node}, nil
}
func (n *wireNodes) DetectNode(_ context.Context, id string) (api.NodeInfo, error) {
	return api.NodeInfo{ID: id, Join: api.NodeSSH}, nil
}

type wireWorkers struct {
	saved       backend.WorkerNodeConfig
	revision    uint64
	connections int
}

func (w *wireWorkers) Snapshot() backend.WorkerNodeSetup {
	return backend.WorkerNodeSetup{Revision: w.revision, Nodes: []backend.WorkerNodeView{{Config: backend.WorkerNodeConfig{ID: "NODE-A", Label: "Backup", Backend: "codex", SSH: "private-destination", Helper: "/private/helper", Socket: "/private/socket", Store: "/private/store", WorkspaceRoot: "/private/workspace"}, Issue: "private-worker-error", State: "ready", Connected: w.connections > 0}}}
}
func (w *wireWorkers) Save(c backend.WorkerNodeConfig, r uint64) (backend.WorkerNodeSetup, error) {
	if r != w.revision {
		return w.Snapshot(), errors.New("stale Worker revision")
	}
	w.saved = c
	w.revision++
	return w.Snapshot(), nil
}
func (w *wireWorkers) Probe(context.Context, string, uint64) (backend.WorkerNodeSetup, error) {
	return w.Snapshot(), ErrUnsupported
}
func (w *wireWorkers) Connect(context.Context, string, uint64) (backend.WorkerNodeSetup, error) {
	return w.Snapshot(), ErrUnsupported
}
func (w *wireWorkers) Disconnect(context.Context, string, uint64) (backend.WorkerNodeSetup, error) {
	return w.Snapshot(), ErrUnsupported
}
func (w *wireWorkers) ProbeTarget(context.Context, api.WorkTarget, uint64) (backend.WorkerNodeSetup, error) {
	return w.Snapshot(), nil
}
func (w *wireWorkers) ConnectTarget(_ context.Context, target api.WorkTarget, r uint64) (backend.WorkerNodeSetup, error) {
	if target.NodeID != "NODE-A" || target.Backend != "codex" || target.Role != api.RoleWorker || r != w.revision {
		return w.Snapshot(), ErrUnsupported
	}
	w.connections++
	w.revision++
	return w.Snapshot(), nil
}
func (w *wireWorkers) DisconnectTarget(context.Context, api.WorkTarget, uint64) (backend.WorkerNodeSetup, error) {
	return w.Snapshot(), nil
}

type wireNotebook struct {
	settings               backend.NotebookSyncSettings
	state                  notebooksync.State
	saves, syncs, switches int
	failSwitch             bool
}

func (n *wireNotebook) State() notebooksync.State              { return n.state }
func (n *wireNotebook) Settings() backend.NotebookSyncSettings { return n.settings }
func (n *wireNotebook) SaveSettings(_ context.Context, v backend.NotebookSyncSettings) (backend.NotebookSyncSettings, error) {
	n.saves++
	n.settings = v
	return v, nil
}
func (n *wireNotebook) Sync(context.Context, string) error {
	n.syncs++
	n.state.Targets[0].LastSuccess = "2026-10-02T00:00:00Z"
	return nil
}
func (n *wireNotebook) Switch(context.Context, string) error {
	n.switches++
	n.state.Targets[0].Phase = "stopped"
	if n.failSwitch {
		n.state.Targets[0].Error = "private-path /private/notebook"
		return errors.New("native stop unconfirmed")
	}
	n.state.Targets[0].Phase = "switched"
	return nil
}

type wireServicePort struct{ ServicePort }

func (wireServicePort) Snapshot() api.Snapshot { return api.Snapshot{Connection: "ready"} }
func nodesWireFixture(t *testing.T) (*Server, *Client, *wireNodes, *wireWorkers, *wireNotebook, Options) {
	t.Helper()
	nodes := &wireNodes{catalog: api.NodeCatalog{Revision: "1", Nodes: []api.NodeInfo{{ID: "local", Label: "This machine", Join: api.NodeLocal}}}}
	workers := &wireWorkers{revision: 1}
	notebook := &wireNotebook{settings: backend.NotebookSyncSettings{IntervalMinutes: 5, SourceNodeID: "local", Targets: []backend.NotebookBackupTarget{}}, state: notebooksync.State{SourceNodeID: "local", Targets: []notebooksync.Status{{NodeID: "NODE-A", Phase: "ready"}}}}
	service := backend.NewService(nil, nil, nil, nil, nil)
	service.SetNodeManagementController(nodes)
	service.ConfigureWorkerNodes(workers)
	service.ConfigureNotebookSync(notebook)
	port := wireServicePort{ServicePort{Service: service}}
	opts := Options{NodeID: "wire-owner", BotID: "wire-bot", Token: strings.Repeat("n", 64), JournalFile: filepath.Join(t.TempDir(), "receipts.json")}
	server, err := NewServer(port, opts)
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(server)
	t.Cleanup(host.Close)
	c, err := NewClient(ClientOptions{URL: host.URL, ExpectedNode: opts.NodeID, ExpectedBot: opts.BotID, Token: opts.Token})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if _, err := c.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	return server, c, nodes, workers, notebook, opts
}
func TestNodeManagementOfficialWireDispatchesExistingServiceAndRegisteredWorker(t *testing.T) {
	s, c, nodes, workers, notebook, _ := nodesWireFixture(t)
	if !s.Identity().Capabilities.NodeManagement {
		t.Fatal("management capability missing")
	}
	view, err := c.NodeManagementState(t.Context(), NodeQuery{Action: "catalog"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.ManageNodes(t.Context(), "enroll-original", NodeCommand{Action: "add-node", Add: &api.NodeAddRequest{OperationID: "enroll-original", Label: "Backup", Join: api.NodeSSH, SSHDestination: "existing-alias", ExpectedRevision: view.Catalog.Revision}})
	if err != nil || r.Outcome != "accepted" || r.NodeManagement.Enrollment.Node.ID != "NODE-A" {
		t.Fatal(r, err)
	}
	r, err = c.ManageNodes(t.Context(), "worker-original", NodeCommand{Action: "save-worker-node", Worker: &RegisteredWorkerSelection{NodeID: "NODE-A", Backend: api.NodeCodex, Revision: 1}})
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	if workers.saved.Transport != "registered-agent" || workers.saved.ID != "NODE-A" || workers.saved.Label != "Backup" || workers.saved.Helper != "" || workers.saved.SSH != "" || workers.saved.WorkspaceRoot != "" {
		t.Fatal("wire supplied native Worker route", workers.saved)
	}
	body, _ := json.Marshal(r)
	if bytes.Contains(body, []byte("/private/")) || bytes.Contains(body, []byte("private-worker-error")) || bytes.Contains(body, []byte("private-destination")) {
		t.Fatal("native Worker metadata leaked")
	}
	r, err = c.ManageNodes(t.Context(), "connect-original", NodeCommand{Action: "connect-worker-target", Worker: &RegisteredWorkerSelection{NodeID: "NODE-A", Backend: api.NodeCodex, Revision: r.NodeManagement.Workers.Revision}})
	if err != nil || r.Outcome != "accepted" || workers.connections != 1 {
		t.Fatal(r, err)
	}
	settings := backend.NotebookSyncSettings{Enabled: true, IntervalMinutes: 5, Targets: []backend.NotebookBackupTarget{{NodeID: "NODE-A", Backend: api.NodeCodex}}}
	for _, in := range []NodeCommand{{Action: "save-notebook-settings", NotebookSettings: &settings}, {Action: "sync-notebook", NodeID: "NODE-A"}, {Action: "switch-notebook-node", NodeID: "NODE-A"}} {
		r, err = c.ManageNodes(t.Context(), in.Action+"-original", in)
		if err != nil || r.Outcome != "accepted" {
			t.Fatal(r, err)
		}
	}
	if notebook.saves != 1 || notebook.syncs != 1 || notebook.switches != 1 || nodes.adds != 1 {
		t.Fatal("existing Service methods not dispatched")
	}
	// Replay is an existing receipt, never a second switch or a cached live view.
	r, err = c.ManageNodes(t.Context(), "switch-notebook-node-original", NodeCommand{Action: "switch-notebook-node", NodeID: "NODE-A"})
	if err != nil || r.Outcome != "accepted" || notebook.switches != 1 || r.NodeManagement != nil {
		t.Fatal("replayed switch or stale cached view", r, err)
	}
	view, err = c.NodeManagementState(t.Context(), NodeQuery{Action: "notebook"})
	if err != nil || view.NotebookState.Targets[0].Phase != "switched" || view.NotebookState.Targets[0].LastSuccess == "" {
		t.Fatal(view, err)
	}
}
func TestNodeManagementWireRetainsAuthenticationScopeAndClosedPayloads(t *testing.T) {
	s, c, nodes, _, _, opts := nodesWireFixture(t)
	scope, _ := c.scope()
	in := Command{Scope: scope, ID: "auth-test", Kind: "manage-nodes", NodeManagement: &NodeCommand{Action: "add-node", Add: &api.NodeAddRequest{OperationID: "auth-test", Join: api.NodeSSH, Label: "Backup", SSHDestination: "alias", ExpectedRevision: "1"}}}
	post := func(body []byte, token, origin string) int {
		req := httptest.NewRequest("POST", "/v1/commands", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Code
	}
	body, _ := json.Marshal(in)
	if post(body, "", "") != http.StatusUnauthorized || post(body, "Bearer "+opts.Token, "https://browser.invalid") != http.StatusUnauthorized {
		t.Fatal("management auth relaxed")
	}
	in.Generation = "other-generation"
	body, _ = json.Marshal(in)
	if post(body, "Bearer "+opts.Token, "") != http.StatusConflict {
		t.Fatal("wrong owner scope admitted")
	}
	in.Scope = scope
	in.NodeManagement.NodeID = "extra-payload"
	body, _ = json.Marshal(in)
	if post(body, "Bearer "+opts.Token, "") != http.StatusBadRequest {
		t.Fatal("mixed command payload admitted")
	}
	configuration := api.NodeManagementRequest{Guard: api.NodeEditGuard{NodeID: "local", Backend: api.NodeCodex, Revision: strings.Repeat("a", 64)}, Ref: api.NodeOperationRef{NodeID: "local", Backend: api.NodeCodex, OperationID: "guard-test"}, Change: &api.RuntimeConfigurationChange{Action: "conversation-model", ExpectedRevision: strings.Repeat("a", 64), Selection: api.WorkExecutionSettings{Model: "fixture-model"}}}
	configuration.Ref.RequestDigest, _ = nodeplane.ManagementDigest(configuration)
	in.ID, in.NodeManagement = "guard-test", &NodeCommand{Action: "configure-node", Configuration: &configuration}
	for _, invalid := range []string{"digest", "revision", "action", "shape"} {
		copy := configuration
		change := *configuration.Change
		copy.Change = &change
		switch invalid {
		case "digest":
			copy.Ref.RequestDigest = strings.Repeat("0", 64)
		case "revision":
			copy.Change.ExpectedRevision = "another-revision"
		case "action":
			copy.Change.Action = "arbitrary-call"
		case "shape":
			copy.Change.ID = "unrelated-role"
		}
		if invalid != "digest" {
			copy.Ref.RequestDigest, _ = nodeplane.ManagementDigest(copy)
		}
		in.NodeManagement.Configuration = &copy
		body, _ = json.Marshal(in)
		if post(body, "Bearer "+opts.Token, "") != http.StatusBadRequest {
			t.Fatal("invalid configuration admitted", invalid)
		}
	}
	raw := `{"botId":"` + scope.BotID + `","generation":"` + scope.Generation + `","id":"unsafe-worker","kind":"manage-nodes","nodeManagement":{"action":"save-worker-node","worker":{"nodeId":"NODE-A","backend":"codex","revision":1,"helper":"/arbitrary/command"}}}`
	if post([]byte(raw), "Bearer "+opts.Token, "") != http.StatusBadRequest || nodes.adds != 0 {
		t.Fatal("wire accepted native path or dispatched unauthorized add")
	}
}
func TestNodeManagementOfficialSSHStdioProjectionAndUnconfirmedSwitch(t *testing.T) {
	s, _, _, _, n, opts := nodesWireFixture(t)
	host := httptest.NewServer(s)
	defer host.Close()
	left, right := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = ProxyStdio(ctx, right, right, host.URL, opts.Token) }()
	c, err := NewStdioClient(StdioOptions{ExpectedNode: opts.NodeID, ExpectedBot: opts.BotID}, left)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	view, err := c.NodeManagementState(t.Context(), NodeQuery{Action: "workers"})
	if err != nil || view.Workers.Revision != 1 {
		t.Fatal(view, err)
	}
	n.failSwitch = true
	r, err := c.ManageNodes(t.Context(), "unconfirmed-original", NodeCommand{Action: "switch-notebook-node", NodeID: "NODE-A"})
	if err != nil || r.Outcome != "unknown" || r.NodeManagement.NotebookState.Targets[0].Phase == "switched" {
		t.Fatal("native stop failure claimed success", r, err)
	}
	body, _ := json.Marshal(r)
	if bytes.Contains(body, []byte("/private/")) {
		t.Fatal("native error leaked")
	}
	if _, err := c.ManageNodes(t.Context(), "unconfirmed-original", NodeCommand{Action: "switch-notebook-node", NodeID: "NODE-A"}); err != nil || n.switches != 1 {
		t.Fatal("uncertain switch replayed", err)
	}
}
