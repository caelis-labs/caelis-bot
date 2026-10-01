//go:build darwin || linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/workerlease"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type roamingOwnedWorker struct {
	owner  *nodeworker.Owner
	socket string
	cancel context.CancelFunc
	done   chan error
	err    error
}
type roamingOwnedWorkers struct {
	mu      sync.Mutex
	life    context.Context
	command roamingCommand
	helper  string
	reader  nodeplane.WorkLeaseReader
	power   func(context.Context, func(), func()) (func(), error)
	sources map[string]string
	workers map[workerwire.Pair]*roamingOwnedWorker
}

func newRoamingOwnedWorkers(life context.Context, c roamingCommand, p roamingWorkerPlan, helper string, reader nodeplane.WorkLeaseReader, power func(context.Context, func(), func()) (func(), error)) *roamingOwnedWorkers {
	result := &roamingOwnedWorkers{life: life, command: c, helper: helper, reader: reader, power: power, sources: map[string]string{}, workers: map[workerwire.Pair]*roamingOwnedWorker{}}
	for _, node := range p.Nodes {
		result.sources[node.ID] = node.Backend
	}
	return result
}
func (w *roamingOwnedWorkers) resolve(ctx context.Context, pair workerwire.Pair) (nodeagent.NativeWorkerProxyEndpoint, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	c := w.command
	if ctx.Err() != nil {
		return nodeagent.NativeWorkerProxyEndpoint{}, ctx.Err()
	}
	if workerwire.ValidateRelayPair(pair) != nil || pair.Target != (api.WorkTarget{NodeID: c.NodeID, Backend: c.Backend, Role: api.RoleWorker}) || pair.BotID != api.ProfileBotID(c.BotID) || w.sources[pair.SourceNode] != pair.SourceBackend {
		return nodeagent.NativeWorkerProxyEndpoint{}, errors.New("Worker source/target is outside approved native deployment")
	}
	if existing := w.workers[pair]; existing != nil {
		if existing.err != nil {
			return nodeagent.NativeWorkerProxyEndpoint{}, existing.err
		}
		if lease, ok := existing.owner.Runtime().(api.LeaseAwareWorkRuntime); !ok || !lease.LeaseAwareAdmission() {
			return nodeagent.NativeWorkerProxyEndpoint{}, errors.New("original Worker generation is fenced; retained receipts cannot be replayed")
		}
		return nodeagent.NativeWorkerProxyEndpoint{Pair: pair, Socket: existing.socket}, nil
	}
	key, _ := json.Marshal(pair)
	sum := sha256.Sum256(key)
	directory := filepath.Join(c.AgentDirectory, "w"+hex.EncodeToString(sum[:])[:12])
	socket := filepath.Join(directory, "s")
	if len(socket) >= 100 {
		return nodeagent.NativeWorkerProxyEndpoint{}, errors.New("private Worker path exceeds native IPC limit")
	}
	if err := prepareWorkerDirectory(directory); err != nil {
		return nodeagent.NativeWorkerProxyEndpoint{}, err
	}
	if err := workerwire.BindPair(directory, pair, false); err != nil {
		return nodeagent.NativeWorkerProxyEndpoint{}, err
	}
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		return nodeagent.NativeWorkerProxyEndpoint{}, errors.New("previous Worker endpoint retained; original owner cannot be replaced")
	}
	entry := &roamingOwnedWorker{socket: socket}
	// A failed/unknown native construction is retained instead of automatically
	// allocating a replacement generation for the same source pairing.
	w.workers[pair] = entry
	preferences, err := nodeagent.ReadExecutionPreferences(c.AgentDirectory)
	if err != nil {
		entry.err = err
		return nodeagent.NativeWorkerProxyEndpoint{}, err
	}
	source := workerwire.SourceProvider()
	var native nodeworker.Client
	if c.Backend == "codex" {
		native = codex.NewWorker(codex.WorkerOptions{Target: pair.Target, Pair: &pair, Directory: directory, WorkRoot: filepath.Join(directory, "Tasks"), Binary: c.CodexBinary, Execution: preferences.Worker, Source: source, Lease: &codex.WorkerLeaseOptions{HelperPath: w.helper, BrokerNodeID: c.BrokerNodeID, RawBotID: c.BotID, SourceNode: pair.SourceNode, SourceBackend: pair.SourceBackend, Reader: w.reader, BindPower: w.power}})
	} else {
		// This exact designated target store must be exclusively ownable. A shared
		// Host or another active owner fails its native lock/readiness checks.
		leased, buildErr := caelis.NewLeasedWorker(ctx, caelis.WorkerOptions{Target: pair.Target, Directory: directory, Execution: preferences.Worker, Source: source, Workspace: roamingWorkerWorkspace{root: filepath.Join(directory, "Tasks")}}, caelis.OwnedHostOptions{NodeID: c.NodeID, Binary: c.CaelisBinary, Store: c.CaelisStore, WatchdogHelper: w.helper}, workerlease.Options{BrokerNodeID: c.BrokerNodeID, BotID: c.BotID, SourceNode: pair.SourceNode, SourceBackend: pair.SourceBackend, Reader: w.reader})
		if buildErr != nil {
			entry.err = buildErr
			return nodeagent.NativeWorkerProxyEndpoint{}, buildErr
		}
		native = &roamingPairedCaelisWorker{LeasedWorker: leased, pair: pair}
	}
	owner := nodeworker.New(native)
	entry.owner = owner
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = owner.Start(bounded)
	cancel()
	if err != nil {
		entry.err = err
		_ = owner.Stop(context.Background())
		return nodeagent.NativeWorkerProxyEndpoint{}, err
	}
	server, err := workerwire.NewServer(owner, pair)
	if err != nil {
		entry.err = err
		_ = owner.Stop(context.Background())
		return nodeagent.NativeWorkerProxyEndpoint{}, err
	}
	life, stop := context.WithCancel(w.life)
	entry.cancel = stop
	entry.done = make(chan error, 1)
	ready := make(chan struct{})
	go func() { entry.done <- server.ServeUnixReady(life, socket, func() { close(ready) }) }()
	select {
	case <-ready:
		return nodeagent.NativeWorkerProxyEndpoint{Pair: pair, Socket: socket}, nil
	case err := <-entry.done:
		entry.err = err
		stop()
		_ = owner.Stop(context.Background())
		return nodeagent.NativeWorkerProxyEndpoint{}, err
	case <-ctx.Done():
		entry.err = ctx.Err()
		stop()
		_ = owner.Stop(context.Background())
		return nodeagent.NativeWorkerProxyEndpoint{}, ctx.Err()
	}
}

