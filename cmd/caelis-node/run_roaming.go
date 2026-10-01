package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodebroker"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
	"github.com/caelis-labs/caelis-bot/internal/roaming"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

type roamingCommand struct {
	reportStop                                                                                                                      func(error)
	ManagedAgent                                                                                                                    bool
	RuntimeDirectory                                                                                                                string
	WorkersFile                                                                                                                     string
	BrokerSSH                                                                                                                       nodeagent.SSHConfig
	NodeID, BotID, Backend, AgentDirectory, GenerationRoot, BrokerSocket, BrokerNodeID, BrokerHelper, AuthFile, CodexBinary, Listen string
	Join                                                                                                                            nodeagent.SSHConfig
	JoinHelper, JoinDirectory                                                                                                       string
}

func parseRoamingCommand(args []string, out io.Writer) (roamingCommand, error) {
	var c roamingCommand
	if len(args) == 0 || args[0] != "serve-roaming" {
		return c, errors.New("usage: caelis-node serve-roaming --node-id ID --bot-id ID --agent-directory ABS --generations ABS --broker-socket ABS --auth-file PRIVATE")
	}
	f := flag.NewFlagSet("serve-roaming", flag.ContinueOnError)
	f.SetOutput(out)
	f.StringVar(&c.NodeID, "node-id", "", "exact previously inspected enrolled machine identity")
	f.StringVar(&c.BotID, "bot-id", "", "stable Notebook Bot identity")
	f.StringVar(&c.Backend, "backend", "codex", "native managed backend; shared Hosts are ineligible")
	f.StringVar(&c.WorkersFile, "workers-file", "", "optional private exact approved Worker deployment configuration; excluded from Notebook")
	f.BoolVar(&c.ManagedAgent, "managed-agent", false, "use the fixed separate managed proof socket alongside an existing catalog agent")
	f.StringVar(&c.RuntimeDirectory, "runtime-directory", "", "existing enrolled target-native managed Runtime installer directory")
	f.StringVar(&c.AgentDirectory, "agent-directory", "", "existing enrolled same-user private agent directory")
	f.StringVar(&c.GenerationRoot, "generations", "", "private root for absent fresh Notebook generations")
	f.StringVar(&c.BrokerNodeID, "broker-node-id", "", "exact inspected designated broker enrollment")
	f.StringVar(&c.BrokerSSH.Target, "broker-ssh-target", "", "optional existing authorized SSH broker target")
	f.StringVar(&c.BrokerHelper, "broker-helper", "", "existing absolute native helper at SSH broker target")
	f.StringVar(&c.BrokerSocket, "broker-socket", "", "existing private broker Unix socket, optionally SSH-forwarded")
	f.StringVar(&c.AuthFile, "auth-file", "", "existing target-private application product token file")
	f.StringVar(&c.CodexBinary, "codex-binary", "", "optional explicit installed target-local Codex executable")
	f.StringVar(&c.Listen, "listen", "127.0.0.1:0", "literal loopback product listener after leased activation")
	f.StringVar(&c.Join.Target, "join-target", "", "optional existing authorized SSH destination for outgoing agent join")
	f.StringVar(&c.JoinHelper, "join-helper", "", "existing absolute native helper at outgoing join destination")
	f.StringVar(&c.JoinDirectory, "join-directory", "", "existing destination private slot paired by the broker")
	if err := f.Parse(args[1:]); err != nil {
		return c, err
	}
	if f.NArg() != 0 || c.NodeID == "" || c.BotID == "" || c.BrokerNodeID == "" || c.Backend != "codex" || !filepath.IsAbs(c.AgentDirectory) || !filepath.IsAbs(c.GenerationRoot) || !filepath.IsAbs(c.BrokerSocket) || !filepath.IsAbs(c.AuthFile) {
		return c, errors.New("exact enrolled identity, owned Codex target and absolute private native paths required")
	}
	if c.RuntimeDirectory != "" && !filepath.IsAbs(c.RuntimeDirectory) {
		return c, errors.New("native runtime directory must be absolute")
	}
	if (c.BrokerSSH.Target == "") != (c.BrokerHelper == "") {
		return c, errors.New("broker SSH requires existing target and helper")
	}
	if c.CodexBinary != "" && !filepath.IsAbs(c.CodexBinary) {
		return c, errors.New("installed Codex executable must be absolute")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return c, errors.New("managed product listener requires literal loopback")
	}
	if c.Join.Target != "" || c.JoinHelper != "" || c.JoinDirectory != "" {
		if c.Join.Target == "" || c.JoinHelper == "" || c.JoinDirectory == "" {
			return c, errors.New("outgoing join requires exact existing target/helper/private directory")
		}
	}
	return c, nil
}

