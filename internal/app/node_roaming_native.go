package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/memorytransfer"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodebroker"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// These are trusted native assembly hooks, never renderer-selected binaries or
// shell commands. Approval covers the closed deployment plan, not new SSH keys,
// system services, automatic login or Runtime credential transfer.
type NodeRoamingNativeOptions struct {
	Host                func() (string, error)
	Artifact            func(string) (nodeagent.Artifact, error)
	LocalSSHDestination string
	LocalCodexBinary    string
	RuntimeSettings     func(context.Context, NodeRegistration, api.NodeBackend) (api.RuntimeSettings, error)
}

type NodeRoamingSupervisorPlan struct {
	Version      int                           `json:"version"`
	PlanID       string                        `json:"planId"`
	OperationID  string                        `json:"operationId"`
	NodeID       string                        `json:"nodeId"`
	Helper       string                        `json:"helper"`
	HelperSHA256 string                        `json:"helperSha256"`
	Directory    string                        `json:"directory"`
	Broker       *NodeRoamingBrokerDeployment  `json:"broker"`
	Managed      *NodeRoamingManagedDeployment `json:"managed"`
}
type NodeRoamingBrokerDeployment struct {
	BotID, NodeID, Profile, Socket, PeersFile, BootstrapPeersFile, PreferredNodeID string
}
type NodeRoamingManagedDeployment struct {
	BotID, NodeID, Backend, AgentDirectory, GenerationRoot, AuthFile, CodexBinary, RuntimeDirectory string
	CaelisBinary, CaelisStore, Model                                                                string
	BrokerNodeID, BrokerSocket, BrokerSSHDestination, BrokerHelper, WorkersFile                     string
	JoinSSHDestination, JoinHelper, JoinDirectory                                                   string
}
type roamingNativeNode struct {
	Registration     NodeRegistration
	Plan             NodeRoamingSupervisorPlan
	HostHelper       string
	AgentSocket      string
	BrokerPeerSocket string
}
type roamingNativePlan struct {
	ID                                                             string
	OperationID, BotID, SourceNodeID, SourceBackend                string
	Coordinator                                                    NodeRegistration
	Nodes                                                          []roamingNativeNode
	Enrollment                                                     []NodeRegistration
	BootstrapSocket, BootstrapDirectory, LocalDirectory, LocalHost string
	Phase, RestoreDirectory                                        string
	RestoredSnapshot                                               nodeplane.SnapshotRef
	LocalRuntime                                                   api.RuntimeSettings
	LocalExecution                                                 api.ExecutionSettings
	LocalWorkExecution                                             api.WorkExecutionSettings
	DisableOperationID                                             string
	DisableLease                                                   nodeplane.Lease
}
type roamingNativeAssembly struct {
	app      *Application
	options  NodeRoamingNativeOptions
	mu       sync.Mutex
	plans    map[string]roamingNativePlan
	sessions map[*nodebroker.Client]*roamingNativeSession
}
type roamingNativeSession struct {
	assembly    *roamingNativeAssembly
	plan        roamingNativePlan
	broker      *nodebroker.Client
	peers       map[string]*nodeagent.Client
	disabled    bool
	closed      bool
	mu          sync.Mutex
	observerCtx context.Context
	detach      context.CancelFunc
}

// DefaultNodeRoamingOptions adds no process or broker dependency at construction.
// Only explicit, exactly reviewed persistent enable can execute deployment.
func attachDefaultNodeRoaming(a *Application) error {
	return AttachNodeRoaming(a, DefaultNodeRoamingOptions(a))
}

