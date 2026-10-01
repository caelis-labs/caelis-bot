package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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
	views     map[string]backend.WorkerNodeView
	active    map[string]workerNodeAdapter
	runtimes  map[string]api.WorkRuntime
	revision  uint64
	issue     string
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
}

func openWorkerNodes(filename string, registry *nodes.Registry, factory workerNodeFactory) *workerNodeController {
	ctx, cancel := context.WithCancel(context.Background())
	c := &workerNodeController{path: filename, registry: registry, factory: factory, document: workerNodeDocument{Version: 1}, views: map[string]backend.WorkerNodeView{}, active: map[string]workerNodeAdapter{}, runtimes: map[string]api.WorkRuntime{}, revision: 1, ctx: ctx, cancel: cancel}
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return c
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 128*1024 {
		c.issue = "config_unreadable"
		return c
	}
	b, err := os.ReadFile(filename)
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	var document workerNodeDocument
	if err != nil || decoder.Decode(&document) != nil || document.Version != 1 || len(document.Nodes) > 16 {
		c.issue = "config_unreadable"
		return c
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		c.issue = "config_unreadable"
		return c
	}
	seen := map[string]bool{}
	for _, config := range document.Nodes {
		if validateWorkerNode(config) != nil || seen[config.ID] {
			c.issue = "config_unreadable"
			return c
		}
		seen[config.ID] = true
	}
	c.document = document
	for _, config := range document.Nodes {
		c.views[config.ID] = backend.WorkerNodeView{Config: config, State: string(nodes.Candidate)}
		_ = c.register(config, nodes.Candidate, nil)
	}
	return c
}

var workerNodeID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var workerSSH = regexp.MustCompile(`^[A-Za-z0-9_.@:\[\]-]+$`)

func validateWorkerNode(config backend.WorkerNodeConfig) error {
	if config.Backend != "" && config.Backend != "caelis" {
		return errors.New("worker backend is unavailable")
	}
	if !workerNodeID.MatchString(config.ID) || config.ID == api.LocalNodeID || strings.TrimSpace(config.Label) == "" || len(config.Label) > 128 || strings.ContainsAny(config.Label, "\x00\r\n") {
		return errors.New("worker node identity is invalid")
	}
	if !workerSSH.MatchString(config.SSH) || strings.HasPrefix(config.SSH, "-") || len(config.SSH) > 256 {
		return errors.New("worker SSH destination is invalid")
	}
	for _, value := range []string{config.Store, config.WorkspaceRoot} {
		if value != "" && (!path.IsAbs(value) || path.Clean(value) != value || strings.ContainsAny(value, "\x00\r\n") || len(value) > 4096) {
			return errors.New("worker paths must be clean absolute target paths")
		}
	}
	if config.WorkspaceRoot == "" {
		return errors.New("worker workspace root is required")
	}
	if config.Helper != "" && (len(config.Helper) > 4096 || strings.ContainsAny(config.Helper, " \t\r\n\x00") || strings.HasPrefix(config.Helper, "-") || strings.Contains(config.Helper, "..")) {
		return errors.New("worker helper executable is invalid")
	}
	return nil
}

func workerTarget(id string) api.WorkTarget {
	return api.WorkTarget{NodeID: id, Backend: "caelis", Role: api.RoleWorker}
}

func (c *workerNodeController) register(config backend.WorkerNodeConfig, state nodes.Availability, runtime api.WorkRuntime) error {
	return c.registry.Set(nodes.Node{ID: config.ID, Label: config.Label}, nodes.Capability{Target: workerTarget(config.ID), State: state}, runtime)
}

func (c *workerNodeController) Snapshot() backend.WorkerNodeSetup {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

func (c *workerNodeController) snapshotLocked() backend.WorkerNodeSetup {
	snapshot := backend.WorkerNodeSetup{Revision: c.revision, Issue: c.issue, Nodes: []backend.WorkerNodeView{}}
	for _, config := range c.document.Nodes {
		snapshot.Nodes = append(snapshot.Nodes, c.views[config.ID])
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
	index := slices.IndexFunc(c.document.Nodes, func(value backend.WorkerNodeConfig) bool { return value.ID == config.ID })
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
	if err := localstate.Write(c.path, document); err != nil {
		return c.snapshotLocked(), errors.New("worker node configuration could not be saved")
	}
	c.document = document
	view, exists := c.views[config.ID]
	view.Config = config
	if !exists {
		view.State = string(nodes.Candidate)
	}
	c.views[config.ID] = view
	c.revision++
	var runtime api.WorkRuntime
	if view.State == string(nodes.Ready) {
		runtime = c.runtimes[config.ID]
	}
	_ = c.register(config, nodes.Availability(view.State), runtime)
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
	c.operation.Lock()
	defer c.operation.Unlock()
	c.mu.Lock()
	if err := c.checkLocked(revision); err != nil {
		snapshot := c.snapshotLocked()
		c.mu.Unlock()
		return snapshot, err
	}
	view, exists := c.views[id]
	if !exists {
		c.mu.Unlock()
		return c.Snapshot(), errors.New("worker node is not configured")
	}
	if c.active[id] != nil {
		snapshot := c.snapshotLocked()
		c.mu.Unlock()
		return snapshot, nil
	}
	c.mu.Unlock()
	ctx, cancel := c.operationContext(parent)
	defer cancel()
	var adapter workerNodeAdapter
	var err error
	directory := filepath.Join(filepath.Dir(c.path), "worker-nodes", id)
	if enroll {
		err = privateWorkerDirectory(directory)
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
			c.active[id], c.runtimes[id] = adapter, runtime
			view.State, view.Connected = string(state), true
		}
	} else {
		view.Facts = facts
		view.State = string(nodes.Candidate)
		_ = c.register(view.Config, nodes.Candidate, nil)
	}
	c.views[id] = view
	c.revision++
	if err != nil {
		return c.snapshotLocked(), errors.New("worker connection unavailable; check the existing SSH access and prepared Caelis Host")
	}
	return c.snapshotLocked(), nil
}

func (c *workerNodeController) Disconnect(ctx context.Context, id string, revision uint64) (backend.WorkerNodeSetup, error) {
	c.operation.Lock()
	defer c.operation.Unlock()
	c.mu.Lock()
	if err := c.checkLocked(revision); err != nil {
		snapshot := c.snapshotLocked()
		c.mu.Unlock()
		return snapshot, err
	}
	view, exists := c.views[id]
	adapter := c.active[id]
	if !exists {
		c.mu.Unlock()
		return c.Snapshot(), errors.New("worker node is not configured")
	}
	view.State, view.Issue, view.Connected = string(nodes.Unavailable), "detached", false
	c.views[id] = view
	delete(c.active, id)
	delete(c.runtimes, id)
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
	c.active, c.runtimes = map[string]workerNodeAdapter{}, map[string]api.WorkRuntime{}
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
