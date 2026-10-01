package productrpc

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
)

// RegisteredWorkerSelection cannot supply a transport, SSH command, socket or
// path. Existing native enrollment resolves the exact Worker route.
type RegisteredWorkerSelection struct {
	NodeID   string          `json:"nodeId"`
	Backend  api.NodeBackend `json:"backend"`
	Revision uint64          `json:"revision"`
}

// NodeCommand maps explicit settings actions to the existing Service methods.
// Its outer Command supplies the inspected Bot scope and durable original ID.
type NodeCommand struct {
	Action           string                        `json:"action"`
	NodeID           string                        `json:"nodeId,omitempty"`
	Add              *api.NodeAddRequest           `json:"add,omitempty"`
	Configuration    *api.NodeManagementRequest    `json:"configuration,omitempty"`
	Worker           *RegisteredWorkerSelection    `json:"worker,omitempty"`
	NotebookSettings *backend.NotebookSyncSettings `json:"notebookSettings,omitempty"`
}
type NodeQuery struct {
	Scope
	Action    string                `json:"action"`
	NodeID    string                `json:"nodeId,omitempty"`
	Backend   api.NodeBackend       `json:"backend,omitempty"`
	Operation *api.NodeOperationRef `json:"operation,omitempty"`
}
type RegisteredWorkerView struct {
	NodeID    string                  `json:"nodeId"`
	Label     string                  `json:"label"`
	Backend   string                  `json:"backend"`
	State     string                  `json:"state"`
	Issue     string                  `json:"issue,omitempty"`
	Facts     backend.WorkerNodeFacts `json:"facts"`
	Connected bool                    `json:"connected"`
}
type RegisteredWorkerState struct {
	Revision uint64                 `json:"revision"`
	Nodes    []RegisteredWorkerView `json:"nodes"`
	Issue    string                 `json:"issue,omitempty"`
}
type NodeManagementView struct {
	Scope
	Catalog          *api.NodeCatalog              `json:"catalog,omitempty"`
	Node             *api.NodeInfo                 `json:"node,omitempty"`
	Enrollment       *api.NodeAddResult            `json:"enrollment,omitempty"`
	Configuration    *api.NodeRuntimeConfiguration `json:"configuration,omitempty"`
	Operation        *api.NodeOperationReceipt     `json:"operation,omitempty"`
	Workers          *RegisteredWorkerState        `json:"workers,omitempty"`
	NotebookSettings *backend.NotebookSyncSettings `json:"notebookSettings,omitempty"`
	NotebookState    *notebooksync.State           `json:"notebookState,omitempty"`
}

// Optional native port; the composed Service already implements these methods.
// This is never a Bot tool, nor an alternate lifecycle/enrollment implementation.
type NodeManagementPort interface {
	NodeCatalog(context.Context) (api.NodeCatalog, error)
	NodeRuntimeConfiguration(context.Context, string, api.NodeBackend) (api.NodeRuntimeConfiguration, error)
	ReconcileNodeOperation(context.Context, api.NodeOperationRef) (api.NodeOperationReceipt, error)
	ReconcileNodeEnrollment(context.Context, string) (api.NodeAddResult, error)
	AddNode(context.Context, api.NodeAddRequest) (api.NodeAddResult, error)
	DetectNode(context.Context, string) (api.NodeInfo, error)
	ChangeNodeConfiguration(context.Context, api.NodeManagementRequest) (api.NodeOperationReceipt, error)
	WorkerNodes() backend.WorkerNodeSetup
	SaveWorkerNode(backend.WorkerNodeConfig, uint64) (backend.WorkerNodeSetup, error)
	ProbeWorkerTarget(context.Context, api.WorkTarget, uint64) (backend.WorkerNodeSetup, error)
	ConnectWorkerTarget(context.Context, api.WorkTarget, uint64) (backend.WorkerNodeSetup, error)
	DisconnectWorkerTarget(context.Context, api.WorkTarget, uint64) (backend.WorkerNodeSetup, error)
	NotebookSyncSettings() (backend.NotebookSyncSettings, error)
	NotebookSyncState() (notebooksync.State, error)
	SaveNotebookSyncSettings(context.Context, backend.NotebookSyncSettings) (backend.NotebookSyncSettings, error)
	SyncNotebook(context.Context, string) (notebooksync.State, error)
	SwitchNotebookNode(context.Context, string) (notebooksync.State, error)
}

