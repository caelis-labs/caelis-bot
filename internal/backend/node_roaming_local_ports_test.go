package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

type localSetupPort struct {
	api.SetupController
	root    string
	busy    bool
	service *Service
}

func (p *localSetupPort) Overview() api.SetupOverview { return api.SetupOverview{Active: p.root} }
func (p *localSetupPort) Profile(string) (api.RuntimeSettings, error) {
	if p.service != nil {
		return p.service.RuntimeSettings(), nil
	}
	return LoadRuntimeSettings(filepath.Join(p.root, "runtime.json"), "codex")
}
func (p *localSetupPort) Activate(_ context.Context, settings api.RuntimeSettings) error {
	if p.busy {
		return errors.New("fresh owner busy")
	}
	return localstate.Write(filepath.Join(p.root, "runtime.json"), settings)
}
func (p *localSetupPort) Dismiss() error {
	return localstate.Write(filepath.Join(p.root, "setup.json"), struct{ Dismissed bool }{true})
}

type localWorkerPort struct {
	WorkerNodeController
	label string
}

func (p *localWorkerPort) Snapshot() WorkerNodeSetup {
	return WorkerNodeSetup{Nodes: []WorkerNodeView{{Config: WorkerNodeConfig{ID: p.label}}}}
}

type facadeConnectionPort struct{ ProductConnectionController }

func (*facadeConnectionPort) ConnectionState() ProductConnectionState {
	return ProductConnectionState{ActiveMode: "facade", Pairing: ProductPairing{Mode: "local"}}
}

type revokedGenerationAdmission struct{ reason string }

func (p revokedGenerationAdmission) Begin(ctx context.Context) (context.Context, func(), error) {
	return ctx, func() {}, errors.New(p.reason)
}
func (p revokedGenerationAdmission) CheckContext(context.Context) error { return errors.New(p.reason) }

