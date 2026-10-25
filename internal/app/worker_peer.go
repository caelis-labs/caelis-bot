package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

type peerHost struct {
	mu               sync.Mutex
	server           *workerpeer.Server
	http             *http.Server
	address          string
	mounted          bool
	owners           map[string]workerpeer.Owner
	listen           string
	configuration    peerListenerConfig
	configurationErr error
}

// Keep the native owner available even when Connect fails. Ordinary reads must
// retain its original identity; only the explicit native recovery action can
// replace a verified exited private process for new work.
type foreignCodexOwner struct {
	*codex.WorkOwner
	mu            sync.Mutex
	endpointPath  string
	savedEndpoint string
}

func (w *foreignCodexOwner) saveEndpoint() error {
	ep, err := w.NativeEndpoint()
	if err != nil {
		return err
	}
	if ep == w.savedEndpoint {
		return nil
	}
	if err = localstate.Write(w.endpointPath, struct {
		Endpoint string `json:"endpoint"`
	}{ep}); err != nil {
		return err
	}
	w.savedEndpoint = ep
	return nil
}
func (w *foreignCodexOwner) Connect(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.WorkOwner.Connect(ctx); err != nil {
		return err
	}
	return w.saveEndpoint()
}
func (w *foreignCodexOwner) RecoverNewWork(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.WorkOwner.RecoverNewWork(ctx); err != nil {
		return err
	}
	return w.saveEndpoint()
}

func (a *Application) configureWorkerPeer() error {
	p := &peerHost{owners: map[string]workerpeer.Owner{}}
	p.configuration, p.configurationErr = readPeerListener(filepath.Join(a.root, "WorkerPeers", "listener.json"))
	a.peer = p
	a.Backend.ConfigureWorkerInteractions(a.machines)
	server, err := workerpeer.Open(filepath.Join(a.root, "WorkerPeers"), a.openForeignWorker)
	if err != nil {
		p.configurationErr = errors.New("worker_peer_state_unavailable")
		a.Backend.ConfigureWorkerSharing(a)
		return err
	}
	p.server = server
	server.ConfigureReadiness(a.localWork.workerReadiness)
	a.Backend.ConfigureWorkerSharing(a)
	a.Backend.ConfigureWorkerInteractions(a.machines)
	return nil
}
func (a *Application) openForeignWorker(ctx context.Context, source, runtime, root string) (workerpeer.Owner, string, *api.WorkExecutionSettings, error) {
	// The factory reads B's settings; none of these values comes from A's JSON.
	if runtime == "" {
		runtime = a.localWork.DefaultRuntime()
		if runtime == "conversation" {
			runtime = a.currentEngine().(api.Provider).ProviderInfo().ID
		}
	}
	bindingOnly := workerpeer.OriginalBindingOnly(ctx)
	if bindingOnly {
		a.peer.mu.Lock()
		owner := a.peer.owners[source+"\x00"+runtime]
		a.peer.mu.Unlock()
		if owner != nil {
			return owner, runtime, nil, nil
		}
	}
	dir, err := providerDirectory(a.root, runtime)
	if err != nil {
		return nil, "", nil, err
	}
	settings, err := backend.LoadRuntimeSettings(filepath.Join(a.root, "runtime-profiles", runtime+".json"), runtime)
	if err != nil {
		return nil, "", nil, err
	}
	if a.currentEngine().(api.Provider).ProviderInfo().ID == runtime {
		settings = a.Backend.RuntimeSettings()
	}
	work, err := backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
	if err != nil {
		return nil, "", nil, err
	}
	if !bindingOnly && a.localWork.DefaultRuntime() == "conversation" && a.currentEngine().(api.Provider).ProviderInfo().ID == runtime {
		if p, ok := a.currentEngine().(interface {
			ConversationExecution(context.Context) (api.WorkExecutionSettings, error)
		}); ok {
			work, err = p.ConversationExecution(ctx)
			if err != nil {
				return nil, "", nil, err
			}
		}
	}
	a.peer.mu.Lock()
	defer a.peer.mu.Unlock()
	key := source + "\x00" + runtime
	if owner := a.peer.owners[key]; owner != nil {
		return owner, runtime, &work, nil
	}
	private := filepath.Join(root, runtime)
	if err = os.MkdirAll(private, 0700); err != nil {
		return nil, "", nil, err
	}
	var owner workerpeer.Owner
	switch runtime {
	case "codex":
		endpointPath := filepath.Join(private, "native-endpoint.json")
		var endpoint struct {
			Endpoint string `json:"endpoint"`
		}
		data, e := os.ReadFile(endpointPath)
		if e == nil {
			if json.Unmarshal(data, &endpoint) != nil || endpoint.Endpoint == "" {
				return nil, "", nil, errors.New("original_worker_endpoint_invalid")
			}
		} else if !os.IsNotExist(e) {
			return nil, "", nil, e
		} else if b, e := os.ReadFile(filepath.Join(private, "worker-bindings.json")); e == nil {
			var saved struct {
				Tasks map[string]json.RawMessage `json:"tasks"`
			}
			if json.Unmarshal(b, &saved) != nil || len(saved.Tasks) > 0 {
				return nil, "", nil, errors.New("original_worker_endpoint_unavailable")
			}
		}
		native, e := codex.NewWorkOwner(codex.SessionOptions{Diagnostics: a.host.Diagnostics, Binary: settings.CLIPath, Socket: strings.TrimPrefix(endpoint.Endpoint, "unix://"), Directory: private, StateFile: filepath.Join(private, "worker-bindings.json"), WorkRoot: filepath.Join(root, "tasks"), WorkExecution: work})
		if e != nil {
			return nil, "", nil, e
		}
		owner = &foreignCodexOwner{WorkOwner: native, endpointPath: endpointPath, savedEndpoint: endpoint.Endpoint}
	case "caelis":
		owner, err = caelis.NewWorkOwner(caelis.Options{Diagnostics: a.host.Diagnostics, Directory: private, Settings: settings, WorkExecution: work, WorkSourceIdentity: source})
	default:
		return nil, "", nil, errors.New("worker_runtime_unsupported")
	}
	if err != nil {
		return nil, "", nil, err
	}
	a.peer.owners[key] = owner
	return owner, runtime, &work, nil
}

