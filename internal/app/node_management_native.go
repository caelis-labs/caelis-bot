package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// NodeRegistration is private native pairing, never a renderer DTO. Source
// paths come from verified bootstrap or an explicitly trusted native join plan.
type NodeRegistration struct {
	ID, Label                                         string
	Join                                              api.NodeJoin
	SSHDestination, Directory, HelperPath, SocketPath string
	BrokerNodeID                                      string
	HostHelperPath                                    string
}

type NodeManagementNativeOptions struct {
	Artifact                               func(string) (nodeagent.Artifact, error)
	LocalAgent                             nodeplane.CatalogAgent
	Bootstrap                              func(context.Context, api.NodeAddRequest) (NodeRegistration, error)
	Dial                                   func(context.Context, NodeRegistration) (nodeplane.CatalogAgent, error)
	PrepareOutgoing                        func(context.Context, NodeRegistration) (NodeRegistration, api.NodeJoinInstructions, error)
	OutgoingInstructions                   func(context.Context, NodeRegistration) (api.NodeJoinInstructions, error)
	BrokerStatus                           func(context.Context, string) (api.NodeBroker, error)
	RuntimeOwner                           nodeplane.RuntimeProofPort
	OutgoingSSHDestination, JoinHelperPath string
	ExecutionState                         func() (string, *api.WorkTarget)
	// Thin APP's Backend belongs to its remote Bot, never local discovery.
	LocalCodexBinary    string
	LocalCaelisSettings *api.RuntimeSettings
}

type nodeManagementDocument struct {
	Version     int
	Revision    uint64
	Nodes       []NodeRegistration
	Coordinator string
}

type nativeNodeManagement struct {
	app       *Application
	directory string
	options   NodeManagementNativeOptions
	local     nodeplane.CatalogAgent
	mu        sync.Mutex
	controlMu sync.Mutex
	document  nodeManagementDocument
	clients   map[string]nodeplane.CatalogAgent
	ownerCtx  context.Context
	cancel    context.CancelFunc
	closed    bool
}