func DefaultNodeRoamingOptions(a *Application, options ...NodeRoamingNativeOptions) NodeRoamingOptions {
	n := &roamingNativeAssembly{app: a, plans: map[string]roamingNativePlan{}, sessions: map[*nodebroker.Client]*roamingNativeSession{}}
	if len(options) == 1 {
		n.options = options[0]
	}
	if n.options.Host == nil {
		n.options.Host = DefaultNativeNodeHost
	}
	if n.options.Artifact == nil {
		n.options.Artifact = DefaultNodeAgentArtifact
	}
	if n.options.LocalSSHDestination == "" {
		n.options.LocalSSHDestination = os.Getenv("CAELIS_BOT_NODE_OUTGOING_SSH_TARGET")
	}
	o := NodeRoamingOptions{PreparePlan: n.prepare, Preflight: n.preflight, Stage: n.stage, ResolveProduct: n.resolve, RestoreLocal: n.restore, Recover: n.recover}
	o.RefreshNodeManagement = &NodeManagementNativeOptions{LocalAgent: roamingManagedCatalog{assembly: n}, Dial: func(ctx context.Context, r NodeRegistration) (nodeplane.CatalogAgent, error) {
		return n.managementPeer(ctx, r.ID)
	}, ExecutionState: func() (string, *api.WorkTarget) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		n.mu.Lock()
		var s *roamingNativeSession
		for _, candidate := range n.sessions {
			s = candidate
		}
		n.mu.Unlock()
		if s == nil {
			return "", nil
		}
		lease, e := s.broker.CurrentLease(ctx, s.plan.BotID)
		if e != nil {
			return "", nil
		}
		return lease.NodeID, nil
	}, BrokerStatus: func(ctx context.Context, id string) (api.NodeBroker, error) {
		n.mu.Lock()
		var s *roamingNativeSession
		for _, candidate := range n.sessions {
			if candidate.plan.Coordinator.ID == id {
				s = candidate
			}
		}
		n.mu.Unlock()
		if s == nil {
			return api.NodeBroker{}, errors.New("approved independent broker is not connected")
		}
		_, e := s.broker.LatestSnapshot(ctx, s.plan.BotID)
		_, leaseErr := s.broker.CurrentLease(ctx, s.plan.BotID)
		return api.NodeBroker{NodeID: id, Reachable: e == nil, AutomaticRoaming: e == nil && leaseErr == nil}, e
	}}
	return o
}
func nativeRoamingKey(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}
func nativeRoamingDigest(path string) (string, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		return "", errors.New("verified native host bytes unavailable")
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !os.SameFile(info, s) || !s.Mode().IsRegular() || s.Size() > 256<<20 {
		return "", errors.New("verified native host bytes unavailable")
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (n *roamingNativeAssembly) catalogNode(ctx context.Context, id string) (api.NodeInfo, error) {
	catalog, e := n.app.Backend.NodeCatalog(ctx)
	if e != nil {
		return api.NodeInfo{}, e
	}
	for _, info := range catalog.Nodes {
		if info.ID == id {
			return info, nil
		}
	}
	return api.NodeInfo{}, errors.New("exact enrolled Runtime catalog unavailable")
}
func (n *roamingNativeAssembly) build(ctx context.Context, in NodeRoamingStageInput) (roamingNativePlan, error) {
	var p roamingNativePlan
	if n.app == nil || in.OperationID == "" || !productIdentifier.MatchString(in.OperationID) || in.Coordinator.ID == "" || in.SourceTarget.NodeID != api.LocalNodeID || (in.SourceTarget.Backend != "codex" && in.SourceTarget.Backend != "caelis") || in.SourceTarget.Role != api.RoleBot {
		return p, errors.New("exact native source and coordinator enrollment required")
	}
	host, e := n.options.Host()
	if e != nil {
		return p, e
	}
	digest, e := nativeRoamingDigest(host)
	if e != nil {
		return p, e
	}
	p.OperationID = in.OperationID
	activeSource := ActiveNodeRoamingApplication(n.app)
	if activeSource == nil {
		return p, errors.New("actual native local source is unavailable")
	}
	p.LocalRuntime = activeSource.Backend.RuntimeSettings()
	p.LocalExecution, _ = activeSource.Backend.ExecutionSettings()
	p.LocalWorkExecution = activeSource.Backend.WorkExecutionSettings()
	p.Coordinator = in.Coordinator
	p.SourceNodeID = in.SourceTarget.NodeID
	p.SourceBackend = in.SourceTarget.Backend
	p.LocalHost = host
	// Runtime IPC lives in a deterministic private native slot, separate from
	// durable cold bundles. Neither path can be supplied by the renderer.
	tempRoot, e := filepath.EvalSymlinks(os.TempDir())
	if e != nil {
		return p, errors.New("canonical private native socket root unavailable")
	}
	p.LocalDirectory = filepath.Join(tempRoot, "caelis-roaming-"+strconv.Itoa(os.Getuid())+"-"+nativeRoamingKey(n.app.root+"/"+in.OperationID))
	p.BotID = in.BotID
	if p.BotID == "" {
		var state struct {
			ID string `json:"id"`
		}
		b, e := os.ReadFile(filepath.Join(n.app.root, "bot.json"))
		if e != nil || json.Unmarshal(b, &state) != nil || state.ID == "" {
			return p, errors.New("source Bot identity unavailable")
		}
		p.BotID = state.ID
	}
	regs := append([]NodeRegistration(nil), in.Nodes...)
	sort.Slice(regs, func(i, j int) bool { return regs[i].ID < regs[j].ID })
	p.Enrollment = append([]NodeRegistration(nil), regs...)
	seen := map[string]bool{}
	foundCoordinator := false
	for _, r := range regs {
		if r.ID == "" || seen[r.ID] {
			return p, errors.New("duplicate native node enrollment")
		}
		seen[r.ID] = true
		if r.Join == api.NodeOutgoing {
			return p, fmt.Errorf("node %s needs an independently configured managed starter before deployment", r.Label)
		}
		dir := filepath.Join(r.Directory, "roaming-"+nativeRoamingKey(in.OperationID))
		helper := r.HostHelperPath
		sha := ""
		if r.ID == api.LocalNodeID {
			dir = p.LocalDirectory
			helper = host
			sha = digest
			r.Join = api.NodeLocal
			r.Directory = filepath.Join(n.app.root, "nodeplane")
			r.HelperPath, r.HostHelperPath = host, host
		} else {
			if validateNodeRegistration(r) != nil || r.Join != api.NodeSSH || helper == "" {
				return p, fmt.Errorf("node %s needs a verified enrolled host companion", r.Label)
			}
			arch, e := nodeagent.ProbeArchitecture(ctx, nodeagent.SSHConfig{Target: r.SSHDestination})
			if e != nil {
				return p, fmt.Errorf("node %s cannot inspect its enrolled host", r.Label)
			}
			a, e := n.options.Artifact(arch)
			if e != nil || a.HostPath == "" || a.HostExpectedSHA256 == "" {
				return p, errors.New("verified packaged host companion unavailable")
			}
			sha = a.HostExpectedSHA256
		}
		plan := NodeRoamingSupervisorPlan{Version: 1, OperationID: in.OperationID, NodeID: r.ID, Helper: helper, HelperSHA256: sha, Directory: dir}
		m := &NodeRoamingManagedDeployment{BotID: p.BotID, NodeID: r.ID, Backend: in.SourceTarget.Backend, AgentDirectory: filepath.Join(dir, "agent"), GenerationRoot: filepath.Join(dir, "generations"), AuthFile: filepath.Join(dir, "product.token"), WorkersFile: filepath.Join(dir, "workers.json"), BrokerNodeID: in.Coordinator.ID}
		if r.ID == api.LocalNodeID {
			m.CodexBinary = n.options.LocalCodexBinary
			if m.CodexBinary == "" {
				m.CodexBinary = p.LocalRuntime.CLIPath
			}
		} else {
			info, err := n.catalogNode(ctx, r.ID)
			if err != nil {
				return p, err
			}
			ready := map[string]bool{}
			for _, runtime := range info.Runtimes {
				if runtime.Authentication == api.NodeAuthenticated && runtime.Health == api.NodeHealthy {
					ready[string(runtime.Backend)] = true
				}
			}
			if !ready[m.Backend] {
				if ready["codex"] {
					m.Backend = "codex"
				} else if ready["caelis"] {
					m.Backend = "caelis"
				} else {
					return p, fmt.Errorf("node %s needs its own authenticated healthy Runtime", r.Label)
				}
			}
			if m.Backend == "codex" {
				m.RuntimeDirectory = filepath.Join(r.Directory, "runtime")
			}
		}
		if m.Backend == "caelis" {
			settings := p.LocalRuntime
			if r.ID != api.LocalNodeID {
				if n.options.RuntimeSettings == nil {
					return p, errors.New("target Caelis needs an independently designated owned Host configuration")
				}
				settings, e = n.options.RuntimeSettings(ctx, r, api.NodeCaelis)
				if e != nil {
					return p, e
				}
			}
			if settings.Runtime != "caelis" || !filepath.IsAbs(settings.CLIPath) || !filepath.IsAbs(settings.CaelisStore) {
				return p, errors.New("target Caelis requires its own installed binary and designated owned Host")
			}
			m.CaelisBinary, m.CaelisStore = settings.CLIPath, settings.CaelisStore
			m.CodexBinary, m.RuntimeDirectory = "", ""
		}
		plan.Managed = m
		p.Nodes = append(p.Nodes, roamingNativeNode{Registration: r, Plan: plan, HostHelper: helper, AgentSocket: filepath.Join(m.AgentDirectory, "agent.sock")})
		foundCoordinator = foundCoordinator || r.ID == in.Coordinator.ID
		if r.ID == in.Coordinator.ID {
			p.Coordinator = r
		}
	}
	if !seen[api.LocalNodeID] || !foundCoordinator {
		return p, errors.New("source/coordinator is absent from native enrollment")
	}
	var coordinator *roamingNativeNode
	for i := range p.Nodes {
		if p.Nodes[i].Registration.ID == p.Coordinator.ID {
			coordinator = &p.Nodes[i]
		}
	}
	brokerDir := coordinator.Plan.Directory
	broker := &NodeRoamingBrokerDeployment{BotID: p.BotID, NodeID: p.Coordinator.ID, Profile: filepath.Join(brokerDir, "broker"), Socket: filepath.Join(brokerDir, "broker.sock"), PeersFile: filepath.Join(brokerDir, "peers.json"), BootstrapPeersFile: filepath.Join(brokerDir, "bootstrap-peers.json"), PreferredNodeID: api.LocalNodeID}
	coordinator.Plan.Broker = broker
	p.BootstrapDirectory = filepath.Join(brokerDir, "bootstrap")
	p.BootstrapSocket = filepath.Join(p.BootstrapDirectory, "agent.sock")
	for i := range p.Nodes {
		x := &p.Nodes[i]
		m := x.Plan.Managed
		m.BrokerSocket = broker.Socket
		if x.Registration.ID == p.Coordinator.ID {
			x.BrokerPeerSocket = x.AgentSocket
			continue
		}
		m.JoinSSHDestination = p.Coordinator.SSHDestination
		m.JoinHelper = p.Coordinator.HelperPath
		if p.Coordinator.ID == api.LocalNodeID {
			m.JoinSSHDestination = n.options.LocalSSHDestination
			m.JoinHelper = host
		}
		if _, e := strictRoamingSSH(m.JoinSSHDestination); e != nil {
			return p, errors.New("existing outward SSH destination unavailable")
		}
		if m.JoinSSHDestination == "" || m.JoinHelper == "" {
			return p, fmt.Errorf("node %s needs its existing outbound SSH route to the designated coordinator", x.Registration.Label)
		}
		m.BrokerSSHDestination = m.JoinSSHDestination
		m.BrokerHelper = coordinator.HostHelper
		m.JoinDirectory = filepath.Join(brokerDir, "joins", nativeRoamingKey(x.Registration.ID))
		x.BrokerPeerSocket = filepath.Join(m.JoinDirectory, "agent.sock")
	}
	if p.Coordinator.ID == api.LocalNodeID {
		p.BootstrapSocket = filepath.Join(p.LocalDirectory, "source.sock")
	}
	// Digest includes exact enrollment, helper bytes and closed native actions;
	// no source snapshot/native thread/receipt changes the approval after fencing.
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	p.ID = hex.EncodeToString(h[:])
	for i := range p.Nodes {
		p.Nodes[i].Plan.PlanID = p.ID
	}
	return p, nil
}
func (n *roamingNativeAssembly) prepare(ctx context.Context, in NodeRoamingStageInput) (backend.NodeRoamingPlan, error) {
	p, e := n.build(ctx, in)
	if e != nil {
		return backend.NodeRoamingPlan{}, e
	}
	n.mu.Lock()
	if n.plans == nil {
		n.plans = map[string]roamingNativePlan{}
	}
	n.plans[p.ID] = p
	n.mu.Unlock()
	result := backend.NodeRoamingPlan{ID: p.ID, CoordinatorNodeID: p.Coordinator.ID, RequiresConfirmation: true}
	for _, x := range p.Nodes {
		result.Actions = append(result.Actions, backend.NodeRoamingPlanAction{NodeID: x.Registration.ID, Label: x.Registration.Label, Action: backend.NodeRoamingPrepareNode}, backend.NodeRoamingPlanAction{NodeID: x.Registration.ID, Label: x.Registration.Label, Action: backend.NodeRoamingStartBot})
		if x.Plan.Broker != nil {
			result.Actions = append(result.Actions, backend.NodeRoamingPlanAction{NodeID: x.Registration.ID, Label: x.Registration.Label, Action: backend.NodeRoamingPrepareCoordinator})
		}
		if x.Plan.Managed.JoinSSHDestination != "" {
			result.Actions = append(result.Actions, backend.NodeRoamingPlanAction{NodeID: x.Registration.ID, Label: x.Registration.Label, Action: backend.NodeRoamingConnectOutgoing})
		}
	}
	result.Actions = append(result.Actions, backend.NodeRoamingPlanAction{NodeID: api.LocalNodeID, Label: "This machine", Action: backend.NodeRoamingStopSource})
	return result, nil
}
func (n *roamingNativeAssembly) preflight(ctx context.Context, in NodeRoamingStageInput) (result error) {
	defer func() {
		if result != nil {
			result = errors.Join(ErrNodeRoamingPreflight, result)
		}
	}()
	p, e := n.build(ctx, in)
	if e != nil {
		return e
	}
	if !in.AllowPersistentExecution || in.ReviewedPlanID != p.ID {
		return errors.New("review and confirm the exact node deployment plan before retiring the local profile")
	}
	if !in.Resume {
		source := ActiveNodeRoamingApplication(n.app)
		if source == nil {
			return errors.New("actual native local source is unavailable")
		}
		native, ok := source.engine.(*codex.Session)
		if !ok || !native.OwnsLiveRuntime() {
			return errors.New("source Runtime has no confirmed owned shutdown authority; shared Caelis Hosts cannot provide bootstrap proof")
		}
		if e = source.guardRuntimeChange(); e != nil {
			return e
		}
	}
	for _, x := range p.Nodes {
		if len(x.AgentSocket) >= 100 || len(x.BrokerPeerSocket) >= 100 {
			return errors.New("native deployment private socket path exceeds platform limit")
		}
		if x.Registration.ID == api.LocalNodeID {
			if m := x.Plan.Managed; m.JoinSSHDestination != "" {
				if e = runRoamingSSH(ctx, m.JoinSSHDestination, nodeShellQuote(m.JoinHelper)+" verify-join-directory --directory "+nodeShellQuote(p.Coordinator.Directory), nil); e != nil {
					return errors.New("this machine's existing outward coordinator authorization unavailable")
				}
			}
			continue
		}
		// Inspect the fixed enrolled helper and the target's own existing outbound
		// authorization before any source fence or deployment write.
		script := "test -x " + nodeShellQuote(x.HostHelper) + " && test \"$(sha256sum " + nodeShellQuote(x.HostHelper) + " | cut -d ' ' -f 1)\" = " + nodeShellQuote(x.Plan.HelperSHA256)
		if e = runRoamingSSH(ctx, x.Registration.SSHDestination, script, nil); e != nil {
			return fmt.Errorf("node %s host companion verification failed", x.Registration.Label)
		}
		info, e := n.catalogNode(ctx, x.Registration.ID)
		healthy := false
		if e == nil {
			for _, r := range info.Runtimes {
				healthy = healthy || string(r.Backend) == x.Plan.Managed.Backend && r.Health == api.NodeHealthy && r.Authentication == api.NodeAuthenticated
			}
		}
		if !healthy {
			return fmt.Errorf("node %s needs its own authenticated healthy selected Runtime", x.Registration.Label)
		}
		m := x.Plan.Managed
		if m.JoinSSHDestination != "" {
			args, e := strictRoamingSSH(m.JoinSSHDestination)
			if e != nil {
				return e
			}
			quoted := []string{"ssh"}
			for _, arg := range args {
				quoted = append(quoted, nodeShellQuote(arg))
			}
			quoted = append(quoted, nodeShellQuote(nodeShellQuote(m.JoinHelper)+" verify-join-directory --directory "+nodeShellQuote(p.Coordinator.Directory)))
			if e = runRoamingSSH(ctx, x.Registration.SSHDestination, strings.Join(quoted, " "), nil); e != nil {
				return fmt.Errorf("node %s existing outbound coordinator authorization unavailable", x.Registration.Label)
			}
		}
	}
	return nil
}
func strictRoamingSSH(target string) ([]string, error) {
	// Reuse the product SSH target validator; an explicit loopback pairing carries
	// no credential bytes. Commands below are constructed from sealed native plans.
	if !workerSSH.MatchString(target) || strings.HasPrefix(target, "-") {
		return nil, errors.New("invalid existing SSH destination")
	}
	return []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UpdateHostKeys=no", "-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "ForwardX11Trusted=no", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ForkAfterAuthentication=no", "-o", "ConnectTimeout=10", "--", target}, nil
}
func runRoamingSSH(ctx context.Context, target, command string, input []byte) error {
	args, e := strictRoamingSSH(target)
	if e != nil {
		return e
	}
	args = append(args, command)
	c := exec.CommandContext(ctx, "ssh", args...)
	c.Stdin = bytes.NewReader(input)
	c.Stdout, c.Stderr = io.Discard, io.Discard
	if c.Run() != nil {
		return errors.New("fixed native SSH action unconfirmed")
	}
	return nil
}

func nativeRoamingJSON(v any) ([]byte, error) { return json.Marshal(v) }
func nativeRoamingPrivateDir(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("absolute private native deployment directory required")
	}
	if e := os.MkdirAll(path, 0700); e != nil {
		return e
	}
	return nodeagent.CheckPrivateDirectory(path)
}
func (n *roamingNativeAssembly) provision(ctx context.Context, p roamingNativePlan) error {
	type peer struct {
		NodeID  string          `json:"nodeId"`
		Backend api.NodeBackend `json:"backend"`
		Socket  string          `json:"socket"`
	}
	type peers struct {
		Version int    `json:"version"`
		BotID   string `json:"botId"`
		Peers   []peer `json:"peers"`
	}
	roster := peers{Version: 1, BotID: p.BotID}
	for _, x := range p.Nodes {
		roster.Peers = append(roster.Peers, peer{x.Registration.ID, api.NodeBackend(x.Plan.Managed.Backend), x.BrokerPeerSocket})
	}
	bootstrap := peers{Version: 1, BotID: p.BotID, Peers: []peer{{p.SourceNodeID, api.NodeBackend(p.SourceBackend), p.BootstrapSocket}}}
	for _, x := range p.Nodes {
		type worker struct {
			ID        string `json:"id"`
			Label     string `json:"label"`
			Backend   string `json:"backend"`
			Transport string `json:"transport"`
		}
		type agentRoute struct {
			NodeID  string `json:"nodeId"`
			Backend string `json:"backend"`
			Socket  string `json:"socket"`
		}
		workers := struct {
			Version int          `json:"version"`
			Nodes   []worker     `json:"nodes"`
			Agents  []agentRoute `json:"agents"`
		}{Version: 1, Nodes: []worker{}, Agents: []agentRoute{}}
		for _, other := range p.Nodes {
			if other.Registration.ID != x.Registration.ID {
				workers.Nodes = append(workers.Nodes, worker{other.Registration.ID, other.Registration.Label, other.Plan.Managed.Backend, "registered-agent"})
				workers.Agents = append(workers.Agents, agentRoute{other.Registration.ID, other.Plan.Managed.Backend, other.BrokerPeerSocket})
			}
		}
		files := map[string]any{"workers.json": workers, "supervisor.json": x.Plan, "agent/node.json": struct {
			ID string `json:"id"`
		}{x.Registration.ID}}
		// Preferences are fetched from this exact target; Runtime auth and previous
		// receipts are never exported or added to the Notebook.
		pref := nodeagent.ExecutionPreferences{Schema: 1, Revision: 1}
		if x.Registration.ID == api.LocalNodeID {
			execution := p.LocalExecution
			pref.Conversation = api.WorkExecutionSettings{Model: execution.Model, Effort: execution.Effort, ServiceTier: execution.ServiceTier}
			pref.Worker = p.LocalWorkExecution
		} else {
			configuration, e := n.app.Backend.NodeRuntimeConfiguration(ctx, x.Registration.ID, api.NodeBackend(x.Plan.Managed.Backend))
			if e != nil || !configuration.ConfigurationAvailable {
				return errors.New("target Codex preferences could not be read")
			}
			if configuration.Conversation != nil {
				pref.Conversation = *configuration.Conversation
			}
			if configuration.Worker != nil {
				pref.Worker = *configuration.Worker
			}
		}
		files["agent/execution.json"] = pref
		if x.Plan.Broker != nil {
			files["peers.json"] = roster
			files["bootstrap-peers.json"] = bootstrap
		}
		if x.Registration.ID == api.LocalNodeID {
			for _, dir := range []string{x.Plan.Directory, x.Plan.Managed.AgentDirectory, x.Plan.Managed.GenerationRoot} {
				if e := nativeRoamingPrivateDir(dir); e != nil {
					return e
				}
			}
			if x.Plan.Broker != nil {
				if e := nativeRoamingPrivateDir(p.BootstrapDirectory); e != nil {
					return e
				}
				for _, node := range p.Nodes {
					if node.Plan.Managed.JoinDirectory != "" {
						if e := nativeRoamingPrivateDir(node.Plan.Managed.JoinDirectory); e != nil {
							return e
						}
					}
				}
			}
			for name, value := range files {
				if e := localstate.Write(filepath.Join(x.Plan.Directory, name), value); e != nil {
					return e
				}
			}
			if _, e := os.Lstat(x.Plan.Managed.AuthFile); errors.Is(e, os.ErrNotExist) {
				if e = os.WriteFile(x.Plan.Managed.AuthFile, []byte(rand.Text()+rand.Text()), 0600); e != nil {
					return e
				}
			} else if e != nil {
				return e
			}
		} else {
			// Reviewed files are transferred only to the fixed enrolled user directory.
			// The product token is generated on its target and never returned to APP.
			dirs := []string{x.Plan.Directory, x.Plan.Managed.AgentDirectory, x.Plan.Managed.GenerationRoot}
			if x.Plan.Broker != nil {
				dirs = append(dirs, p.BootstrapDirectory)
				for _, node := range p.Nodes {
					if node.Plan.Managed.JoinDirectory != "" {
						dirs = append(dirs, node.Plan.Managed.JoinDirectory)
					}
				}
			}
			for _, dir := range dirs {
				script := "umask 077; mkdir -p " + nodeShellQuote(dir) + " && " + nodeShellQuote(x.Registration.HelperPath) + " verify-join-directory --directory " + nodeShellQuote(dir)
				if e := runRoamingSSH(ctx, x.Registration.SSHDestination, script, nil); e != nil {
					return e
				}
			}
			for name, value := range files {
				b, e := nativeRoamingJSON(value)
				if e != nil {
					return e
				}
				target := filepath.Join(x.Plan.Directory, name)
				script := "umask 077; test ! -L " + nodeShellQuote(target) + " && cat > " + nodeShellQuote(target+".stage") + " && chmod 600 " + nodeShellQuote(target+".stage") + " && mv -f " + nodeShellQuote(target+".stage") + " " + nodeShellQuote(target)
				if e = runRoamingSSH(ctx, x.Registration.SSHDestination, script, b); e != nil {
					return e
				}
			}
			token := x.Plan.Managed.AuthFile
			script := "umask 077; if test ! -e " + nodeShellQuote(token) + "; then head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \\n' > " + nodeShellQuote(token) + "; fi; test -f " + nodeShellQuote(token) + " && test ! -L " + nodeShellQuote(token)
			if e := runRoamingSSH(ctx, x.Registration.SSHDestination, script, nil); e != nil {
				return e
			}
		}
	}
	return nil
}
func (n *roamingNativeAssembly) launch(ctx context.Context, x roamingNativeNode) error {
	planfile := filepath.Join(x.Plan.Directory, "supervisor.json")
	if x.Registration.ID != api.LocalNodeID {
		script := "umask 077; nohup " + nodeShellQuote(x.HostHelper) + " supervise-roaming --plan-file " + nodeShellQuote(planfile) + " < /dev/null > " + nodeShellQuote(filepath.Join(x.Plan.Directory, "supervisor.log")) + " 2>&1 &"
		return runRoamingSSH(ctx, x.Registration.SSHDestination, script, nil)
	}
	log, e := os.OpenFile(filepath.Join(x.Plan.Directory, "supervisor.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer log.Close()
	c := exec.Command("/usr/bin/nohup", x.HostHelper, "supervise-roaming", "--plan-file", planfile)
	c.Stdin = nil
	c.Stdout, c.Stderr = log, log
	if e = c.Start(); e != nil {
		return errors.New("independent native supervisor could not start")
	}
	return c.Process.Release()
}
func (n *roamingNativeAssembly) dialBroker(ctx context.Context, p roamingNativePlan) (*nodebroker.Client, error) {
	for _, x := range p.Nodes {
		if x.Plan.Broker != nil {
			if x.Registration.ID == api.LocalNodeID {
				return nodebroker.DialUnixForBroker(ctx, x.Plan.Broker.Socket, x.Registration.ID)
			}
			return nodebroker.NewSSHClient(ctx, nodeagent.SSHConfig{Target: x.Registration.SSHDestination}, x.HostHelper, x.Plan.Broker.Socket, x.Registration.ID)
		}
	}
	return nil, errors.New("designated broker plan unavailable")
}
func dialRoamingNode(ctx context.Context, x roamingNativeNode) (*nodeagent.Client, error) {
	if x.Registration.ID == api.LocalNodeID {
		return nodeagent.Dial(ctx, x.AgentSocket, x.Registration.ID)
	}
	return nodeagent.NewSSHClient(ctx, nodeagent.SSHConfig{Target: x.Registration.SSHDestination}, x.Registration.HelperPath, x.AgentSocket, x.Registration.ID)
}
func boundedRoamingRetry(ctx context.Context, f func(context.Context) error) error {
	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var last error
	for {
		attempt, stop := context.WithTimeout(deadline, 5*time.Second)
		last = f(attempt)
		stop()
		if last == nil {
			return nil
		}
		t := time.NewTimer(250 * time.Millisecond)
		select {
		case <-deadline.Done():
			t.Stop()
			return errors.Join(errors.New("independent native deployment readiness unconfirmed"), last)
		case <-t.C:
		}
	}
}
func nativeRoamingInputMatches(p roamingNativePlan, in NodeRoamingStageInput) bool {
	if !in.AllowPersistentExecution || in.ReviewedPlanID != p.ID || in.OperationID != p.OperationID || in.BotID != p.BotID || in.Coordinator.ID != p.Coordinator.ID || in.SourceTarget != (api.WorkTarget{NodeID: p.SourceNodeID, Backend: p.SourceBackend, Role: api.RoleBot}) {
		return false
	}
	regs := append([]NodeRegistration(nil), in.Nodes...)
	sort.Slice(regs, func(i, j int) bool { return regs[i].ID < regs[j].ID })
	a, _ := json.Marshal(regs)
	b, _ := json.Marshal(p.Enrollment)
	return bytes.Equal(a, b)
}
func (n *roamingNativeAssembly) stage(ctx context.Context, in NodeRoamingStageInput) (NodeRoamingStage, error) {
	if n.app == nil {
		return NodeRoamingStage{}, errors.New("native application unavailable")
	}
	manifest := filepath.Join(n.app.root, "nodeplane", "roaming-deployment.json")
	n.mu.Lock()
	p, known := n.plans[in.ReviewedPlanID]
	n.mu.Unlock()
	var e error
	if in.Resume {
		p, e = readNativeRoamingPlan(manifest)
		known = e == nil && p.Phase == "owners-ready"
	}
	if !known || !nativeRoamingInputMatches(p, in) {
		return NodeRoamingStage{}, errors.New("original frozen approved deployment does not match exact enrollment")
	}
	if !in.Resume {
		if previous, err := readNativeRoamingPlan(manifest); err == nil {
			if previous.OperationID == p.OperationID || previous.Phase != "restored-local" {
				return NodeRoamingStage{}, errors.New("original native deployment must be reconciled without replay")
			}
		} else if _, statErr := os.Lstat(manifest); !errors.Is(statErr, os.ErrNotExist) {
			return NodeRoamingStage{}, errors.New("previous native deployment record is unconfirmed")
		}
		if in.Source == nil || in.Snapshot.BotID != p.BotID || in.Snapshot.Epoch != "0" || len(in.Payload) == 0 {
			return NodeRoamingStage{}, errors.New("actual stopped native bootstrap proof is required")
		}
		p.Phase = "preparing"
		if e = localstate.Write(manifest, p); e != nil {
			return NodeRoamingStage{}, e
		}
		if e = n.provision(ctx, p); e != nil {
			return NodeRoamingStage{}, e
		}
		p.Phase = "provisioned"
		if e = localstate.Write(manifest, p); e != nil {
			return NodeRoamingStage{}, e
		}
		// The source socket/reverse connection exists only for one-time genesis.
		// Independently joined candidate endpoints are the normal broker peers.
		release, e := n.bootstrapSource(ctx, p, in.Source)
		if e != nil {
			return NodeRoamingStage{}, e
		}
		defer release()
		for _, x := range p.Nodes {
			if e = n.launch(ctx, x); e != nil {
				return NodeRoamingStage{}, e
			}
		}
		var broker *nodebroker.Client
		e = boundedRoamingRetry(ctx, func(c context.Context) error {
			var err error
			broker, err = n.dialBroker(context.Background(), p)
			return err
		})
		if e != nil {
			return NodeRoamingStage{}, e
		}
		e = broker.BootstrapSnapshot(ctx, in.SourceTarget, in.Snapshot, in.Payload)
		if e != nil {
			if latest, readErr := broker.LatestSnapshot(ctx, p.BotID); readErr == nil && latest == in.Snapshot {
				e = nil
			}
		}
		broker.Close()
		if e != nil {
			return NodeRoamingStage{}, e
		}
		p.Phase = "bootstrap-confirmed"
		if e = localstate.Write(manifest, p); e != nil {
			return NodeRoamingStage{}, e
		}
	}
	observerCtx, detach := context.WithCancel(context.Background())
	broker, e := n.dialBroker(observerCtx, p)
	if e != nil {
		detach()
		return NodeRoamingStage{}, e
	}
	s := &roamingNativeSession{assembly: n, plan: p, broker: broker, peers: map[string]*nodeagent.Client{}, observerCtx: observerCtx, detach: detach}
	for _, x := range p.Nodes {
		var client *nodeagent.Client
		e = boundedRoamingRetry(ctx, func(c context.Context) error {
			var err error
			client, err = dialRoamingNode(observerCtx, x)
			if err != nil {
				return err
			}
			catalog, err := client.Catalog(c)
			if err != nil {
				_ = client.Close()
				return err
			}
			if len(catalog.Nodes) != 1 || catalog.Nodes[0].ID != x.Registration.ID {
				_ = client.Close()
				return errors.New("managed native node identity mismatch")
			}
			return nil
		})
		if e != nil {
			s.close()
			return NodeRoamingStage{}, e
		}
		s.peers[x.Registration.ID] = client
	}
	n.mu.Lock()
	n.sessions[broker] = s
	n.mu.Unlock()
	if !in.Resume {
		s.plan.Phase = "owners-ready"
		if e = localstate.Write(manifest, s.plan); e != nil {
			s.close()
			return NodeRoamingStage{}, e
		}
	}
	return NodeRoamingStage{Broker: broker, Disable: s.disable, Close: s.close}, nil
}
func (n *roamingNativeAssembly) bootstrapSource(ctx context.Context, p roamingNativePlan, source nodeplane.RuntimeProofPort) (func(), error) {
	local := filepath.Join(p.LocalDirectory, "source-agent")
	if e := nativeRoamingPrivateDir(local); e != nil {
		return nil, e
	}
	agent, e := nodeagent.New(nodeagent.Options{Directory: local, NodeID: p.SourceNodeID, RuntimeOwner: source})
	if e != nil {
		return nil, e
	}
	socket := filepath.Join(p.LocalDirectory, "source.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(socket, 0600); e != nil {
		listener.Close()
		return nil, e
	}
	life, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- nodeagent.Serve(life, listener, agent) }()
	if p.Coordinator.ID != api.LocalNodeID {
		var helper string
		for _, x := range p.Nodes {
			if x.Registration.ID == p.Coordinator.ID {
				helper = x.Registration.HelperPath
			}
		}
		go func() {
			_ = nodeagent.Join(life, nodeagent.SSHConfig{Target: p.Coordinator.SSHDestination}, helper, p.BootstrapDirectory, socket)
		}()
	}
	return func() { cancel(); listener.Close(); <-done; _ = os.Remove(socket) }, nil
}
func (n *roamingNativeAssembly) resolve(ctx context.Context, lease nodeplane.Lease, reg NodeRegistration) (NodeRoamingProductLocation, error) {
	n.mu.Lock()
	var session *roamingNativeSession
	for _, s := range n.sessions {
		if s.plan.BotID == lease.BotID {
			session = s
		}
	}
	n.mu.Unlock()
	if session == nil {
		return NodeRoamingProductLocation{}, errors.New("exact independent native session unavailable")
	}
	var node roamingNativeNode
	for _, x := range session.plan.Nodes {
		if x.Registration.ID == lease.NodeID {
			node = x
		}
	}
	if node.Registration.ID == "" || node.Registration.ID != reg.ID {
		return NodeRoamingProductLocation{}, errors.New("active lease node is not in the approved deployment")
	}
	target := api.WorkTarget{NodeID: lease.NodeID, Backend: string(lease.Backend), Role: api.RoleBot}
	peer := session.peers[lease.NodeID]
	if peer == nil {
		return NodeRoamingProductLocation{}, errors.New("active managed node is not paired")
	}
	endpoint, e := peer.ReadManagedProduct(ctx, target)
	if e != nil {
		return NodeRoamingProductLocation{}, e
	}
	if endpoint.BotID != lease.BotID || endpoint.Lease.NodeID != lease.NodeID || endpoint.Lease.Epoch != lease.Epoch || endpoint.AuthFile != node.Plan.Managed.AuthFile {
		return NodeRoamingProductLocation{}, errors.New("managed product endpoint does not match exact lease and target-local token")
	}
	pairing := backend.ProductPairing{Mode: "remote", NodeID: lease.NodeID, BotID: endpoint.Identity.BotID, Label: node.Registration.Label, SSH: node.Registration.SSHDestination, Helper: node.HostHelper, Endpoint: endpoint.Endpoint, AuthFile: endpoint.AuthFile}
	location := NodeRoamingProductLocation{Pairing: pairing, Generation: endpoint.Identity.Generation}
	if node.Registration.ID == api.LocalNodeID {
		location.Pairing.SSH = "localhost"
		location.ClientFactory = localRoamingProductFactory(node.HostHelper)
	}
	return location, nil
}
func localRoamingProductFactory(helper string) productClientFactory {
	return func(p backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		if e := validateProductPairing(p); e != nil {
			return nil, nil, e
		}
		c := exec.Command(helper, "proxy-product", "--endpoint", p.Endpoint, "--auth-file", p.AuthFile)
		c.Stderr = io.Discard
		in, e := c.StdinPipe()
		if e != nil {
			return nil, nil, e
		}
		out, e := c.StdoutPipe()
		if e != nil {
			in.Close()
			return nil, nil, e
		}
		if e = c.Start(); e != nil {
			in.Close()
			out.Close()
			return nil, nil, e
		}
		stream := &productSSHStream{input: in, output: out, cmd: c, done: make(chan struct{})}
		go func() { _ = c.Wait(); close(stream.done) }()
		client, e := productrpc.NewStdioClient(productrpc.StdioOptions{ExpectedNode: p.NodeID, ExpectedBot: p.BotID}, stream)
		if e != nil {
			stream.Close()
			return nil, nil, e
		}
		return client, stream, nil
	}
}
func (s *roamingNativeSession) marker(ctx context.Context, state string) error {
	for _, x := range s.plan.Nodes {
		if x.Registration.ID == api.LocalNodeID {
			if e := SetNodeRoamingSupervisorState(filepath.Join(x.Plan.Directory, "supervisor.json"), s.plan.DisableOperationID, state); e != nil {
				return e
			}
		} else {
			command := nodeShellQuote(x.HostHelper) + " control-roaming --plan-file " + nodeShellQuote(filepath.Join(x.Plan.Directory, "supervisor.json")) + " --operation-id " + nodeShellQuote(s.plan.DisableOperationID) + " --state " + nodeShellQuote(state)
			if e := runRoamingSSH(ctx, x.Registration.SSHDestination, command, nil); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *roamingNativeSession) disable(ctx context.Context, operationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !productIdentifier.MatchString(operationID) || s.disabled || s.plan.DisableOperationID != "" {
		return errors.New("native disable already completed; reconcile the original operation")
	}
	lease, e := s.broker.CurrentLease(ctx, s.plan.BotID)
	if e != nil {
		return e
	}
	owner := s.peers[lease.NodeID]
	if owner == nil {
		return errors.New("active owner is not paired")
	}
	proof, e := owner.ReadRuntimeProof(ctx, api.WorkTarget{NodeID: lease.NodeID, Backend: string(lease.Backend), Role: api.RoleBot})
	if e != nil {
		return e
	}
	if !proof.SafeIdle || proof.Pending || proof.Unknown {
		return ErrNodeRoamingBusy
	}
	if proof.LeaseEpoch != lease.Epoch || proof.Snapshot.BotID != lease.BotID {
		return errors.New("native disable owner changed")
	}
	s.plan.DisableOperationID = operationID
	s.plan.DisableLease = lease
	s.plan.Phase = "disabling"
	if e = nodeagent.WriteManagedPrivateJSON(filepath.Join(s.assembly.app.root, "nodeplane", "roaming-deployment.json"), s.plan); e != nil {
		return e
	}
	// No mutation precedes the exact busy check. Prevent independently owned
	// supervisors from restarting a cancelled claimant before touching owners.
	if e = s.marker(ctx, "disabling"); e != nil {
		return e
	}
	s.plan.Phase = "disabling"
	if e = localstate.Write(filepath.Join(s.assembly.app.root, "nodeplane", "roaming-deployment.json"), s.plan); e != nil {
		return e
	}
	for _, x := range s.plan.Nodes {
		if x.Registration.ID == lease.NodeID {
			continue
		}
		peer := s.peers[x.Registration.ID]
		if _, e = peer.PrepareManagedDisable(ctx, nodeagent.ManagedDisableRequest{Target: api.WorkTarget{NodeID: x.Registration.ID, Backend: x.Plan.Managed.Backend, Role: api.RoleBot}, Lease: lease, OperationID: operationID}); e != nil {
			return e
		}
	}
	if _, e = owner.PrepareManagedDisable(ctx, nodeagent.ManagedDisableRequest{Target: api.WorkTarget{NodeID: lease.NodeID, Backend: string(lease.Backend), Role: api.RoleBot}, Lease: lease, OperationID: operationID}); e != nil {
		return e
	}
	// Keep the cold broker alive until latest bytes have been independently read.
	s.disabled = true
	s.plan.Phase = "owners-stopped"
	if e = localstate.Write(filepath.Join(s.assembly.app.root, "nodeplane", "roaming-deployment.json"), s.plan); e != nil {
		return e
	}
	return nil
}
func (n *roamingNativeAssembly) restore(ctx context.Context, stage NodeRoamingStage) (*Application, error) {
	broker, ok := stage.Broker.(*nodebroker.Client)
	if !ok {
		return nil, errors.New("restore requires the exact native stage")
	}
	n.mu.Lock()
	s := n.sessions[broker]
	n.mu.Unlock()
	if s == nil || !s.disabled {
		return nil, errors.New("independent claimants have not confirmed stopped authority")
	}
	latest, e := broker.LatestSnapshot(ctx, s.plan.BotID)
	if e != nil {
		return nil, e
	}
	payload, e := broker.ReadSnapshot(ctx, latest)
	if e != nil {
		return nil, e
	}
	destination := filepath.Join(n.app.root, "nodeplane", "local-restores", "generation-"+rand.Text())
	result, e := memorytransfer.ApplyNotebook(ctx, memorytransfer.NotebookApplyOptions{Payload: payload, Destination: destination, DestinationStopped: true, Expected: latest, Commit: func(c context.Context, ref nodeplane.SnapshotRef, install func() error) error {
		return broker.CommitInstall(c, ref, install)
	}})
	if e != nil || !result.Activated {
		return nil, errors.Join(errors.New("latest stopped Notebook restore failed"), e)
	}
	// Only this machine's original native settings are retained; no remote auth,
	// history, receipts, tasks or machine paths are installed from the bundle.
	settings := s.plan.LocalRuntime
	if settings.Runtime != "codex" && settings.Runtime != "caelis" {
		return nil, errors.New("local restore requires the original supported native Runtime")
	}
	if e = localstate.Write(filepath.Join(destination, "runtime.json"), settings); e != nil {
		return nil, e
	}
	if e = localstate.Write(filepath.Join(destination, "execution.json"), s.plan.LocalExecution); e != nil {
		return nil, e
	}
	if e = localstate.Write(filepath.Join(destination, "work-execution.json"), s.plan.LocalWorkExecution); e != nil {
		return nil, e
	}
	fresh, e := New(destination, n.app.host)
	if e != nil {
		return nil, e
	}
	if e = s.marker(ctx, "disabled"); e != nil {
		fresh.Close()
		return nil, e
	}
	s.plan.Phase = "restored-local"
	s.plan.RestoreDirectory = destination
	s.plan.RestoredSnapshot = latest
	if e = localstate.Write(filepath.Join(n.app.root, "nodeplane", "roaming-deployment.json"), s.plan); e != nil {
		fresh.Close()
		return nil, e
	}
	return fresh, nil
}
func (s *roamingNativeSession) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.assembly.mu.Lock()
	delete(s.assembly.sessions, s.broker)
	s.assembly.mu.Unlock()
	if s.detach != nil {
		s.detach()
	}
	for _, p := range s.peers {
		_ = p.Close()
	}
	s.broker.Close()
	// Detach only: approved independent supervisors and cold broker remain on
	// APP quit. Only explicit Disable above can change their persistent intent.
	return nil
}

// RunNodeRoamingSupervisor is used only by the verified standalone native host.
// It is independently owned after explicit deployment approval. Its JSON schema
// contains closed broker/managed options, never a general command/RPC payload.
func RunNodeRoamingSupervisor(ctx context.Context, filename string) error {
	info, e := os.Lstat(filename)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 64<<10 {
		return errors.New("approved private supervisor plan unavailable")
	}
	if e = nodeagent.CheckPrivateDirectory(filepath.Dir(filename)); e != nil {
		return e
	}
	b, e := os.ReadFile(filename)
	if e != nil {
		return e
	}
	var p NodeRoamingSupervisorPlan
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid closed native supervisor plan")
	}
	if e = validateNativeSupervisor(p, filename); e != nil {
		return e
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	digest, e := nativeRoamingDigest(executable)
	if e != nil || digest != p.HelperSHA256 {
		return errors.New("supervisor does not match reviewed host bytes")
	}
	// A same-plan duplicate launch cannot create a second coordinator or owner.
	// Child broker/native owner locks remain the actual process authority.
	lock := filepath.Join(p.Directory, "supervisor.lock")
	unlock, e := lockNativeRoamingSupervisor(lock)
	if e != nil {
		return errors.New("native deployment supervisor already owned; inspect existing state")
	}
	defer unlock()
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	var children sync.WaitGroup
	start := func(args []string) {
		children.Add(1)
		go func() {
			defer children.Done()
			for {
				if nativeSupervisorState(p) != "running" {
					return
				}
				unlockIntent, err := lockNativeRoamingIntent(filepath.Join(p.Directory, "supervisor-intent.lock"))
				if err != nil {
					return
				}
				if nativeSupervisorState(p) != "running" {
					unlockIntent()
					return
				}
				c := exec.CommandContext(life, p.Helper, args...)
				c.Stdin = nil
				c.Stdout, c.Stderr = io.Discard, os.Stderr
				err = c.Start()
				unlockIntent()
				if err == nil {
					_ = c.Wait()
				}
				if life.Err() != nil || nativeSupervisorState(p) != "running" {
					return
				}
				t := time.NewTimer(5 * time.Second)
				select {
				case <-life.Done():
					t.Stop()
					return
				case <-t.C:
				}
			}
		}()
	}
	if p.Broker != nil {
		start(nativeBrokerArgs(*p.Broker))
	}
	if p.Managed != nil {
		start(nativeManagedArgs(*p.Managed))
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			children.Wait()
			return ctx.Err()
		case <-ticker.C:
			state := nativeSupervisorState(p)
			if state == "disabled" || state == "invalid" {
				cancel()
				children.Wait()
				return nil
			}
			// disabling leaves the exact existing children alive for final publication
			// and quiescence, but the launch loops never restart failed claimants.
		}
	}
}
func nativeSupervisorState(p NodeRoamingSupervisorPlan) string {
	path := filepath.Join(p.Directory, "deployment-state.json")
	info, e := os.Lstat(path)
	if errors.Is(e, os.ErrNotExist) {
		return "running"
	}
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 {
		return "invalid"
	}
	b, e := os.ReadFile(path)
	var v struct{ PlanID, State string }
	if e != nil || json.Unmarshal(b, &v) != nil || v.PlanID != p.PlanID || (v.State != "disabling" && v.State != "disabled") {
		return "invalid"
	}
	return v.State
}
func validateNativeSupervisor(p NodeRoamingSupervisorPlan, filename string) error {
	digest, e := hex.DecodeString(p.PlanID)
	helperDigest, helperErr := hex.DecodeString(p.HelperSHA256)
	if helperErr != nil || len(helperDigest) != 32 || e != nil || len(digest) != 32 || p.Version != 1 || !productIdentifier.MatchString(p.OperationID) || p.NodeID == "" || p.Broker == nil && p.Managed == nil || p.Directory != filepath.Dir(filename) || filename != filepath.Join(p.Directory, "supervisor.json") || !filepath.IsAbs(p.Helper) || len(p.HelperSHA256) != 64 {
		return errors.New("exact approved supervisor identity required")
	}
	inside := func(path string) bool {
		r, e := filepath.Rel(p.Directory, path)
		return e == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && filepath.IsAbs(path) && filepath.Clean(path) == path
	}
	if p.Broker != nil {
		v := p.Broker
		if v.NodeID != p.NodeID || v.BotID == "" || !inside(v.Profile) || !inside(v.Socket) || !inside(v.PeersFile) || !inside(v.BootstrapPeersFile) {
			return errors.New("broker paths do not match exact approved native slot")
		}
	}
	if p.Managed != nil {
		v := p.Managed
		if v.NodeID != p.NodeID || v.BotID == "" || (v.Backend != "codex" && v.Backend != "caelis") || v.Backend == "caelis" && (!filepath.IsAbs(v.CaelisBinary) || !filepath.IsAbs(v.CaelisStore)) || v.BrokerNodeID == "" || !inside(v.AgentDirectory) || !inside(v.GenerationRoot) || !inside(v.AuthFile) || v.WorkersFile != "" && !inside(v.WorkersFile) || !filepath.IsAbs(v.BrokerSocket) || v.BrokerSSHDestination != "" && (!filepath.IsAbs(v.BrokerHelper) || v.JoinSSHDestination == "" || !filepath.IsAbs(v.JoinDirectory)) {
			return errors.New("managed paths do not match exact approved native slot")
		}
		if p.Broker != nil && (v.BrokerNodeID != p.NodeID || v.BrokerSocket != p.Broker.Socket) {
			return errors.New("local broker/managed pairing mismatch")
		}
	}
	return nil
}
func nativeBrokerArgs(v NodeRoamingBrokerDeployment) []string {
	return []string{"serve-broker", "--profile", v.Profile, "--bot-id", v.BotID, "--node-id", v.NodeID, "--socket", v.Socket, "--peers-file", v.PeersFile, "--bootstrap-peers-file", v.BootstrapPeersFile, "--preferred-node", v.PreferredNodeID}
}
func nativeManagedArgs(v NodeRoamingManagedDeployment) []string {
	a := []string{"serve-roaming", "--node-id", v.NodeID, "--bot-id", v.BotID, "--backend", v.Backend, "--agent-directory", v.AgentDirectory, "--generations", v.GenerationRoot, "--broker-node-id", v.BrokerNodeID, "--broker-socket", v.BrokerSocket, "--auth-file", v.AuthFile, "--listen", "127.0.0.1:0"}
	if v.BrokerSSHDestination != "" {
		a = append(a, "--broker-ssh-target", v.BrokerSSHDestination, "--broker-helper", v.BrokerHelper)
	}
	if v.JoinSSHDestination != "" {
		a = append(a, "--join-target", v.JoinSSHDestination, "--join-helper", v.JoinHelper, "--join-directory", v.JoinDirectory)
	}
	if v.Backend == "caelis" {
		a = append(a, "--caelis-binary", v.CaelisBinary, "--caelis-store", v.CaelisStore)
	}
	if v.Model != "" {
		a = append(a, "--model", v.Model)
	}
	if v.CodexBinary != "" {
		a = append(a, "--codex-binary", v.CodexBinary)
	}
	if v.WorkersFile != "" {
		a = append(a, "--workers-file", v.WorkersFile)
	}
	if v.RuntimeDirectory != "" {
		a = append(a, "--runtime-directory", v.RuntimeDirectory)
	}
	return a
}

func readNativeRoamingPlan(filename string) (roamingNativePlan, error) {
	var p roamingNativePlan
	info, e := os.Lstat(filename)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 128<<10 {
		return p, errors.New("original native deployment record unavailable")
	}
	if e = nodeagent.CheckPrivateDirectory(filepath.Dir(filename)); e != nil {
		return p, e
	}
	f, e := os.Open(filename)
	if e != nil {
		return p, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return p, errors.New("native deployment record changed")
	}
	d := json.NewDecoder(io.LimitReader(f, (128<<10)+1))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.ID == "" || p.OperationID == "" || p.BotID == "" || len(p.Nodes) == 0 {
		return p, errors.New("invalid original native deployment record")
	}
	for _, node := range p.Nodes {
		if node.Plan.PlanID != p.ID || node.Plan.OperationID != p.OperationID || validateNativeSupervisor(node.Plan, filepath.Join(node.Plan.Directory, "supervisor.json")) != nil {
			return p, errors.New("original native deployment pairing changed")
		}
	}
	return p, nil
}
func (n *roamingNativeAssembly) recover(ctx context.Context, in NodeRoamingRecoveryInput) (NodeRoamingRecovery, error) {
	result := NodeRoamingRecovery{OperationID: in.OperationID, Outcome: "unknown"}
	p, e := readNativeRoamingPlan(filepath.Join(n.app.root, "nodeplane", "roaming-deployment.json"))
	if e != nil {
		return result, e
	}
	if p.OperationID != in.StageOperationID || p.ID != in.StageInput.ReviewedPlanID || p.BotID != in.StageInput.BotID || p.Coordinator.ID != in.StageInput.Coordinator.ID {
		return result, errors.New("original native recovery scope changed")
	}
	if in.OperationKind == "enable" && in.OperationID != p.OperationID || in.OperationKind == "disable" && in.OperationID != p.DisableOperationID {
		return result, errors.New("recovery requires the original native operation ID")
	}
	if p.Phase == "owners-ready" && in.OperationKind == "enable" {
		input := in.StageInput
		input.OperationID = p.OperationID
		input.Resume = true
		stage, e := n.stage(ctx, input)
		if e != nil {
			return result, e
		}
		broker := stage.Broker.(*nodebroker.Client)
		ref, e := broker.LatestSnapshot(ctx, p.BotID)
		if e != nil {
			stage.Close()
			return result, e
		}
		payload, e := broker.ReadSnapshot(ctx, ref)
		if e != nil {
			stage.Close()
			return result, e
		}
		verified, e := memorytransfer.ValidateNotebookPayload(ctx, payload)
		if e != nil || verified != ref {
			stage.Close()
			return result, errors.New("native recovery lacks exact complete cold Notebook")
		}
		return NodeRoamingRecovery{OperationID: in.OperationID, Outcome: "accepted", Phase: "active", Stage: stage}, nil
	}
	if p.Phase == "restored-local" && in.OperationKind == "disable" {
		base := filepath.Join(n.app.root, "nodeplane", "local-restores")
		relative, e := filepath.Rel(base, p.RestoreDirectory)
		if e != nil || relative == "." || relative == ".." || strings.Contains(relative, string(filepath.Separator)) || !strings.HasPrefix(relative, "generation-") {
			return result, errors.New("native restore generation is outside original scope")
		}
		ref, e := memorytransfer.ReadInstalledNotebookRef(ctx, p.RestoreDirectory)
		if e != nil || ref != p.RestoredSnapshot || ref.BotID != p.BotID {
			return result, errors.New("native local restore receipt is unconfirmed")
		}
		for _, node := range p.Nodes {
			if node.Registration.ID == api.LocalNodeID && nativeSupervisorState(node.Plan) != "disabled" {
				return result, errors.New("native claimant intent is not disabled")
			}
		}
		fresh, e := New(p.RestoreDirectory, n.app.host)
		if e != nil {
			return result, e
		}
		return NodeRoamingRecovery{OperationID: in.OperationID, Outcome: "accepted", Phase: "local", Local: fresh}, nil
	}
	// Preparing/provisioned/bootstrap/disabling cannot be converted to success
	// by resending a side effect. Original source admission remains withdrawn.
	return result, errors.New("original native deployment outcome still requires exact confirmation")
}

// Management reads/writes remain paired to the actual native managed agent;
// the stable APP Backend may now represent a different remote Bot owner.
func (n *roamingNativeAssembly) managementPeer(ctx context.Context, id string) (*nodeagent.Client, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, s := range n.sessions {
		if peer := s.peers[id]; peer != nil {
			return peer, nil
		}
	}
	return nil, errors.New("managed node agent is not paired")
}

type roamingManagedCatalog struct{ assembly *roamingNativeAssembly }

func (p roamingManagedCatalog) Catalog(ctx context.Context) (api.NodeCatalog, error) {
	peer, e := p.assembly.managementPeer(ctx, api.LocalNodeID)
	if e != nil {
		return api.NodeCatalog{}, e
	}
	c, e := peer.Catalog(ctx)
	if e != nil {
		return c, e
	}
	c.Nodes = append([]api.NodeInfo(nil), c.Nodes...)
	for i := range c.Nodes {
		if c.Nodes[i].ID == api.LocalNodeID {
			c.Nodes[i].Join = api.NodeLocal
			c.Nodes[i].Label = "This machine"
		}
	}
	return c, nil
}
func (p roamingManagedCatalog) Configuration(ctx context.Context, id string, b api.NodeBackend) (api.NodeRuntimeConfiguration, error) {
	peer, e := p.assembly.managementPeer(ctx, id)
	if e != nil {
		return api.NodeRuntimeConfiguration{}, e
	}
	return peer.Configuration(ctx, id, b)
}
func (p roamingManagedCatalog) Manage(ctx context.Context, r nodeplane.ManagementRequest) (api.NodeOperationReceipt, error) {
	peer, e := p.assembly.managementPeer(ctx, r.Ref.NodeID)
	if e != nil {
		return api.NodeOperationReceipt{}, e
	}
	return peer.Manage(ctx, r)
}
func (p roamingManagedCatalog) Reconcile(ctx context.Context, r api.NodeOperationRef) (api.NodeOperationReceipt, error) {
	peer, e := p.assembly.managementPeer(ctx, r.NodeID)
	if e != nil {
		return api.NodeOperationReceipt{}, e
	}
	return peer.Reconcile(ctx, r)
}

// SetNodeRoamingSupervisorState records a closed explicit disable intent with
// atomic publication plus file/directory fsync before any claimant can restart.
// It changes only this reviewed deployment, never a system service or account.
func SetNodeRoamingSupervisorState(filename, operationID, state string) error {
	if !productIdentifier.MatchString(operationID) || (state != "disabling" && state != "disabled") {
		return errors.New("original reviewed deployment disable operation required")
	}
	info, e := os.Lstat(filename)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 64<<10 {
		return errors.New("approved native supervisor record unavailable")
	}
	if e = nodeagent.CheckPrivateDirectory(filepath.Dir(filename)); e != nil {
		return e
	}
	b, e := os.ReadFile(filename)
	if e != nil {
		return e
	}
	var p NodeRoamingSupervisorPlan
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || validateNativeSupervisor(p, filename) != nil {
		return errors.New("original approved supervisor scope changed")
	}
	unlockIntent, e := lockNativeRoamingIntent(filepath.Join(p.Directory, "supervisor-intent.lock"))
	if e != nil {
		return errors.New("native supervisor intent is being confirmed")
	}
	defer unlockIntent()
	current := filepath.Join(p.Directory, "deployment-state.json")
	if bytes, e := os.ReadFile(current); e == nil {
		var v struct{ PlanID, OperationID, State string }
		if json.Unmarshal(bytes, &v) != nil || v.PlanID != p.PlanID || v.OperationID != operationID || v.State == "disabled" && state != "disabled" {
			return errors.New("supervisor disable intent changed; reconcile original operation")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return nodeagent.WriteManagedPrivateJSON(current, struct{ PlanID, OperationID, State string }{p.PlanID, operationID, state})
}