// WorkerPeerHandler mounts in headless's existing listener, before browser
// middleware. It has its own auth; this does not relax browser Host/Origin rules.
func (a *Application) WorkerPeerHandler(next http.Handler, address string) http.Handler {
	a.peer.mu.Lock()
	a.peer.mounted = true
	a.peer.address = address
	a.peer.listen = strings.TrimPrefix(address, "http://")
	a.peer.mu.Unlock()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, workerpeer.Prefix) {
			if a.peer.server == nil {
				http.Error(w, "worker_peer_state_unavailable", http.StatusServiceUnavailable)
				return
			}
			a.peer.server.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *Application) startWorkerPeer() error {
	if a.peer == nil {
		return nil
	}
	if a.peer.server == nil {
		return errors.New("worker_peer_state_unavailable")
	}
	a.peer.mu.Lock()
	defer a.peer.mu.Unlock()
	if a.peer.http != nil {
		return nil
	}
	if a.peer.mounted {
		host, _, _ := net.SplitHostPort(a.peer.listen)
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil
		}
	}

	if a.peer.configurationErr != nil {
		return a.peer.configurationErr
	}
	c := a.peer.configuration
	if !c.Dedicated {
		c = peerListenerConfig{Listen: "0.0.0.0:9317", Dedicated: true}
	}
	err := a.replacePeerListenerLocked(c)
	if err != nil && err.Error() == "worker_listener_in_use" {
		err = a.replacePeerListenerLocked(peerListenerConfig{Listen: "0.0.0.0:0", Dedicated: true})
	}
	return err
}
func (a *Application) WorkerSharing() api.WorkerSharing {
	if a.peer == nil || a.peer.server == nil {
		return api.WorkerSharing{Issue: "worker_peer_state_unavailable", Requests: []api.WorkerPairingRequest{}, Sources: []api.WorkerPeerSource{}}
	}
	v := a.peer.server.Status()
	a.peer.mu.Lock()
	address := a.peer.address
	if a.peer.configuration.Address != "" {
		address = a.peer.configuration.Address
	}
	listen := a.peer.listen
	if listen == "" {
		listen = a.peer.configuration.Listen
	}
	listening, managed := a.peer.http != nil || a.peer.mounted, a.peer.mounted && a.peer.http == nil
	a.peer.mu.Unlock()
	issue := ""
	if !listening {
		issue = "worker_listener_unavailable"
	}
	return api.WorkerSharing{Issue: issue, Address: address, Listen: listen, Listening: listening, Managed: managed, Peer: v.Peer, Sources: v.Sources, Requests: v.Requests}
}
func (a *Application) DecideWorkerPairing(ctx context.Context, in api.WorkerPairingDecision) error {
	if a.peer == nil || a.peer.server == nil {
		return &api.ActionRejectedError{Code: "worker_peer_state_unavailable"}
	}
	err := a.peer.server.DecidePairing(ctx, in)
	if code := api.WorkerReadinessCode(err); err != nil && code != "" {
		return &api.ActionRejectedError{Code: code}
	}
	return err
}
func (a *Application) RevokeWorkerPeer(peer string) error {
	if a.peer == nil || a.peer.server == nil {
		return errors.New("worker_peer_state_unavailable")
	}
	return a.peer.server.Revoke(peer)
}
func (a *Application) closeWorkerPeer() error {
	if a.peer == nil || a.peer.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.peer.mu.Lock()
	server := a.peer.http
	a.peer.mu.Unlock()
	var err error
	if server != nil {
		err = server.Close()
	}
	a.peer.mu.Lock()
	owners := a.peer.owners
	a.peer.owners = map[string]workerpeer.Owner{}
	a.peer.mu.Unlock()
	for _, owner := range owners {
		if d, ok := owner.(interface{ DetachForUpdate(context.Context) error }); ok {
			err = errors.Join(err, d.DetachForUpdate(ctx))
		} else {
			err = errors.Join(err, owner.Close(ctx))
		}
	}
	return err
}