// AttachNodeManagement is called once after the ordinary local Backend/setup
// assembly. It adds an in-process local agent; no broker or socket is started.
// Native construction supplies reviewed artifacts and outgoing join plans.
func AttachNodeManagement(a *Application, options ...NodeManagementNativeOptions) error {
	if a == nil || a.Backend == nil || len(options) > 1 {
		return errors.New("invalid node management assembly")
	}
	var o NodeManagementNativeOptions
	if len(options) == 1 {
		o = options[0]
	}
	if o.Artifact == nil {
		o.Artifact = DefaultNodeAgentArtifact
	}
	directory := filepath.Join(a.root, "nodeplane")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if err := nodeagent.CheckPrivateDirectory(directory); err != nil {
		return err
	}
	doc, err := loadNodeManagementDocument(filepath.Join(directory, "config.json"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &nativeNodeManagement{app: a, directory: directory, options: o, document: doc, clients: map[string]nodeplane.CatalogAgent{}, ownerCtx: ctx, cancel: cancel}
	if o.LocalAgent != nil {
		n.local = o.LocalAgent
	} else {
		localDir := filepath.Join(directory, "local")
		if err = os.MkdirAll(localDir, 0700); err != nil {
			cancel()
			return err
		}
		ports := map[api.NodeBackend]nodeagent.NativeConfiguration{}
		ports[api.NodeCodex] = &nodeLocalCodexConfiguration{nodeLocalConfiguration{app: a, backend: api.NodeCodex}}
		ports[api.NodeCaelis] = &nodeLocalConfiguration{app: a, backend: api.NodeCaelis}
		binaries := map[api.NodeBackend]string{}
		var localHealth func(context.Context, api.NodeBackend) (nodeagent.NativeHealth, error)
		if a.product != nil {
			binary := o.LocalCodexBinary
			if binary == "" {
				binary = os.Getenv("CODEX_BIN")
			}
			codex := &nodeagent.CodexConfiguration{Directory: localDir, Binary: binary}
			if binary != "" {
				if !filepath.IsAbs(binary) {
					cancel()
					return errors.New("explicit local Codex executable must be absolute")
				}
				binaries[api.NodeCodex] = binary
			}
			ports = map[api.NodeBackend]nodeagent.NativeConfiguration{api.NodeCodex: codex}
			var caelis *nodeagent.CaelisConfiguration
			if o.LocalCaelisSettings != nil {
				settings := *o.LocalCaelisSettings
				if settings.Runtime != "caelis" || settings.CaelisStore == "" || !filepath.IsAbs(settings.CaelisStore) {
					cancel()
					return errors.New("explicit local Caelis profile is required")
				}
				caelis = &nodeagent.CaelisConfiguration{Settings: settings}
				ports[api.NodeCaelis] = caelis
				if settings.CLIPath != "" {
					binaries[api.NodeCaelis] = settings.CLIPath
				}
			}
			localHealth = func(ctx context.Context, b api.NodeBackend) (nodeagent.NativeHealth, error) {
				if b == api.NodeCodex {
					return codex.Health(ctx)
				}
				if caelis != nil {
					return caelis.Health(ctx)
				}
				return nodeagent.NativeHealth{}, nil
			}
		} else {
			localHealth = func(ctx context.Context, b api.NodeBackend) (nodeagent.NativeHealth, error) {
				return nodeLocalHealth(ctx, a, b)
			}
		}
		n.local, err = nodeagent.New(nodeagent.Options{Directory: localDir, NodeID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal, Binaries: binaries, Configurations: ports, OwnedRuntimeSettings: n.localOwnedRuntimeSettings, OwnedRuntimeCompanion: n.localOwnedRuntimeCompanion, RuntimeOwner: o.RuntimeOwner, Health: func(ctx context.Context, b api.NodeBackend) (nodeagent.NativeHealth, error) {
			return localHealth(ctx, b)
		}})
		if err != nil {
			cancel()
			return err
		}
	}
	a.Backend.SetNodeManagementController(NewNodeManagement(n, n))
	return nil
}

func loadNodeManagementDocument(path string) (nodeManagementDocument, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nodeManagementDocument{Version: 1, Revision: 1, Nodes: []NodeRegistration{}}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 128*1024 {
		return nodeManagementDocument{}, errors.New("node pairing document is unavailable")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nodeManagementDocument{}, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var doc nodeManagementDocument
	if err = d.Decode(&doc); err != nil || doc.Version != 1 || doc.Revision == 0 || len(doc.Nodes) > 16 {
		return doc, errors.New("node pairing document is invalid")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return doc, errors.New("node pairing document has extra data")
	}
	seen := map[string]bool{api.LocalNodeID: true}
	for _, r := range doc.Nodes {
		if err := validateNodeRegistration(r); err != nil || seen[r.ID] {
			return doc, errors.New("node pairing identity is invalid")
		}
		seen[r.ID] = true
	}
	if doc.Coordinator != "" && !seen[doc.Coordinator] {
		return doc, errors.New("designated coordinator is not enrolled")
	}
	return doc, nil
}

func validateNodeRegistration(r NodeRegistration) error {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(r.ID) || r.ID == api.LocalNodeID || r.Label == "" || (r.Join != api.NodeSSH && r.Join != api.NodeOutgoing) {
		return errors.New("invalid native node registration")
	}
	if r.Join == api.NodeSSH {
		if r.SSHDestination == "" || !filepath.IsAbs(r.Directory) || !filepath.IsAbs(r.HelperPath) {
			return errors.New("incomplete SSH node pairing")
		}
	} else if !filepath.IsAbs(r.SocketPath) {
		return errors.New("incomplete outgoing node pairing")
	}
	return nil
}

func (n *nativeNodeManagement) registration(id string) (NodeRegistration, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, r := range n.document.Nodes {
		if r.ID == id {
			return r, nil
		}
	}
	return NodeRegistration{}, errors.New("exact node is not enrolled")
}

func (n *nativeNodeManagement) agent(id string) (nodeplane.CatalogAgent, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil, errors.New("node management is closed")
	}
	if id == api.LocalNodeID {
		return n.local, nil
	}
	if c := n.clients[id]; c != nil {
		return c, nil
	}
	var r NodeRegistration
	for _, candidate := range n.document.Nodes {
		if candidate.ID == id {
			r = candidate
			break
		}
	}
	if r.ID == "" {
		return nil, errors.New("exact node is not enrolled")
	}
	var c nodeplane.CatalogAgent
	var err error
	if n.options.Dial != nil {
		c, err = n.options.Dial(n.ownerCtx, r)
	} else if r.Join == api.NodeOutgoing {
		if r.BrokerNodeID != "" && r.BrokerNodeID != api.LocalNodeID {
			var broker NodeRegistration
			for _, candidate := range n.document.Nodes {
				if candidate.ID == r.BrokerNodeID {
					broker = candidate
				}
			}
			if broker.Join != api.NodeSSH {
				return nil, errors.New("outgoing broker SSH pairing is unavailable")
			}
			c, err = nodeagent.NewSSHClient(n.ownerCtx, nodeagent.SSHConfig{Target: broker.SSHDestination}, broker.HelperPath, r.SocketPath, r.ID)
		} else {
			c, err = nodeagent.Dial(n.ownerCtx, r.SocketPath, r.ID)
		}
	} else {
		c, err = nodeagent.NewSSHForegroundClient(n.ownerCtx, nodeagent.SSHConfig{Target: r.SSHDestination}, r.HelperPath, r.Directory, r.ID)
	}
	if err != nil {
		return nil, err
	}
	n.clients[id] = c
	return c, nil
}

func (n *nativeNodeManagement) Catalog(ctx context.Context) (api.NodeCatalog, error) {
	c, err := n.local.Catalog(ctx)
	if err != nil {
		return c, err
	}
	// Sorting/appending aggregation must not mutate a cached source catalog.
	c.Nodes = append([]api.NodeInfo(nil), c.Nodes...)
	c.PendingOperations = append([]api.NodeOperationRef(nil), c.PendingOperations...)
	c.PendingEnrollments, err = n.pendingEnrollments()
	if err != nil {
		return api.NodeCatalog{}, err
	}
	n.mu.Lock()
	doc := n.document
	doc.Nodes = append([]NodeRegistration(nil), doc.Nodes...)
	n.mu.Unlock()
	for _, r := range doc.Nodes {
		fallback := api.NodeInfo{ID: r.ID, Label: r.Label, OS: api.NodeLinux, Join: r.Join, Runtimes: []api.NodeRuntime{}}
		client, err := n.agent(r.ID)
		if err == nil {
			remote, e := client.Catalog(ctx)
			if e == nil && len(remote.Nodes) == 1 && remote.Nodes[0].ID == r.ID {
				fallback = remote.Nodes[0]
				fallback.Label = r.Label
				fallback.Join = r.Join
				c.PendingOperations = append(c.PendingOperations, remote.PendingOperations...)
			}
		}
		c.Nodes = append(c.Nodes, fallback)
	}
	sort.Slice(c.Nodes, func(i, j int) bool { return c.Nodes[i].ID < c.Nodes[j].ID })
	c.ActiveBotNodeID = api.LocalNodeID
	if n.app != nil && n.app.product != nil {
		pairing := n.app.product.pairing
		c.ActiveBotNodeID = pairing.NodeID
		product := n.app.product
		product.mu.Lock()
		binding := ""
		if !product.closed && product.client != nil && product.connection == "ready" && (product.identity.Capabilities.RuntimeManagement || product.identity.Capabilities.Execution) {
			binding = product.managementBindingLocked()
		}
		product.mu.Unlock()
		c.PairedRuntime = &api.NodePairedRuntime{NodeID: pairing.NodeID, Binding: binding}
		c.WorkerTarget = nil // The product-only pairing does not attest Worker routes.
		found := false
		for _, node := range c.Nodes {
			found = found || node.ID == pairing.NodeID
		}
		if !found && pairing.NodeID != "" {
			label := pairing.Label
			if label == "" {
				label = "Connected Bot machine"
			}
			c.Nodes = append(c.Nodes, api.NodeInfo{ID: pairing.NodeID, Label: label, OS: api.NodeOSUnknown, Join: api.NodeSSH, Runtimes: []api.NodeRuntime{}})
		}
	} else if n.app != nil && n.app.Backend != nil {
		backend := n.app.Backend.ProviderInfo().ID
		if backend != "" {
			c.WorkerTarget = &api.WorkTarget{NodeID: api.LocalNodeID, Backend: backend, Role: api.RoleWorker}
		}
	}
	if n.options.ExecutionState != nil {
		c.ActiveBotNodeID, c.WorkerTarget = n.options.ExecutionState()
	}
	sort.Slice(c.Nodes, func(i, j int) bool { return c.Nodes[i].ID < c.Nodes[j].ID })
	c.Broker = nil
	if doc.Coordinator != "" {
		broker := api.NodeBroker{NodeID: doc.Coordinator, Reachable: false, Reason: "broker-unavailable"}
		if n.options.BrokerStatus != nil {
			if b, e := n.options.BrokerStatus(ctx, doc.Coordinator); e == nil && b.NodeID == doc.Coordinator {
				broker = b
			}
		}
		c.Broker = &broker
	}
	data, _ := json.Marshal(struct {
		Revision        string
		Nodes           []api.NodeInfo
		Broker          *api.NodeBroker
		ActiveBotNodeID string
		WorkerTarget    *api.WorkTarget
		PairedRuntime   *api.NodePairedRuntime
	}{strconv.FormatUint(doc.Revision, 10), c.Nodes, c.Broker, c.ActiveBotNodeID, c.WorkerTarget, c.PairedRuntime})
	digest := sha256.Sum256(data)
	c.Revision = hex.EncodeToString(digest[:])
	return c, nodeplane.ValidateCatalog(c)
}

func (n *nativeNodeManagement) Configuration(ctx context.Context, id string, b api.NodeBackend) (api.NodeRuntimeConfiguration, error) {
	c, e := n.agent(id)
	if e != nil {
		return api.NodeRuntimeConfiguration{}, e
	}
	return c.Configuration(ctx, id, b)
}
func (n *nativeNodeManagement) Manage(ctx context.Context, r nodeplane.ManagementRequest) (api.NodeOperationReceipt, error) {
	c, e := n.agent(r.Ref.NodeID)
	if e != nil {
		return api.NodeOperationReceipt{}, e
	}
	return c.Manage(ctx, r)
}
func (n *nativeNodeManagement) Reconcile(ctx context.Context, r api.NodeOperationRef) (api.NodeOperationReceipt, error) {
	c, e := n.agent(r.NodeID)
	if e != nil {
		return api.NodeOperationReceipt{}, e
	}
	return c.Reconcile(ctx, r)
}

func (n *nativeNodeManagement) addEnrollment(ctx context.Context, r api.NodeAddRequest, beforeMutation func() error, prepared func(NodeRegistration, api.NodeAddResult) error) (api.NodeAddResult, error) {
	c, err := n.Catalog(ctx)
	if err != nil {
		return api.NodeAddResult{}, err
	}
	if c.Revision != r.ExpectedRevision {
		return api.NodeAddResult{}, errors.New("node catalog changed")
	}
	n.mu.Lock()
	revision := n.document.Revision
	for _, existing := range n.document.Nodes {
		if r.Join == api.NodeSSH && existing.SSHDestination == r.SSHDestination {
			n.mu.Unlock()
			return api.NodeAddResult{}, errors.New("SSH machine is already enrolled")
		}
	}
	if len(n.document.Nodes) >= 16 {
		n.mu.Unlock()
		return api.NodeAddResult{}, errors.New("node enrollment limit reached")
	}
	n.mu.Unlock()
	var reg NodeRegistration
	var instructions *api.NodeJoinInstructions
	if n.options.Bootstrap != nil {
		if beforeMutation != nil {
			if err = beforeMutation(); err != nil {
				return api.NodeAddResult{}, err
			}
		}
		reg, err = n.options.Bootstrap(ctx, r)
	} else if r.Join == api.NodeSSH {
		ssh := nodeagent.SSHConfig{Target: r.SSHDestination}
		arch, e := nodeagent.ProbeArchitecture(ctx, ssh)
		if e != nil {
			return api.NodeAddResult{}, enrollmentPreflightError{nodeagent.ArchitectureProbeReason(e)}
		}
		artifact, e := n.options.Artifact(arch)
		if e != nil {
			return api.NodeAddResult{}, enrollmentPreflightError{"artifact"}
		}
		if e = nodeagent.VerifyArtifact(artifact); e != nil {
			return api.NodeAddResult{}, enrollmentPreflightError{"artifact"}
		}
		if beforeMutation != nil {
			if err = beforeMutation(); err != nil {
				return api.NodeAddResult{}, err
			}
		}
		dir, e := nodeagent.PrepareNodeDirectory(ctx, ssh)
		if e != nil {
			return api.NodeAddResult{}, e
		}
		if e = nodeagent.InstallVerified(ctx, nodeagent.BootstrapPlan{SSH: ssh, Artifact: artifact, Directory: dir}); e != nil {
			return api.NodeAddResult{}, e
		}
		reg = NodeRegistration{ID: "node-" + rand.Text(), Label: r.Label, Join: r.Join, SSHDestination: r.SSHDestination, Directory: dir, HelperPath: filepath.Join(dir, "caelis-agent")}
		if artifact.HostPath != "" {
			reg.HostHelperPath = filepath.Join(dir, "caelis-node")
		}
	} else {
		if beforeMutation != nil {
			if err = beforeMutation(); err != nil {
				return api.NodeAddResult{}, err
			}
		}
		reg = NodeRegistration{ID: "node-" + rand.Text(), Label: r.Label, Join: api.NodeOutgoing}
		var i api.NodeJoinInstructions
		if n.options.PrepareOutgoing != nil {
			reg, i, err = n.options.PrepareOutgoing(ctx, reg)
		} else {
			reg, i, err = n.DefaultOutgoingNodePlan(ctx, reg)
		}
		instructions = &i
	}
	if err != nil {
		return api.NodeAddResult{}, err
	}
	if reg.Join != r.Join || reg.Label != r.Label || reg.Join == api.NodeSSH && reg.SSHDestination != r.SSHDestination {
		return api.NodeAddResult{}, errors.New("bootstrap changed node enrollment intent")
	}
	if err = validateNodeRegistration(reg); err != nil {
		return api.NodeAddResult{}, err
	}
	var verified api.NodeInfo
	var verifiedClient nodeplane.CatalogAgent
	retained := false
	defer func() {
		if !retained && verifiedClient != nil {
			if closer, ok := verifiedClient.(io.Closer); ok {
				closer.Close()
			}
		}
	}()
	if reg.Join == api.NodeSSH {
		if n.options.Dial != nil {
			verifiedClient, err = n.options.Dial(n.ownerCtx, reg)
		} else {
			verifiedClient, err = nodeagent.NewSSHForegroundClient(n.ownerCtx, nodeagent.SSHConfig{Target: reg.SSHDestination}, reg.HelperPath, reg.Directory, reg.ID)
		}
		if err != nil {
			return api.NodeAddResult{}, err
		}
		catalog, e := verifiedClient.Catalog(ctx)
		if e != nil || len(catalog.Nodes) != 1 || catalog.Nodes[0].ID != reg.ID {
			return api.NodeAddResult{}, errors.New("verified node agent identity is unavailable")
		}
		verified = catalog.Nodes[0]
		verified.Label = reg.Label
		verified.Join = reg.Join
	}
	// Retain the exact verified registration/result before publishing its local
	// pairing, so an original receipt can prove success after response loss.
	if verified.ID == "" {
		verified = api.NodeInfo{ID: reg.ID, Label: reg.Label, OS: api.NodeLinux, Join: reg.Join, Runtimes: []api.NodeRuntime{}}
	}
	result := api.NodeAddResult{Node: verified, JoinInstructions: instructions}
	if prepared != nil {
		if err = prepared(reg, result); err != nil {
			return api.NodeAddResult{}, err
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.document.Revision != revision {
		return api.NodeAddResult{}, errors.New("node enrollment configuration changed during bootstrap")
	}
	for _, existing := range n.document.Nodes {
		if existing.ID == reg.ID {
			return api.NodeAddResult{}, errors.New("node identity already enrolled")
		}
	}
	next := n.document
	next.Nodes = append(append([]NodeRegistration(nil), next.Nodes...), reg)
	next.Revision++
	if err = localstate.Write(filepath.Join(n.directory, "config.json"), next); err != nil {
		return api.NodeAddResult{}, err
	}
	n.document = next
	if verifiedClient != nil {
		n.clients[reg.ID] = verifiedClient
		retained = true
	}
	return result, nil
}

func (n *nativeNodeManagement) Detect(ctx context.Context, id string) (api.NodeInfo, error) {
	c, e := n.agent(id)
	if e != nil {
		return api.NodeInfo{}, e
	}
	catalog, e := c.Catalog(ctx)
	if e != nil {
		return api.NodeInfo{}, e
	}
	for _, node := range catalog.Nodes {
		if node.ID == id {
			return node, nil
		}
	}
	return api.NodeInfo{}, errors.New("detected agent changed enrolled identity")
}
func (n *nativeNodeManagement) JoinInstructions(ctx context.Context, id string) (api.NodeJoinInstructions, error) {
	r, e := n.registration(id)
	if e != nil {
		return api.NodeJoinInstructions{}, e
	}
	if r.Join != api.NodeOutgoing {
		return api.NodeJoinInstructions{NodeID: id, State: api.NodeJoinConnected, Instructions: "Connected through the selected standard SSH destination."}, nil
	}
	if n.options.OutgoingInstructions == nil {
		return n.outgoingInstructions(ctx, r)
	}
	return n.options.OutgoingInstructions(ctx, r)
}

// DefaultOutgoingNodePlan prepares an actual private reverse-SSH slot. A
// remote SSH coordinator uses its enrolled helper; a local coordinator needs
// the user's already-authorized outbound SSH route and packaged native helper.
// It provisions no account/key/listener and never starts a background service.
func (n *nativeNodeManagement) DefaultOutgoingNodePlan(ctx context.Context, r NodeRegistration) (NodeRegistration, api.NodeJoinInstructions, error) {
	n.mu.Lock()
	coordinator := n.document.Coordinator
	n.mu.Unlock()
	if coordinator == "" {
		return r, api.NodeJoinInstructions{}, errors.New("choose a designated coordinator before outgoing enrollment")
	}
	r.BrokerNodeID = coordinator
	if coordinator == api.LocalNodeID {
		target := n.options.OutgoingSSHDestination
		if target == "" {
			target = os.Getenv("CAELIS_BOT_NODE_OUTGOING_SSH_TARGET")
		}
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._:-]{0,253}$`).MatchString(target) || strings.Contains(target, "::") {
			return r, api.NodeJoinInstructions{}, errors.New("an existing authorized SSH destination for this coordinator is required")
		}
		helper := n.options.JoinHelperPath
		if helper == "" {
			var err error
			helper, err = DefaultNativeJoinHelper()
			if err != nil {
				return r, api.NodeJoinInstructions{}, err
			}
		}
		if !filepath.IsAbs(helper) {
			return r, api.NodeJoinInstructions{}, errors.New("packaged native join helper is unavailable")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return r, api.NodeJoinInstructions{}, err
		}
		sum := sha256.Sum256([]byte(r.ID))
		directory := filepath.Join(home, ".caelis-bot-joins", hex.EncodeToString(sum[:8]))
		if len(filepath.Join(directory, "agent.sock")) >= 100 {
			return r, api.NodeJoinInstructions{}, errors.New("private join slot path is too long")
		}
		if err = os.MkdirAll(directory, 0700); err != nil {
			return r, api.NodeJoinInstructions{}, err
		}
		if err = nodeagent.CheckPrivateDirectory(directory); err != nil {
			return r, api.NodeJoinInstructions{}, err
		}
		r.Directory, r.SocketPath, r.HelperPath, r.SSHDestination = directory, filepath.Join(directory, "agent.sock"), helper, target
	} else {
		broker, err := n.registration(coordinator)
		if err != nil || broker.Join != api.NodeSSH {
			return r, api.NodeJoinInstructions{}, errors.New("outgoing coordinator requires an enrolled SSH machine")
		}
		directory, err := nodeagent.PrepareJoinDirectory(ctx, nodeagent.SSHConfig{Target: broker.SSHDestination}, r.ID)
		if err != nil {
			return r, api.NodeJoinInstructions{}, err
		}
		r.Directory, r.SocketPath, r.HelperPath, r.SSHDestination = directory, filepath.Join(directory, "agent.sock"), broker.HelperPath, broker.SSHDestination
	}
	i, err := n.outgoingInstructions(ctx, r)
	return r, i, err
}

func nodeShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (n *nativeNodeManagement) outgoingInstructions(_ context.Context, r NodeRegistration) (api.NodeJoinInstructions, error) {
	if r.Join != api.NodeOutgoing || r.SSHDestination == "" || !filepath.IsAbs(r.HelperPath) || !filepath.IsAbs(r.Directory) {
		return api.NodeJoinInstructions{}, errors.New("native outgoing SSH plan is incomplete")
	}
	// The two foreground commands keep their lifetimes explicit. The native
	// helper validates existing private directories and ordinary SSH policy.
	profile := `"$HOME/.local/share/caelis-bot/node-agent"`
	socket := `"$HOME/.local/share/caelis-bot/node-agent/agent.sock"`
	first := "umask 077; mkdir -p " + profile + "; caelis-agent serve-agent --directory " + profile + " --node-id " + nodeShellQuote(r.ID) + " --join outgoing"
	second := "caelis-agent join-agent --socket " + socket + " --ssh-target " + nodeShellQuote(r.SSHDestination) + " --join-helper " + nodeShellQuote(r.HelperPath) + " --join-directory " + nodeShellQuote(r.Directory)
	return api.NodeJoinInstructions{NodeID: r.ID, State: api.NodeJoinWaiting, Instructions: "On the target Linux machine, use the verified Caelis Agent and your existing SSH authorization. Run these commands in two separate terminals and keep both foreground commands running, then choose Detect node.\n\n" + first + "\n\n" + second}, nil
}

func (n *nativeNodeManagement) SetCoordinator(ctx context.Context, r api.NodeCoordinatorSelection) (api.NodeCatalog, error) {
	if err := GuardNodeRoamingCoordinator(n.app, r.NodeID); err != nil {
		return api.NodeCatalog{}, err
	}
	n.controlMu.Lock()
	defer n.controlMu.Unlock()
	c, err := n.Catalog(ctx)
	if err != nil {
		return c, err
	}
	if c.Revision != r.ExpectedRevision {
		return api.NodeCatalog{}, errors.New("node catalog changed")
	}
	n.mu.Lock()
	if r.NodeID != "" && r.NodeID != api.LocalNodeID {
		found := false
		for _, node := range n.document.Nodes {
			found = found || node.ID == r.NodeID
		}
		if !found {
			n.mu.Unlock()
			return api.NodeCatalog{}, errors.New("coordinator requires an enrolled node agent")
		}
	}
	next := n.document
	next.Coordinator = r.NodeID
	next.Revision++
	err = localstate.Write(filepath.Join(n.directory, "config.json"), next)
	if err == nil {
		n.document = next
	}
	n.mu.Unlock()
	if err != nil {
		return api.NodeCatalog{}, err
	}
	return n.Catalog(ctx)
}

func (n *nativeNodeManagement) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil
	}
	n.closed = true
	n.cancel()
	var result error
	for _, c := range n.clients {
		if closer, ok := c.(io.Closer); ok {
			result = errors.Join(result, closer.Close())
		}
	}
	return result
}

type nodeLocalConfiguration struct {
	app     *Application
	backend api.NodeBackend
}

func (p *nodeLocalConfiguration) Read(ctx context.Context) (api.RuntimeConfiguration, error) {
	if p.backend == api.NodeCodex {
		if p.app.Backend.ProviderInfo().ID != "codex" {
			return api.RuntimeConfiguration{}, errors.New("local Codex owner is inactive")
		}
		rev, conversation, _, err := p.app.Backend.NodeExecutionScopes(ctx)
		if err != nil {
			return api.RuntimeConfiguration{}, err
		}
		models, err := p.app.Backend.Models(ctx)
		if err != nil {
			return api.RuntimeConfiguration{}, err
		}
		return api.RuntimeConfiguration{Revision: rev, Main: conversation, Models: models, Team: api.RuntimeTeam{Available: false, Revision: rev, Models: models, Reason: "native-team-configuration-unavailable"}}, nil
	}
	settings, err := nodeLocalCaelisSettings(p.app, filepath.Join(p.app.root, "nodeplane", "local"), nil)
	if err != nil {
		return api.RuntimeConfiguration{}, err
	}
	return (&nodeagent.CaelisConfiguration{Settings: settings}).Read(ctx)
}
func (p *nodeLocalConfiguration) Change(ctx context.Context, r nodeplane.ManagementRequest) (api.RuntimeMutationResult, error) {
	if p.backend == api.NodeCodex {
		return p.app.Backend.ChangeNodeExecutionScopes(ctx, r)
	}
	settings, err := nodeLocalCaelisSettings(p.app, filepath.Join(p.app.root, "nodeplane", "local"), nil)
	if err != nil {
		return api.RuntimeMutationResult{}, err
	}
	return (&nodeagent.CaelisConfiguration{Settings: settings}).Change(ctx, r)
}

type nodeLocalCodexConfiguration struct{ nodeLocalConfiguration }

func (p *nodeLocalCodexConfiguration) ExecutionScopes(ctx context.Context) (api.WorkExecutionSettings, api.WorkExecutionSettings, error) {
	_, conversation, worker, err := p.app.Backend.NodeExecutionScopes(ctx)
	return conversation, worker, err
}
func nodeLocalHealth(ctx context.Context, a *Application, b api.NodeBackend) (nodeagent.NativeHealth, error) {
	var settings api.RuntimeSettings
	var err error
	if b == api.NodeCaelis && a.Backend.ProviderInfo().ID != "caelis" {
		// An inactive Node Runtime must observe the same designated private slot
		// as its configuration/setup ports. A blank ordinary alternate profile
		// would otherwise discover the user's unrelated default Caelis Host.
		settings, err = nodeLocalCaelisSettings(a, filepath.Join(a.root, "nodeplane", "local"), nil)
	} else {
		// Preserve ordinary active local Runtime semantics, including an
		// explicitly selected default Caelis Store.
		settings, err = a.Backend.SetupProfile(string(b))
	}
	if err != nil {
		return nodeagent.NativeHealth{}, err
	}
	if b == api.NodeCaelis {
		state, err := caelis.InspectNodeRuntimeHealth(ctx, settings)
		if err != nil {
			return nodeagent.NativeHealth{}, err
		}
		workerEligible := false
		if state.AuthenticationKnown && state.Authenticated && state.HealthKnown && state.Healthy && a.nodeRegistry != nil {
			_, err := a.nodeRegistry.WorkRuntimeFor(api.WorkTarget{NodeID: api.LocalNodeID, Backend: string(b), Role: api.RoleWorker})
			workerEligible = err == nil
		}
		return nodeagent.NativeHealth{AuthenticationKnown: state.AuthenticationKnown, Authenticated: state.Authenticated, HealthKnown: state.HealthKnown, Healthy: state.Healthy, WorkerEligible: workerEligible, SharedHost: true}, nil
	}
	state, err := a.Backend.InspectSetup(ctx, settings)
	if err != nil {
		return nodeagent.NativeHealth{}, err
	}
	workerEligible := false
	if state.State == "ready" && a.nodeRegistry != nil {
		_, err := a.nodeRegistry.WorkRuntimeFor(api.WorkTarget{NodeID: api.LocalNodeID, Backend: string(b), Role: api.RoleWorker})
		workerEligible = err == nil
	}
	return nodeagent.NativeHealth{AuthenticationKnown: state.State == "ready" || state.State == "login" || state.State == "models", Authenticated: state.State == "ready", HealthKnown: state.Installation.Installed, Healthy: state.State == "ready", WorkerEligible: workerEligible, SharedHost: b == api.NodeCaelis}, nil
}

var _ nodeplane.CatalogAgent = (*nativeNodeManagement)(nil)
var _ nodeplane.NodeSetup = (*nativeNodeManagement)(nil)