func TestNodeRoamingLocalAdoptsFreshPortsAndKeepsHostFacade(t *testing.T) {
	s, _, oldWorker, _ := interactionFixture(t)
	oldRoot, freshRoot := t.TempDir(), t.TempDir()
	s.ConfigureSetup(&localSetupPort{root: oldRoot})
	s.ConfigureWorkerNodes(&localWorkerPort{label: "retired"})
	connection := &facadeConnectionPort{}
	s.ConfigureProductConnection(connection)
	if err := s.ConfigureDraft(filepath.Join(oldRoot, "draft.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "host draft"}); err != nil {
		t.Fatal(err)
	}
	oldApproval := s.Snapshot().Approvals[0].ID
	proxy, err := s.PrepareNodeRoamingEngine()
	if err != nil {
		t.Fatal(err)
	}
	other, routes, freshWorker, _ := interactionFixture(t)
	freshSetup := &localSetupPort{root: freshRoot, busy: true}
	other.ConfigureSetup(freshSetup)
	other.ConfigureWorkerNodes(&localWorkerPort{label: "fresh"})
	other.ConfigureWorkRoutes(&receiptCatalogFixture{workerRouteFixture: *routes, tasks: []api.Task{{ID: "fresh-task", Title: "Fresh", Status: "working"}}, previews: []api.TaskPreview{{ID: "fresh-task"}}}, filepath.Join(freshRoot, "Artifacts"))
	other.ConfigureRuntime(filepath.Join(freshRoot, "runtime.json"), api.RuntimeSettings{Runtime: "codex"})
	other.ConfigureProductConnection(&facadeConnectionPort{})
	if err := s.ActivateNodeRoamingProduct(&roamingBoundaryEngine{}); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateNodeRoamingLocal(other); err != nil {
		t.Fatal(err)
	}
	if s.engine != proxy || NativeProductConnectionController(s) != connection || s.Draft().Text != "host draft" {
		t.Fatal("fresh generation replaced host-owned facade or draft")
	}
	if s.SetupOverview().Active != freshRoot || s.WorkerNodes().Nodes[0].Config.ID != "fresh" {
		t.Fatal("native setup or worker actions retained the retired owner")
	}
	if rows := s.TaskSummaries(); len(rows) != 1 || rows[0].ID != "fresh-task" {
		t.Fatal("task summaries retained the retired route", rows)
	}
	if err := s.Decide(t.Context(), api.Decision{ID: oldApproval, Choice: "once"}); err == nil || oldWorker.decisions != 0 {
		t.Fatal("retired worker handle retained authority", err)
	}
	freshApproval := s.Snapshot().Approvals[0].ID
	if err := s.Decide(t.Context(), api.Decision{ID: freshApproval, Choice: "once"}); err != nil || freshWorker.decisions != 1 {
		t.Fatal("fresh worker interaction route was not adopted", err)
	}
	settings := api.RuntimeSettings{Runtime: "codex", CLIPath: "/fixture/selected"}
	if err := s.ActivateRuntime(t.Context(), settings); err == nil || err.Error() != "fresh owner busy" {
		t.Fatal("renderer setup bypassed the fresh owner guard", err)
	}
	freshSetup.busy = false
	if err := s.ActivateRuntime(t.Context(), settings); err != nil {
		t.Fatal(err)
	}
	if err := s.DismissSetup(); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadRuntimeSettings(filepath.Join(freshRoot, "runtime.json"), "codex"); err != nil || loaded != settings {
		t.Fatal("renderer setup did not persist into the fresh generation", loaded, err)
	}
	for _, name := range []string{"runtime.json", "setup.json"} {
		if _, err := os.Stat(filepath.Join(oldRoot, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("renderer setup mutated retired source", name, err)
		}
	}
}

func TestNodeRoamingLocalAdoptsFreshAdmissionAndSetupRequirement(t *testing.T) {
	s := NewService(&roamingBoundaryEngine{}, nil, nil, nil, nil)
	s.ConfigureExecutionAdmission(revokedGenerationAdmission{reason: "retired generation revoked"})
	if _, err := s.PrepareNodeRoamingEngine(); err != nil {
		t.Fatal(err)
	}
	other := NewService(&roamingBoundaryEngine{}, nil, nil, nil, nil)
	other.ConfigureExecutionAdmission(revokedGenerationAdmission{reason: "fresh generation revoked"})
	if err := s.ActivateNodeRoamingLocal(other); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(t.Context(), api.Submission{ID: "denied"}); err == nil || err.Error() != "fresh generation revoked" {
		t.Fatal("submission used the retired execution fence", err)
	}
	other.ConfigureExecutionAdmission(nil)
	other.RequireSetup(true)
	if err := s.ActivateNodeRoamingLocal(other); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(t.Context()); err == nil || err.Error() != "请先完成运行时设置" {
		t.Fatal("fresh setup requirement was cleared", err)
	}
}

type freshConfigurationEngine struct{ workModelSettingsEngine }

func (*freshConfigurationEngine) ChangeRuntime(_ context.Context, _ api.RuntimeSettings, persist func() error) (api.RuntimeCheck, error) {
	return api.RuntimeCheck{Saved: true}, persist()
}

func TestNodeRoamingRendererSettingsUpdateFreshAuthoritativeCaches(t *testing.T) {
	s := NewService(&roamingBoundaryEngine{}, nil, nil, nil, nil)
	if _, err := s.PrepareNodeRoamingEngine(); err != nil {
		t.Fatal(err)
	}
	other := modelSettingsFixture(t, &freshConfigurationEngine{})
	other.ConfigureRuntime(filepath.Join(t.TempDir(), "runtime.json"), api.RuntimeSettings{Runtime: "codex", CLIPath: "/fixture/initial"})
	other.ConfigureSetup(&localSetupPort{service: other})
	if err := s.ActivateNodeRoamingLocal(other); err != nil {
		t.Fatal(err)
	}
	settings := api.RuntimeSettings{Runtime: "codex", CLIPath: "/fixture/selected"}
	if result, err := s.SaveRuntimeSettings(t.Context(), settings); err != nil || !result.Saved {
		t.Fatal(result, err)
	}
	if other.RuntimeSettings() != settings {
		t.Fatal("renderer runtime save left the fresh APP cache stale")
	}
	if profile, err := s.SetupProfile("codex"); err != nil || profile != settings {
		t.Fatal("fresh setup profile missed the renderer runtime save", profile, err)
	}
	state, _, err := s.ReadModelSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyModelSettings(t.Context(), productmanagement.ExecutionRevision(state), "conversation", productmanagement.Selection{Model: "two", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	if v, err := other.ExecutionSettings(); err != nil || v.Model != "two" || v.Effort != "high" || v.ApprovalMode != "ask" {
		t.Fatal("renderer model save left fresh native planning settings stale", v, err)
	}
	state, _, err = s.ReadModelSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyModelSettings(t.Context(), productmanagement.ExecutionRevision(state), "work", productmanagement.Selection{Model: "two", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	if other.WorkExecutionSettings().Model != "two" {
		t.Fatal("renderer Worker save left fresh native planning settings stale")
	}
	_, conversation, worker, err := other.NodeExecutionScopes(t.Context())
	if err != nil || conversation.Model != "two" || worker.Model != "two" {
		t.Fatal("next native plan missed the authoritative renderer settings", conversation, worker, err)
	}
	if err := s.ActivateNodeRoamingProduct(&roamingBoundaryEngine{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadModelSettings(t.Context()); err == nil {
		t.Fatal("withdrawn fresh generation retained configuration authority")
	}
}

type metadataRoamingControl struct {
	NodeRoamingController
}

func (*metadataRoamingControl) BlockLocalSetup() bool { return true }
func (*metadataRoamingControl) NodeRoamingState(context.Context) (NodeRoamingState, error) {
	panic("configuration admission must not reconcile native roaming receipts")
}

type metadataNodeManagement struct{ api.NodeManagementController }

func TestNodeRoamingMetadataBlocksRetiredLocalConfigurationWithoutRecovery(t *testing.T) {
	root := t.TempDir()
	s := modelSettingsFixture(t, &freshConfigurationEngine{})
	s.ConfigureSetup(&localSetupPort{root: root})
	s.ConfigureWorkerNodes(&localWorkerPort{label: "retired"})
	s.ConfigureRuntime(filepath.Join(root, "runtime.json"), api.RuntimeSettings{Runtime: "codex"})
	s.ConfigureRuntimeManagement(nil, nil, func(context.Context, string, api.RuntimeSettings) (api.RuntimeStatus, error) {
		t.Fatal("retired native Runtime manager was reached")
		return api.RuntimeStatus{}, nil
	}, nil)
	s.nodeManagement = &metadataNodeManagement{}
	if err := s.ConfigureNodeRoaming(&metadataRoamingControl{}); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"activate": func() error { return s.ActivateRuntime(t.Context(), api.RuntimeSettings{Runtime: "codex"}) },
		"dismiss":  s.DismissSetup,
		"runtime": func() error {
			_, err := s.SaveRuntimeSettings(t.Context(), api.RuntimeSettings{Runtime: "codex"})
			return err
		},
		"conversation-model": func() error { return s.SaveExecutionSettings(t.Context(), api.ExecutionSettings{Model: "two"}) },
		"worker-model":       func() error { return s.SaveWorkExecutionSettings(t.Context(), api.WorkExecutionSettings{Model: "two"}) },
		"model-cas": func() error {
			return s.ApplyModelSettings(t.Context(), "stale", "conversation", productmanagement.Selection{Model: "two"})
		},
		"runtime-manager":          func() error { _, err := s.ManageRuntime(t.Context(), "install", api.RuntimeSettings{}); return err },
		"legacy-worker-save":       func() error { _, err := s.SaveWorkerNode(WorkerNodeConfig{}, 0); return err },
		"legacy-worker-connect":    func() error { _, err := s.ConnectWorkerNode(t.Context(), "retired", 0); return err },
		"legacy-worker-disconnect": func() error { _, err := s.DisconnectWorkerNode(t.Context(), "retired", 0); return err },
	} {
		if err := call(); err == nil {
			t.Fatalf("%s admitted a retired local configuration mutation", name)
		}
	}
	if s.SetupOverview() != (api.SetupOverview{}) || s.WorkerNodes().Issue == "" {
		t.Fatal("retired local setup was advertised as available")
	}
	if _, err := os.Stat(filepath.Join(root, "runtime.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("guarded renderer mutation rewrote retired source", err)
	}
}