func nodeBackend(b api.NodeBackend) bool { return b == api.NodeCodex || b == api.NodeCaelis }
func validNodeCommand(c NodeCommand, id string) bool {
	n := 0
	for _, set := range []bool{c.NodeID != "", c.Add != nil, c.Configuration != nil, c.Worker != nil, c.NotebookSettings != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return false
	}
	switch c.Action {
	case "add-node":
		return c.Add != nil && c.Add.OperationID == id && c.Add.Join == api.NodeSSH && publicManagementText(c.Add.Label, 128) && strings.TrimSpace(c.Add.Label) != "" && c.Add.SSHDestination != "" && publicManagementText(c.Add.SSHDestination, 256) && c.Add.ExpectedRevision != "" && publicManagementText(c.Add.ExpectedRevision, 256)
	case "detect-node", "sync-notebook", "switch-notebook-node":
		return identifier.MatchString(c.NodeID)
	case "save-worker-node", "probe-worker-target", "connect-worker-target", "disconnect-worker-target":
		return c.Worker != nil && identifier.MatchString(c.Worker.NodeID) && nodeBackend(c.Worker.Backend) && c.Worker.Revision > 0
	case "save-notebook-settings":
		if c.NotebookSettings == nil {
			return false
		}
		v := c.NotebookSettings
		if v.IntervalMinutes < 1 || v.IntervalMinutes > 1440 || len(v.Targets) > 16 || v.Enabled && len(v.Targets) == 0 {
			return false
		}
		seen := map[string]bool{}
		for _, t := range v.Targets {
			if !identifier.MatchString(t.NodeID) || !nodeBackend(t.Backend) || seen[t.NodeID] {
				return false
			}
			seen[t.NodeID] = true
		}
		return true
	case "configure-node":
		if c.Configuration == nil {
			return false
		}
		v := c.Configuration
		if !identifier.MatchString(v.Guard.NodeID) || !nodeBackend(v.Guard.Backend) || v.Guard.Revision == "" || !publicManagementText(v.Guard.Revision, 256) || v.Ref.NodeID != v.Guard.NodeID || v.Ref.Backend != v.Guard.Backend || v.Ref.OperationID != id || nodeplane.ValidateManagementRequest(*v) != nil {
			return false
		}
		if v.Change != nil && v.Installation == nil {
			return validNodeConfiguration(*v.Change)
		}
		if v.Installation != nil && v.Change == nil {
			switch v.Installation.Action {
			case api.NodeDetect, api.NodeCheckUpdate, api.NodeInstall, api.NodeUpdate:
				return publicManagementText(v.Installation.Version, 128) && publicManagementText(v.Installation.ExpectedVersion, 128)
			}
		}
	}
	return false
}