// A designated Caelis store has one foreground owner. A standby Worker keeps
// that store until its source lease is fenced; candidate Bot creation must wait.
func (w *roamingOwnedWorkers) CaelisStoreInUse() bool {
	if w.command.Backend != "caelis" {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, worker := range w.workers {
		if worker.owner != nil {
			if runtime, ok := worker.owner.Runtime().(api.LeaseAwareWorkRuntime); ok && runtime.LeaseAwareAdmission() {
				return true
			}
		}
	}
	return false
}
func (w *roamingOwnedWorkers) Close() error {
	var result error
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, worker := range w.workers {
		if worker.cancel != nil {
			worker.cancel()
		}
		if worker.owner != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			result = errors.Join(result, worker.owner.Stop(ctx))
			cancel()
		}
	}
	return result
}

type roamingPairedCaelisWorker struct {
	*caelis.LeasedWorker
	pair workerwire.Pair
}

func (w *roamingPairedCaelisWorker) WorkerPair() workerwire.Pair { return w.pair }

var roamingTaskID = regexp.MustCompile(`^task-[a-f0-9]{32}$`)

type roamingWorkerWorkspace struct{ root string }

func (w roamingWorkerWorkspace) ResolveWorkWorkspace(ctx context.Context, id, requested string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !roamingTaskID.MatchString(id) {
		return "", errors.New("invalid Worker task identity")
	}
	if requested != "" {
		return api.ResolveTaskWorkspace(requested)
	}
	return filepath.Join(w.root, id), nil
}
func (w roamingWorkerWorkspace) PrepareWorkWorkspace(ctx context.Context, id, workspace string, selected bool) error {
	expected, err := w.ResolveWorkWorkspace(ctx, id, "")
	if err != nil {
		return err
	}
	if selected {
		canonical, err := api.ResolveTaskWorkspace(workspace)
		if err != nil {
			return err
		}
		if canonical != workspace {
			return errors.New("Worker selected workspace redirected")
		}
		return nil
	}
	if workspace != expected {
		return errors.New("Worker workspace differs from owned root")
	}
	if err = os.MkdirAll(w.root, 0700); err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(w.root)
	if err != nil || canonical != w.root {
		return errors.New("Worker workspace root redirected")
	}
	if err = os.Mkdir(workspace, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	canonical, err = api.ResolveTaskWorkspace(workspace)
	if err != nil || canonical != workspace {
		return errors.New("Worker workspace redirected")
	}
	return nil
}