// roamingProofOwner keeps a single exact native generation visible through the
// persistent agent while candidates remain closed to work. It never accepts a
// controllable request flag or reconstructs authority from an on-disk receipt.
type roamingProofOwner struct {
	mu         sync.RWMutex
	target     api.WorkTarget
	botID      string
	generation uint64
	port       nodeplane.RuntimeProofPort
	native     *app.Application
	health     func(context.Context) (nodeagent.NativeHealth, error)
}

func (h *roamingProofOwner) register(target api.WorkTarget, port nodeplane.RuntimeProofPort) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if target != h.target || port == nil {
		return nodecoord.ErrIneligible
	}
	native, ok := port.(*app.Application)
	if !ok {
		return nodecoord.ErrIneligible
	}
	h.generation++
	h.port = port
	h.native = native
	return nil
}
func (h *roamingProofOwner) clear() {
	h.mu.Lock()
	h.generation++
	h.port = nil
	h.native = nil
	h.mu.Unlock()
}
func (h *roamingProofOwner) read(ctx context.Context, target api.WorkTarget, requireHealthy bool) (nodeplane.RuntimeEligibility, error) {
	h.mu.RLock()
	port, generation := h.port, h.generation
	h.mu.RUnlock()
	if target != h.target || port == nil {
		return nodeplane.RuntimeEligibility{}, nodecoord.ErrIneligible
	}
	if requireHealthy {
		native, err := h.health(ctx)
		if err != nil || !native.HealthKnown || !native.Healthy || !native.AuthenticationKnown || !native.Authenticated {
			return nodeplane.RuntimeEligibility{}, nodecoord.ErrIneligible
		}
	}
	proof, err := port.ReadRuntimeProof(ctx, target)
	if err != nil {
		return nodeplane.RuntimeEligibility{}, err
	}
	h.mu.RLock()
	same := h.generation == generation
	h.mu.RUnlock()
	if !same || proof.Snapshot.BotID != h.botID || proof.Proof.NodeID != target.NodeID || string(proof.Proof.Backend) != target.Backend || !proof.Proof.Controllable {
		return nodeplane.RuntimeEligibility{}, nodecoord.ErrIneligible
	}
	return proof, ctx.Err()
}
func (h *roamingProofOwner) ReadRuntimeProof(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	return h.read(ctx, target, true)
}
func (h *roamingProofOwner) Health(ctx context.Context, b api.NodeBackend) (nodeagent.NativeHealth, error) {
	if b != api.NodeCodex {
		return nodeagent.NativeHealth{}, nil
	}
	native, err := h.health(ctx)
	if err != nil {
		return native, err
	}
	proof, proofErr := h.read(ctx, h.target, false)
	if proofErr == nil {
		native.ManagedOwner = true
		native.Fenceable = true
		native.BotEligible = proof.Proof.Controllable && (proof.SafeIdle && !proof.Pending && !proof.Unknown || proof.LeaseEpoch != "")
	}
	return native, nil
}
func (h *roamingProofOwner) application() *app.Application {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.native
}