// Node revisions are opaque Service edit guards, including Codex SHA256 values.
// Preserve the public field bounds and closed payload shapes used by settings.
func validNodeConfiguration(c api.RuntimeConfigurationChange) bool {
	if !publicManagementText(c.ExpectedRevision, 256) || !publicManagementText(c.ID, 256) || !publicManagementText(c.Name, 256) || len(c.Description) > 8192 || strings.ContainsRune(c.Description, '\x00') || !publicManagementText(c.Selection.Model, 512) || !publicManagementText(c.Selection.Effort, 64) || !publicManagementText(c.Selection.ServiceTier, 64) {
		return false
	}
	empty := c.Selection == (api.WorkExecutionSettings{})
	switch c.Action {
	case "conversation-model", "worker-model":
		return c.ID == "" && c.Name == "" && c.Description == ""
	case "main":
		return c.Selection.Model != "" && c.ID == "" && c.Name == "" && c.Description == ""
	case "bind":
		return c.ID != "" && c.Selection.Model != "" && c.Name == "" && c.Description == ""
	case "reset", "delete-role", "remove-model", "disconnect-agent":
		return c.ID != "" && c.Name == "" && c.Description == "" && empty
	case "create-role":
		return c.ID != "" && c.Name == "" && empty
	case "save-set", "apply-set", "delete-set":
		return c.Name != "" && c.ID == "" && c.Description == "" && empty
	}
	return false
}
func validNodeQuery(q NodeQuery) bool {
	switch q.Action {
	case "catalog", "workers", "notebook":
		return q.NodeID == "" && q.Backend == "" && q.Operation == nil
	case "configuration":
		return identifier.MatchString(q.NodeID) && nodeBackend(q.Backend) && q.Operation == nil
	case "enrollment":
		return identifier.MatchString(q.NodeID) && q.Backend == "" && q.Operation == nil
	case "operation":
		return q.NodeID == "" && q.Backend == "" && q.Operation != nil && identifier.MatchString(q.Operation.NodeID) && nodeBackend(q.Operation.Backend) && identifier.MatchString(q.Operation.OperationID) && nodeplane.ValidateOperationRef(*q.Operation) == nil
	}
	return false
}
func projectWorkers(v backend.WorkerNodeSetup) RegisteredWorkerState {
	out := RegisteredWorkerState{Revision: v.Revision, Nodes: []RegisteredWorkerView{}}
	if v.Issue != "" {
		out.Issue = "unavailable"
	}
	for _, n := range v.Nodes {
		row := RegisteredWorkerView{NodeID: n.Config.ID, Label: n.Config.Label, Backend: n.Config.Backend, State: n.State, Facts: n.Facts, Connected: n.Connected}
		if n.Issue != "" {
			row.Issue = "unavailable"
		}
		out.Nodes = append(out.Nodes, row)
	}
	return out
}
func projectNotebook(v notebooksync.State) notebooksync.State {
	v.Targets = append([]notebooksync.Status{}, v.Targets...)
	for i := range v.Targets {
		if v.Targets[i].Error != "" {
			v.Targets[i].Error = "backup-or-switch-unconfirmed"
		}
	}
	return v
}
func projectNodeView(v NodeManagementView) NodeManagementView {
	if v.NotebookState != nil {
		s := projectNotebook(*v.NotebookState)
		v.NotebookState = &s
	}
	if v.Operation != nil {
		s := *v.Operation
		if s.Message != "" {
			s.Message = "Node operation outcome received"
		}
		v.Operation = &s
	}
	return v
}
func (s *Server) nodesHTTP(w http.ResponseWriter, r *http.Request) {
	var q NodeQuery
	if !decode(w, r, &q) || !s.scope(w, q.Scope) {
		return
	}
	if !validNodeQuery(q) {
		problem(w, 400, "invalid-node-query")
		return
	}
	p, ok := s.port.(NodeManagementPort)
	if !ok {
		problem(w, 409, "node-management-unavailable")
		return
	}
	v := NodeManagementView{Scope: q.Scope}
	var err error
	switch q.Action {
	case "catalog":
		var x api.NodeCatalog
		x, err = p.NodeCatalog(r.Context())
		v.Catalog = &x
	case "configuration":
		var x api.NodeRuntimeConfiguration
		x, err = p.NodeRuntimeConfiguration(r.Context(), q.NodeID, q.Backend)
		v.Configuration = &x
	case "operation":
		var x api.NodeOperationReceipt
		x, err = p.ReconcileNodeOperation(r.Context(), *q.Operation)
		v.Operation = &x
	case "enrollment":
		// Recovery can publish the original verified pairing, so serialize it
		// with owner stop and other admitted settings actions.
		s.commands.Lock()
		defer s.commands.Unlock()
		if s.stopping {
			problem(w, 409, "node-management-unavailable")
			return
		}
		var x api.NodeAddResult
		x, err = p.ReconcileNodeEnrollment(r.Context(), q.NodeID)
		v.Enrollment = &x
	case "workers":
		x := projectWorkers(p.WorkerNodes())
		v.Workers = &x
	case "notebook":
		var x backend.NotebookSyncSettings
		var y notebooksync.State
		x, err = p.NotebookSyncSettings()
		if err == nil {
			y, err = p.NotebookSyncState()
		}
		v.NotebookSettings, v.NotebookState = &x, &y
	}
	if err != nil {
		problem(w, 409, "node-management-unavailable")
		return
	}
	s.write(w, projectNodeView(v))
}
func (s *Server) executeNodes(ctx context.Context, c Command) Result {
	p, ok := s.port.(NodeManagementPort)
	if !ok {
		return Result{ID: c.ID, Outcome: "rejected", Code: "node-management-unavailable"}
	}
	in := c.NodeManagement
	v := NodeManagementView{Scope: c.Scope}
	result := Result{ID: c.ID, Outcome: "accepted"}
	var err error
	switch in.Action {
	case "add-node":
		var x api.NodeAddResult
		x, err = p.AddNode(ctx, *in.Add)
		v.Enrollment = &x
		switch x.Outcome {
		case "committed":
		case "failed":
			result.Outcome = "rejected"
		default:
			result.Outcome = "unknown"
		}
	case "detect-node":
		var x api.NodeInfo
		x, err = p.DetectNode(ctx, in.NodeID)
		v.Node = &x
	case "configure-node":
		var x api.NodeOperationReceipt
		x, err = p.ChangeNodeConfiguration(ctx, *in.Configuration)
		v.Operation = &x
		switch x.Outcome {
		case api.NodeCommitted:
		case api.NodeRejected, api.NodeConflicted:
			result.Outcome = "rejected"
		default:
			result.Outcome = "unknown"
		}
	case "save-worker-node", "probe-worker-target", "connect-worker-target", "disconnect-worker-target":
		var x backend.WorkerNodeSetup
		t := in.Worker
		target := api.WorkTarget{NodeID: t.NodeID, Backend: string(t.Backend), Role: api.RoleWorker}
		if in.Action == "save-worker-node" {
			// Label and route are resolved from this owner's existing enrolled catalog.
			catalog, e := p.NodeCatalog(ctx)
			err = e
			if err == nil {
				label := ""
				for _, n := range catalog.Nodes {
					if n.ID == t.NodeID {
						label = n.Label
					}
				}
				if label == "" {
					err = ErrUnsupported
				} else {
					x, err = p.SaveWorkerNode(backend.WorkerNodeConfig{Transport: "registered-agent", ID: t.NodeID, Label: label, Backend: string(t.Backend)}, t.Revision)
				}
			}
		} else {
			switch in.Action {
			case "probe-worker-target":
				x, err = p.ProbeWorkerTarget(ctx, target, t.Revision)
			case "connect-worker-target":
				x, err = p.ConnectWorkerTarget(ctx, target, t.Revision)
			case "disconnect-worker-target":
				x, err = p.DisconnectWorkerTarget(ctx, target, t.Revision)
			}
		}
		y := projectWorkers(x)
		v.Workers = &y
	case "save-notebook-settings":
		var x backend.NotebookSyncSettings
		x, err = p.SaveNotebookSyncSettings(ctx, *in.NotebookSettings)
		v.NotebookSettings = &x
	case "sync-notebook", "switch-notebook-node":
		var x notebooksync.State
		if in.Action == "sync-notebook" {
			x, err = p.SyncNotebook(ctx, in.NodeID)
		} else {
			x, err = p.SwitchNotebookNode(ctx, in.NodeID)
		}
		v.NotebookState = &x
	}
	if err != nil {
		result.Outcome, result.Code = "unknown", "native-outcome-unconfirmed"
		if errors.Is(err, ErrUnsupported) {
			result.Outcome, result.Code = "rejected", "unavailable"
		}
	}
	projected := projectNodeView(v)
	result.NodeManagement = &projected
	return result
}
func (c *Client) NodeManagementState(ctx context.Context, q NodeQuery) (NodeManagementView, error) {
	scope, err := c.scope()
	if err != nil {
		return NodeManagementView{}, err
	}
	q.Scope = scope
	if !validNodeQuery(q) {
		return NodeManagementView{}, errors.New("invalid node query")
	}
	var v NodeManagementView
	err = c.request(ctx, "POST", "/v1/management/nodes", q, &v)
	if err == nil && v.Scope != scope {
		err = errors.New("node management identity mismatch")
	}
	return v, err
}

// ManageNodes uses the same journalled command and SSH product proxy. It never
// retries, and loss of a response is reconciled through Receipt(original ID).
func (c *Client) ManageNodes(ctx context.Context, id string, in NodeCommand) (Result, error) {
	if !validNodeCommand(in, id) {
		return Result{}, errors.New("invalid node command")
	}
	r, err := c.Command(ctx, Command{ID: id, Kind: "manage-nodes", NodeManagement: &in})
	scope, e := c.scope()
	if err == nil && e != nil {
		err = e
	}
	if err == nil && !receiptOutcome(r.Outcome) {
		err = errors.New("invalid node management receipt outcome")
	}
	if err == nil && r.NodeManagement != nil && r.NodeManagement.Scope != scope {
		err = errors.New("node management receipt identity mismatch")
	}
	return r, err
}

var _ NodeManagementPort = ServicePort{}
