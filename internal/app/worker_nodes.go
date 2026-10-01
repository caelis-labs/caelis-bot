package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
)

type workerNodeAdapter interface {
	Probe(context.Context) (backend.WorkerNodeFacts, error)
	Connect(context.Context) (api.WorkRuntime, error)
	Close(context.Context) error
}

type workerNodeFactory func(backend.WorkerNodeConfig, string) (workerNodeAdapter, error)

type workerNodeDocument struct {
	Version int                        `json:"version"`
	Nodes   []backend.WorkerNodeConfig `json:"nodes"`
}

// Loading candidate configuration is entirely offline. A failed optional file
// affects only node setup; the direct local resident and Worker remain usable.
type workerNodeController struct {
	operation sync.Mutex
	mu        sync.Mutex
	path      string
	registry  *nodes.Registry
	factory   workerNodeFactory
	document  workerNodeDocument
	views     map[api.WorkTarget]backend.WorkerNodeView
	active    map[api.WorkTarget]workerNodeAdapter
	runtimes  map[api.WorkTarget]api.WorkRuntime
	revision  uint64
	issue     string
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
}

func openWorkerNodes(filename string, registry *nodes.Registry, factory workerNodeFactory) *workerNodeController {
	ctx, cancel := context.WithCancel(context.Background())
	c := &workerNodeController{path: filename, registry: registry, factory: factory, document: workerNodeDocument{Version: 1}, views: map[api.WorkTarget]backend.WorkerNodeView{}, active: map[api.WorkTarget]workerNodeAdapter{}, runtimes: map[api.WorkTarget]api.WorkRuntime{}, revision: 1, ctx: ctx, cancel: cancel}
	document, err := loadWorkerNodeDocument(filename)
	if err != nil {
		c.issue = "config_unreadable"
		return c
	}
	c.document = document
	for _, config := range document.Nodes {
		c.views[configuredWorkerTarget(config)] = backend.WorkerNodeView{Config: config, State: string(nodes.Candidate)}
		_ = c.register(config, nodes.Candidate, nil)
	}
	return c
}

var workerNodeID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var workerSSH = regexp.MustCompile(`^[A-Za-z0-9_.@:\[\]-]+$`)

func validateWorkerNode(config backend.WorkerNodeConfig) error {
	if config.Backend != "" && config.Backend != "caelis" && config.Backend != "codex" {
		return errors.New("worker backend is invalid")
	}
	if !workerNodeID.MatchString(config.ID) || config.ID == api.LocalNodeID || strings.TrimSpace(config.Label) == "" || len(config.Label) > 128 || strings.ContainsAny(config.Label, "\x00\r\n") {
		return errors.New("worker node identity is invalid")
	}
	if !workerSSH.MatchString(config.SSH) || strings.HasPrefix(config.SSH, "-") || len(config.SSH) > 256 {
		return errors.New("worker SSH destination is invalid")
	}
	for _, value := range []string{config.Store, config.WorkspaceRoot, config.Socket} {
		if value != "" && (!path.IsAbs(value) || path.Clean(value) != value || strings.ContainsAny(value, "\x00\r\n") || len(value) > 4096) {
			return errors.New("worker paths must be clean absolute target paths")
		}
	}
	if config.WorkspaceRoot == "" {
		return errors.New("worker workspace root is required")
	}
	if config.Backend == "codex" && (config.Socket == "" || config.Store != "") {
		return errors.New("Codex Worker requires an explicit private native socket")
	}
	if config.Backend != "codex" && config.Socket != "" {
		return errors.New("Caelis Worker does not accept a Codex native socket")
	}
	if config.Helper != "" && (len(config.Helper) > 4096 || strings.ContainsAny(config.Helper, " \t\r\n\x00") || strings.HasPrefix(config.Helper, "-") || strings.Contains(config.Helper, "..")) {
		return errors.New("worker helper executable is invalid")
	}
	return nil
}

func workerTarget(id string) api.WorkTarget {
	return api.WorkTarget{NodeID: id, Backend: "caelis", Role: api.RoleWorker}
}

