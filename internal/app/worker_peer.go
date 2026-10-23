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
	mu      sync.Mutex
	server  *workerpeer.Server
	http    *http.Server
	address string
	mounted bool
	owners  map[string]workerpeer.Owner
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
	a.peer = p
	server, err := workerpeer.Open(filepath.Join(a.root, "WorkerPeers"), a.openForeignWorker)
	if err != nil {
		return err
	}
	p.server = server
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
	if a.localWork.DefaultRuntime() == "conversation" && a.currentEngine().(api.Provider).ProviderInfo().ID == runtime {
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
	a.peer.mu.Unlock()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, workerpeer.Prefix) {
			a.peer.server.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *Application) startWorkerPeer() error {
	if a.peer == nil || !a.peer.server.Status().Enabled {
		return nil
	}
	a.peer.mu.Lock()
	defer a.peer.mu.Unlock()
	if a.peer.mounted || a.peer.http != nil {
		return nil
	}
	// Desktop has no existing web listener. Keep one App-owned loopback listener;
	// retain its successful port across restarts. No global browser port policy.
	listen := "127.0.0.1:0"
	var saved struct {
		Listen string `json:"listen"`
	}
	path := filepath.Join(a.root, "WorkerPeers", "listener.json")
	if b, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(b, &saved) != nil || saved.Listen == "" {
			return errors.New("worker_listener_invalid")
		}
		listen = saved.Listen
	} else if !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	if err = localstate.Write(path, struct {
		Listen string `json:"listen"`
	}{ln.Addr().String()}); err != nil {
		ln.Close()
		return err
	}
	a.peer.address = "http://" + ln.Addr().String()
	a.peer.http = &http.Server{Handler: a.peer.server, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		err := a.peer.http.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && a.host.ReportError != nil {
			a.host.ReportError(err)
		}
	}()
	return nil
}
func (a *Application) WorkerSharing() api.WorkerSharing {
	if a.peer == nil {
		return api.WorkerSharing{}
	}
	v := a.peer.server.Status()
	a.peer.mu.Lock()
	address := a.peer.address
	a.peer.mu.Unlock()
	return api.WorkerSharing{Enabled: v.Enabled, Address: address, Peer: v.Peer, Sources: v.Sources}
}
func (a *Application) SetWorkerSharing(enabled bool) (api.WorkerSharing, error) {
	if err := a.peer.server.Enable(enabled); err != nil {
		return a.WorkerSharing(), err
	}
	if enabled {
		if err := a.startWorkerPeer(); err != nil {
			return a.WorkerSharing(), err
		}
	}
	return a.WorkerSharing(), nil
}
func (a *Application) CreateWorkerPairing(ctx context.Context) (api.WorkerSharing, error) {
	v, err := a.peer.server.Pairing(ctx)
	out := a.WorkerSharing()
	out.Code, out.ExpiresAt = v.Code, v.ExpiresAt
	return out, err
}
func (a *Application) RevokeWorkerPeer(peer string) error { return a.peer.server.Revoke(peer) }
func (a *Application) RecoverWorkerSharing(ctx context.Context) (api.WorkerSharing, error) {
	err := a.peer.server.RecoverReadiness(ctx)
	return a.WorkerSharing(), err
}
func (a *Application) closeWorkerPeer() error {
	if a.peer == nil {
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