func writeRoamingNativeSettings(profile, agentDirectory, binary string) error {
	preferences, err := nodeagent.ReadExecutionPreferences(agentDirectory)
	if err != nil {
		return err
	}
	execution := api.ExecutionSettings{ApprovalMode: "auto", Model: preferences.Conversation.Model, Effort: preferences.Conversation.Effort, ServiceTier: preferences.Conversation.ServiceTier}
	if err = api.ValidateExecutionSettings(execution); err != nil {
		return err
	}
	if err = localstate.Write(filepath.Join(profile, "runtime.json"), api.RuntimeSettings{Runtime: "codex", CLIPath: binary}); err != nil {
		return err
	}
	if err = localstate.Write(filepath.Join(profile, "execution.json"), execution); err != nil {
		return err
	}
	return localstate.Write(filepath.Join(profile, "work-execution.json"), preferences.Worker)
}

// runRoaming combines a real managed native application, one persistent private
// proof agent and the optional broker. Existing local serve-bot is unchanged.
func runRoaming(ctx context.Context, args []string, out io.Writer) error {
	c, err := parseRoamingCommand(args, out)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	return runRoamingCommand(ctx, c, out, bindRoamingPower)
}

func runRoamingCommand(ctx context.Context, c roamingCommand, out io.Writer, power func(context.Context, func(), func()) (func(), error)) error {
	var err error
	workerConfigs, err := loadRoamingWorkers(c.WorkersFile)
	if err != nil {
		return err
	}
	if err = nodeagent.CheckPrivateDirectory(c.AgentDirectory); err != nil {
		return err
	}
	// Enrollment is prerequisite; this command cannot manufacture a replacement
	// node identity that silently changes an existing broker pairing.
	identityBytes, err := readBrokerFile(filepath.Join(c.AgentDirectory, "node.json"), 1024)
	if err != nil {
		return err
	}
	var identity struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(identityBytes, &identity) != nil || identity.ID != c.NodeID {
		return errors.New("managed node must match existing inspected enrollment")
	}
	if err = os.MkdirAll(c.GenerationRoot, 0700); err != nil {
		return err
	}
	if err = nodeagent.CheckPrivateDirectory(c.GenerationRoot); err != nil {
		return err
	}
	if c.CodexBinary == "" && c.RuntimeDirectory != "" {
		manager, e := runtimemanagement.New(c.RuntimeDirectory)
		if e != nil {
			return e
		}
		c.CodexBinary, e = manager.BinaryPath("codex")
		if e != nil {
			return e
		}
	}
	ownerLock := ".agent-owner.lock"
	agentSocketName := "agent.sock"
	if c.ManagedAgent {
		ownerLock = ".managed-owner.lock"
		agentSocketName = "managed.sock"
	}
	unlock, err := lockProfile(filepath.Join(c.AgentDirectory, ownerLock))
	if err != nil {
		return err
	}
	defer unlock()
	token, err := readProductToken(c.AuthFile)
	if err != nil {
		return err
	}
	var broker *nodebroker.Client
	if c.BrokerSSH.Target != "" {
		broker, err = nodebroker.NewSSHClient(ctx, c.BrokerSSH, c.BrokerHelper, c.BrokerSocket, c.BrokerNodeID)
	} else {
		broker, err = nodebroker.DialUnixForBroker(ctx, c.BrokerSocket, c.BrokerNodeID)
	}
	if err != nil {
		return err
	}
	defer broker.Close()
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	target := api.WorkTarget{NodeID: c.NodeID, Backend: c.Backend, Role: api.RoleBot}
	config := &nodeagent.CodexConfiguration{Directory: c.AgentDirectory, Binary: c.CodexBinary}
	holder := &roamingProofOwner{target: target, botID: c.BotID, health: config.Health}
	control := &roamingManagedControl{holder: holder, broker: broker, token: token, productHTTP: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	defer holder.clear()
	binaries := map[api.NodeBackend]string{}
	if c.CodexBinary != "" {
		binaries[api.NodeCodex] = c.CodexBinary
	}
	service, err := nodeagent.New(nodeagent.Options{Directory: c.AgentDirectory, NodeID: c.NodeID, Label: "Managed Bot node", Join: api.NodeSSH, Binaries: binaries, Configurations: map[api.NodeBackend]nodeagent.NativeConfiguration{api.NodeCodex: config}, RuntimeOwner: holder, ManagedProduct: control, Health: holder.Health})
	if err != nil {
		return err
	}
	agentSocket := filepath.Join(c.AgentDirectory, agentSocketName)
	if len(agentSocket) >= 100 {
		return errors.New("managed agent socket path exceeds private IPC limit")
	}
	agentListener, err := net.Listen("unix", agentSocket)
	if err != nil {
		return errors.New("private agent already owned or unavailable")
	}
	defer agentListener.Close()
	if err = os.Chmod(agentSocket, 0600); err != nil {
		return err
	}
	agentDone := make(chan error, 1)
	go func() { agentDone <- nodeagent.Serve(life, agentListener, service) }()
	joinDone := make(chan error, 1)
	if c.Join.Target != "" {
		go func() { joinDone <- nodeagent.Join(life, c.Join, c.JoinHelper, c.JoinDirectory, agentSocket) }()
	}
	var currentServer atomic.Pointer[productrpc.Server]
	var currentProfile string
	var uploads atomic.Pointer[productrpc.UploadStore]
	host := app.Host{ResolveFiles: func(ids []string) ([]api.InputFile, error) {
		store := uploads.Load()
		if store == nil {
			return nil, productrpc.ErrUnsupported
		}
		return store.Resolve(ids)
	}, ConsumeFiles: func([]string) {}, BindLeasePower: func(suspend, wake func()) (func(), error) { return power(life, suspend, wake) }, OpenURL: func(string) error { return productrpc.ErrUnsupported }, RevealFile: func(string) error { return productrpc.ErrUnsupported }, Observe: func(api.Snapshot) {
		if s := currentServer.Load(); s != nil {
			s.NotifySnapshot()
		}
	}, ObserveTasks: func([]api.TaskPreview) {
		if s := currentServer.Load(); s != nil {
			s.NotifySnapshot()
		}
	}}
	factory := app.ManagedNodeFactory(host, app.ManagedNodeOptions{BrokerNodeID: c.BrokerNodeID})
	runner, err := roaming.NewRunner(roaming.RunnerOptions{BotID: c.BotID, Target: target, GenerationRoot: c.GenerationRoot, Broker: broker, RegisterOwner: holder.register, Factory: func(ctx context.Context, profile string, target api.WorkTarget) (roaming.ManagedRuntime, *roaming.Guard, error) {
		if err := writeRoamingNativeSettings(profile, c.AgentDirectory, c.CodexBinary); err != nil {
			return nil, nil, err
		}
		store, err := productrpc.OpenUploads(filepath.Join(profile, "Product", "Uploads"))
		if err != nil {
			return nil, nil, err
		}
		uploads.Store(store)
		currentProfile = profile
		native, guard, err := factory(ctx, profile, target)
		if err != nil {
			return nil, nil, err
		}
		actual, ok := native.(*app.Application)
		if !ok {
			guard.Revoke()
			_ = native.Close()
			return nil, nil, errors.New("managed native application unavailable")
		}
		if err = connectRoamingWorkers(ctx, actual.Backend, workerConfigs); err != nil {
			guard.Revoke()
			_ = native.Close()
			return nil, nil, err
		}
		return native, guard, nil
	}})
	if err != nil {
		return err
	}
	control.mu.Lock()
	control.runner = runner
	control.mu.Unlock()
	defer func() {
		cancel()
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = runner.Stop(stopCtx)
	}()
	if err = json.NewEncoder(out).Encode(struct {
		NodeID string `json:"nodeId"`
		Socket string `json:"socket"`
		State  string `json:"state"`
	}{c.NodeID, agentSocket, "standby"}); err != nil {
		return err
	}
	retry := time.NewTicker(nodeplane.DefaultHeartbeatInterval)
	defer retry.Stop()
	for {
		bounded, stop := context.WithTimeout(life, 15*time.Second)
		control.mu.Lock()
		if control.disabled.Load() {
			err = nodecoord.ErrConflict
		} else if _, latestErr := broker.LatestSnapshot(bounded, c.BotID); latestErr != nil {
			err = latestErr
		} else {
			err = runner.Activate(bounded)
		}
		control.mu.Unlock()
		stop()
		if err == nil {
			break
		}
		if !errors.Is(err, nodecoord.ErrConflict) && !errors.Is(err, nodecoord.ErrUnavailable) && !errors.Is(err, nodecoord.ErrIneligible) && !errors.Is(err, nodecoord.ErrSnapshot) && !errors.Is(err, roaming.ErrSnapshotChanged) {
			return err
		}
		select {
		case <-retry.C:
		case err = <-agentDone:
			return err
		case err = <-joinDone:
			return err
		case <-life.Done():
			return life.Err()
		}
	}
	native := holder.application()
	if native == nil {
		return errors.New("leased native application unavailable")
	}
	productCtx, stopProduct := context.WithCancel(life)
	defer stopProduct()
	stopped := make(chan productrpc.Result, 1)
	var explicitStop atomic.Bool
	port := productrpc.ServicePort{Service: native.Backend, Stop: func(ctx context.Context) error {
		explicitStop.Store(true)
		err := runner.Stop(ctx)
		if c.reportStop != nil {
			c.reportStop(err)
		}
		if err != nil {
			explicitStop.Store(false)
		}
		return err
	}}
	uploads.Load().Artifacts = func(ctx context.Context, id string) (productrpc.Resource, io.ReadCloser, error) {
		artifact, err := native.Backend.ReadProductArtifact(ctx, id)
		if err != nil {
			return productrpc.Resource{}, nil, err
		}
		return productrpc.Resource{ID: id, Name: artifact.Name, Size: artifact.Size, SHA256: artifact.SHA256}, io.NopCloser(bytes.NewReader(artifact.Bytes)), nil
	}
	execution := func(scope productmanagement.Scope) (productmanagement.ExecutionPort, error) {
		return productmanagement.NewExecution(scope, native.Backend)
	}
	server, err := productrpc.NewServer(port, productrpc.Options{Context: productCtx, NodeID: c.NodeID, BotID: productrpc.ProfileBotID(c.BotID), Token: token, JournalFile: filepath.Join(currentProfile, "Product", "receipts.json"), Resources: uploads.Load(), Capabilities: productrpc.Capabilities{Files: true, Interrupt: port.ExactInterruptAvailable()}, Execution: execution, OnStopped: func(r productrpc.Result) { stopped <- r }})
	if err != nil {
		return err
	}
	currentServer.Store(server)
	defer currentServer.Store(nil)
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	currentLease, err := broker.CurrentLease(life, c.BotID)
	if err != nil {
		return err
	}
	control.descriptor.Store(&nodeagent.ManagedProductEndpoint{BotID: c.BotID, Lease: currentLease, Identity: server.Identity(), Endpoint: "http://" + listener.Addr().String(), AuthFile: c.AuthFile})
	defer control.descriptor.Store(nil)
	if err = json.NewEncoder(out).Encode(struct {
		Endpoint string              `json:"endpoint"`
		Identity productrpc.Identity `json:"identity"`
	}{"http://" + listener.Addr().String(), server.Identity()}); err != nil {
		return err
	}
	productDone := make(chan error, 1)
	go func() { productDone <- server.Serve(listener) }()
	runDone := make(chan error, 1)
	go func() { runDone <- runner.Run(life) }()
	select {
	case err = <-runDone:
		if control.disabled.Load() {
			select {
			case <-life.Done():
				return life.Err()
			case err = <-agentDone:
				return err
			case err = <-joinDone:
				return err
			}
		}
		if explicitStop.Load() {
			select {
			case result := <-stopped:
				if result.Outcome == "accepted" {
					return nil
				}
				return errors.New("managed stop outcome unresolved")
			case <-life.Done():
				return life.Err()
			}
		}
		stopProduct()
		_ = listener.Close()
		return err
	case err = <-productDone:
		return err
	case result := <-stopped:
		if result.Outcome != "accepted" {
			return errors.New("managed stop outcome unresolved")
		}
		return nil
	case err = <-agentDone:
		return err
	case err = <-joinDone:
		return err
	case <-life.Done():
		return life.Err()
	}
}