func configuredWorkerTarget(config backend.WorkerNodeConfig) api.WorkTarget {
	target := workerTarget(config.ID)
	if config.Backend != "" {
		target.Backend = config.Backend
	}
	return target
}

func (c *workerNodeController) register(config backend.WorkerNodeConfig, state nodes.Availability, runtime api.WorkRuntime) error {
	return c.registry.Set(nodes.Node{ID: config.ID, Label: config.Label}, nodes.Capability{Target: configuredWorkerTarget(config), State: state}, runtime)
}

func (c *workerNodeController) Snapshot() backend.WorkerNodeSetup {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

func (c *workerNodeController) snapshotLocked() backend.WorkerNodeSetup {
	snapshot := backend.WorkerNodeSetup{Revision: c.revision, Issue: c.issue, Nodes: []backend.WorkerNodeView{}}
	for _, config := range c.document.Nodes {
		snapshot.Nodes = append(snapshot.Nodes, c.views[configuredWorkerTarget(config)])
	}
	return snapshot
}

func (c *workerNodeController) checkLocked(revision uint64) error {
	if c.closed || c.issue != "" {
		return errors.New("worker node setup is unavailable; existing configuration was preserved")
	}
	if revision != c.revision {
		return errors.New("worker node configuration changed; refresh before continuing")
	}
	return nil
}

func (c *workerNodeController) Save(config backend.WorkerNodeConfig, revision uint64) (backend.WorkerNodeSetup, error) {
	c.operation.Lock()
	defer c.operation.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.checkLocked(revision); err != nil {
		return c.snapshotLocked(), err
	}
	if config.ID == "" {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return c.snapshotLocked(), err
		}
		config.ID = "node-" + hex.EncodeToString(random[:])
	}
	if err := validateWorkerNode(config); err != nil {
		return c.snapshotLocked(), err
	}
	for _, previous := range c.document.Nodes {
		if previous.ID == config.ID && previous.SSH != config.SSH {
			return c.snapshotLocked(), errors.New("use a new worker node for a different SSH machine association")
		}
	}
	index := slices.IndexFunc(c.document.Nodes, func(value backend.WorkerNodeConfig) bool {
		return configuredWorkerTarget(value) == configuredWorkerTarget(config)
	})
	if index < 0 && len(c.document.Nodes) >= 16 {
		return c.snapshotLocked(), errors.New("worker node limit reached")
	}
	if index >= 0 {
		previous := c.document.Nodes[index]
		previous.Label = config.Label
		if previous != config {
			// Target identity must not change under durable task/native bindings.
			return c.snapshotLocked(), errors.New("use a new worker node for a different connection or workspace root")
		}
	}
	document := c.document
	document.Nodes = slices.Clone(document.Nodes)
	if index < 0 {
		document.Nodes = append(document.Nodes, config)
	} else {
		document.Nodes[index] = config
	}
	for i := range document.Nodes {
		if document.Nodes[i].ID == config.ID {
			document.Nodes[i].Label = config.Label
		}
	}
	if err := localstate.Write(c.path, document); err != nil {
		return c.snapshotLocked(), errors.New("worker node configuration could not be saved")
	}
	c.document = document
	for _, sibling := range document.Nodes {
		if sibling.ID != config.ID {
			continue
		}
		target := configuredWorkerTarget(sibling)
		view, exists := c.views[target]
		view.Config = sibling
		if !exists {
			view.State = string(nodes.Candidate)
		}
		c.views[target] = view
		var runtime api.WorkRuntime
		if view.State == string(nodes.Ready) {
			runtime = c.runtimes[target]
		}
		_ = c.register(sibling, nodes.Availability(view.State), runtime)
	}
	c.revision++
	return c.snapshotLocked(), nil
}

func (c *workerNodeController) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	stop := context.AfterFunc(c.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

func (c *workerNodeController) Probe(ctx context.Context, id string, revision uint64) (backend.WorkerNodeSetup, error) {
	return c.connect(ctx, id, revision, false)
}

func (c *workerNodeController) Connect(ctx context.Context, id string, revision uint64) (backend.WorkerNodeSetup, error) {
	return c.connect(ctx, id, revision, true)
}

func (c *workerNodeController) connect(parent context.Context, id string, revision uint64, enroll bool) (backend.WorkerNodeSetup, error) {
	return c.connectTarget(parent, api.WorkTarget{NodeID: id}, revision, enroll, false)
}
func (c *workerNodeController) ProbeTarget(ctx context.Context, target api.WorkTarget, revision uint64) (backend.WorkerNodeSetup, error) {
	return c.connectTarget(ctx, target, revision, false, true)
}
func (c *workerNodeController) ConnectTarget(ctx context.Context, target api.WorkTarget, revision uint64) (backend.WorkerNodeSetup, error) {
	return c.connectTarget(ctx, target, revision, true, true)
}

func (c *workerNodeController) selectTargetLocked(target api.WorkTarget, exact bool) (api.WorkTarget, error) {
	if exact {
		if target.Validate() != nil || target.Role != api.RoleWorker || (target.Backend != "caelis" && target.Backend != "codex") {
			return api.WorkTarget{}, errors.New("invalid exact Worker target")
		}
		if _, exists := c.views[target]; !exists {
			return api.WorkTarget{}, errors.New("worker target is not configured")
		}
		return target, nil
	}
	var selected api.WorkTarget
	matches := 0
	for candidate := range c.views {
		if candidate.NodeID == target.NodeID {
			selected = candidate
			matches++
		}
	}
	if matches > 1 {
		return api.WorkTarget{}, errors.New("worker node has multiple backends; select an exact Worker target")
	}
	if matches == 0 {
		return api.WorkTarget{}, errors.New("worker node is not configured")
	}
	return selected, nil
}

func (c *workerNodeController) connectTarget(parent context.Context, target api.WorkTarget, revision uint64, enroll, exact bool) (backend.WorkerNodeSetup, error) {
	c.operation.Lock()
	defer c.operation.Unlock()
	c.mu.Lock()
	if err := c.checkLocked(revision); err != nil {
		snapshot := c.snapshotLocked()
		c.mu.Unlock()
		return snapshot, err
	}
	selected, selectionErr := c.selectTargetLocked(target, exact)
	if selectionErr != nil {
		snapshot := c.snapshotLocked()
		c.mu.Unlock()
		return snapshot, selectionErr
	}
	target = selected
	view := c.views[target]
	var stale workerNodeAdapter
	if c.active[target] != nil {
		readiness, known := c.runtimes[target].(interface{ Ready() bool })
		if !known || readiness.Ready() {
			snapshot := c.snapshotLocked()
			c.mu.Unlock()
			return snapshot, nil
		}
		stale = c.active[target]
		delete(c.active, target)
		delete(c.runtimes, target)
		view.State, view.Issue, view.Connected = string(nodes.Unavailable), "connection_unavailable", false
		c.views[target] = view
		_ = c.register(view.Config, nodes.Unavailable, nil)
	}
	c.mu.Unlock()
	if stale != nil {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = stale.Close(closeCtx)
		closeCancel()
	}
	ctx, cancel := c.operationContext(parent)
	defer cancel()
	var adapter workerNodeAdapter
	var err error
	directory := filepath.Join(filepath.Dir(c.path), "worker-nodes", view.Config.ID)
	if target.Backend == "codex" {
		directory = filepath.Join(filepath.Dir(c.path), "worker-nodes", ".codex", view.Config.ID)
	}
	if enroll {
		if target.Backend == "codex" {
			err = privateWorkerDirectory(filepath.Dir(directory))
		}
		if err == nil {
			err = privateWorkerDirectory(directory)
		}
	}
	if err == nil {
		if c.factory == nil {
			err = errors.New("worker adapter is unavailable")
		} else {
			adapter, err = c.factory(view.Config, directory)
			if err == nil && adapter == nil {
				err = errors.New("worker adapter is unavailable")
			}
		}
	}
	var runtime api.WorkRuntime
	var facts backend.WorkerNodeFacts
	if err == nil {
		if enroll {
			runtime, err = adapter.Connect(ctx)
			if err == nil && runtime == nil {
				err = errors.New("worker adapter returned no execution port")
			}
		} else {
			facts, err = adapter.Probe(ctx)
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil || !enroll {
		if adapter != nil {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = adapter.Close(closeCtx)
			closeCancel()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	view.Issue = ""
	if err != nil {
		view.State, view.Issue = string(nodes.Unavailable), "connection_unavailable"
		_ = c.register(view.Config, nodes.Unavailable, nil)
	} else if enroll {
		state := nodes.Ready
		if readiness, ok := runtime.(interface{ ModelReadiness() (bool, string) }); ok {
			configured, authentication := readiness.ModelReadiness()
			if !configured {
				state, view.Issue = nodes.Candidate, "model_setup_required"
			} else if authentication == "reported_missing" {
				state, view.Issue = nodes.Candidate, "authentication_required"
			}
		}
		var route api.WorkRuntime
		if state == nodes.Ready {
			route = runtime
		}
		if err = c.register(view.Config, state, route); err == nil {
			c.active[target], c.runtimes[target] = adapter, runtime
			view.State, view.Connected = string(state), true
		}
	} else {
		view.Facts = facts
		view.State = string(nodes.Candidate)
		_ = c.register(view.Config, nodes.Candidate, nil)
	}
	c.views[target] = view
	c.revision++
	if err != nil {
		return c.snapshotLocked(), errors.New("worker connection unavailable; check existing SSH access and the prepared native Worker")
	}
	return c.snapshotLocked(), nil
}

func (c *workerNodeController) Disconnect(ctx context.Context, id string, revision uint64) (backend.WorkerNodeSetup, error) {
	return c.disconnectTarget(ctx, api.WorkTarget{NodeID: id}, revision, false)
}
func (c *workerNodeController) DisconnectTarget(ctx context.Context, target api.WorkTarget, revision uint64) (backend.WorkerNodeSetup, error) {
	return c.disconnectTarget(ctx, target, revision, true)
}
func (c *workerNodeController) disconnectTarget(ctx context.Context, target api.WorkTarget, revision uint64, exact bool) (backend.WorkerNodeSetup, error) {
	c.operation.Lock()
	defer c.operation.Unlock()
	c.mu.Lock()
	if err := c.checkLocked(revision); err != nil {
		snapshot := c.snapshotLocked()
		c.mu.Unlock()
		return snapshot, err
	}
	selected, selectionErr := c.selectTargetLocked(target, exact)
	if selectionErr != nil {
		snapshot := c.snapshotLocked()
		c.mu.Unlock()
		return snapshot, selectionErr
	}
	target = selected
	view := c.views[target]
	adapter := c.active[target]
	view.State, view.Issue, view.Connected = string(nodes.Unavailable), "detached", false
	c.views[target] = view
	delete(c.active, target)
	delete(c.runtimes, target)
	_ = c.register(view.Config, nodes.Unavailable, nil)
	c.revision++
	c.mu.Unlock()
	var err error
	if adapter != nil {
		closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = adapter.Close(closeCtx)
		cancel()
	}
	return c.Snapshot(), err
}

func (c *workerNodeController) Close() error {
	c.cancel()
	c.operation.Lock()
	defer c.operation.Unlock()
	c.mu.Lock()
	c.closed = true
	active := c.active
	c.active, c.runtimes = map[api.WorkTarget]workerNodeAdapter{}, map[api.WorkTarget]api.WorkRuntime{}
	for _, config := range c.document.Nodes {
		_ = c.register(config, nodes.Unavailable, nil)
	}
	c.mu.Unlock()
	var err error
	for _, adapter := range active {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = errors.Join(err, adapter.Close(ctx))
		cancel()
	}
	return err
}

var _ backend.WorkerNodeController = (*workerNodeController)(nil)
var _ backend.WorkerNodeTargetController = (*workerNodeController)(nil)

func privateWorkerDirectory(directory string) error {
	for _, candidate := range []string{filepath.Dir(directory), directory} {
		info, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			if err = os.Mkdir(candidate, 0700); err != nil {
				return err
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return errors.New("worker connection directory must be private and cannot be a symlink")
		}
	}
	return nil
}
