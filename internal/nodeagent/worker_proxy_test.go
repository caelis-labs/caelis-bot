//go:build darwin || linux

package nodeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type proxyRuntime struct {
	api.WorkRuntime
	api.WorkWorkspaceProvider
	api.WorkApprovalProvider
	pair                      workerwire.Pair
	stops, closes, admissions atomic.Int32
	watching                  chan struct{}
	watchOnce                 atomic.Bool
	data                      []byte
}

func (r *proxyRuntime) WorkerPair() workerwire.Pair   { return r.pair }
func (r *proxyRuntime) Connect(context.Context) error { return nil }
func (r *proxyRuntime) Close(context.Context) error   { r.closes.Add(1); return nil }
func (r *proxyRuntime) Snapshot() api.Snapshot        { return api.Snapshot{Revision: 1, Connection: "ready"} }
func (r *proxyRuntime) WaitSnapshot(ctx context.Context, _ uint64) (api.Snapshot, error) {
	if r.watchOnce.CompareAndSwap(false, true) {
		close(r.watching)
	}
	<-ctx.Done()
	return api.Snapshot{}, ctx.Err()
}
func (r *proxyRuntime) WorkStates() []api.WorkState         { return nil }
func (r *proxyRuntime) WorkAdmission(context.Context) error { r.admissions.Add(1); return nil }
func (r *proxyRuntime) WorkApprovals() []api.WorkApproval   { return nil }
func (r *proxyRuntime) ReadWork(_ context.Context, id string) (api.Task, error) {
	target := r.pair.Target
	return api.Task{ID: id, Target: &target}, nil
}
func (r *proxyRuntime) ResolveWorkWorkspace(_ context.Context, _, path string) (string, error) {
	return path, nil
}
func (r *proxyRuntime) StopWork(context.Context, string) (api.Task, error) {
	r.stops.Add(1)
	return api.Task{}, nil
}
func (r *proxyRuntime) ReadWorkArtifact(context.Context, string, string) (api.WorkArtifact, error) {
	h := sha256.Sum256(r.data)
	return api.WorkArtifact{ID: "artifact", Size: int64(len(r.data)), SHA256: hex.EncodeToString(h[:]), Bytes: r.data}, nil
}

type proxySource struct{ pair workerwire.Pair }

func (s proxySource) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	return api.WorkDispatchSource{NodeID: s.pair.SourceNode, Backend: s.pair.SourceBackend, BindingID: "thread", OperationID: "turn", Kind: "native_activation"}, nil
}

type proxyFixture struct {
	service *Service
	proxy   *NativeWorkerProxy
	client  *Client
	pair    workerwire.Pair
	runtime *proxyRuntime
	cancel  context.CancelFunc
}

func newProxyFixture(t *testing.T) proxyFixture {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "worker-proxy-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	ctx, cancel := context.WithCancel(t.Context())
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: "node-test", Backend: "codex", Role: api.RoleWorker}, BotID: api.ProfileBotID("synthetic-bot"), SourceNode: "host", SourceBackend: "codex"}
	runtime := &proxyRuntime{pair: pair, watching: make(chan struct{}), data: bytes.Repeat([]byte("artifact"), 128<<10)}
	owner := nodeworker.New(runtime)
	server, err := workerwire.NewServer(owner, pair)
	if err != nil {
		t.Fatal(err)
	}
	workerSocket := filepath.Join(root, "w.sock")
	ready, workerDone := make(chan struct{}), make(chan error, 1)
	go func() { workerDone <- server.ServeUnixReady(ctx, workerSocket, func() { close(ready) }) }()
	select {
	case <-ready:
	case err := <-workerDone:
		cancel()
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("Worker IPC readiness timed out")
	}
	proxy := NewNativeWorkerProxy(ctx, func(_ context.Context, _ workerwire.Pair) (NativeWorkerProxyEndpoint, error) {
		return NativeWorkerProxyEndpoint{Pair: pair, Socket: workerSocket}, nil
	})
	service := agentFixture(t)
	service.options.WorkerProxy = proxy
	agentSocket := filepath.Join(root, "a.sock")
	listener, err := net.Listen("unix", agentSocket)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	agentDone := make(chan error, 1)
	go func() { agentDone <- Serve(ctx, listener, service) }()
	conn, err := net.Dial("unix", agentSocket)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	client, err := NewClient(pair.Target.NodeID, conn)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		cancel()
		for _, done := range []chan error{agentDone, workerDone} {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("IPC observer did not detach")
			}
		}
		_ = owner.Stop(context.Background())
	})
	return proxyFixture{service, proxy, client, pair, runtime, cancel}
}
func awaitProxySessions(t *testing.T, p *NativeWorkerProxy, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		got := len(p.sessions)
		p.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("proxy observer session count did not settle", want)
}

func TestNativeWorkerProxyIPCFullDuplexChunkingAndObserverDetach(t *testing.T) {
	f := newProxyFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stream, err := f.client.OpenWorkerStream(ctx, f.pair)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := workerwire.NewClient(ctx, f.pair, proxySource{f.pair}, stream)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.runtime.watching:
	case <-ctx.Done():
		t.Fatal("long Worker watch did not start")
	}
	// A waiting Worker response and agent read polls must not block another
	// native command, catalog request or a reply larger than one agent chunk.
	for i := 0; i < 3; i++ {
		if task, err := worker.ReadWork(ctx, "owned-task"); err != nil || task.ID != "owned-task" {
			t.Fatal("full duplex read failed", task, err)
		}
		if _, err := f.client.Catalog(ctx); err != nil {
			t.Fatal("Worker watch blocked catalog", err)
		}
	}
	artifact, err := worker.ReadWorkArtifact(ctx, "owned-task", "artifact")
	if err != nil || !bytes.Equal(artifact.Bytes, f.runtime.data) {
		t.Fatal("chunked artifact bytes changed", err)
	}
	largeRequest := string(bytes.Repeat([]byte("workspace"), 32<<10))
	if path, err := worker.ResolveWorkWorkspace(ctx, "owned-task", largeRequest); err != nil || path != largeRequest {
		t.Fatal("chunked request bytes changed", err)
	}
	worker.Close()
	awaitProxySessions(t, f.proxy, 0)
	if f.runtime.stops.Load() != 0 || f.runtime.closes.Load() != 0 {
		t.Fatal("observer detach stopped native Worker owner")
	}
	if _, err := f.client.Catalog(ctx); err != nil {
		t.Fatal("observer detach closed shared agent", err)
	}
	// A new observer reaches the same retained owner after ordinary detach.
	stream, err = f.client.OpenWorkerStream(ctx, f.pair)
	if err != nil {
		t.Fatal(err)
	}
	worker, err = workerwire.NewClient(ctx, f.pair, proxySource{f.pair}, stream)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if task, err := worker.ReadWork(ctx, "owned-task"); err != nil || task.ID != "owned-task" {
		t.Fatal("retained owner reconnect failed", err)
	}
	_ = f.client.Close()
	awaitProxySessions(t, f.proxy, 0)
	if f.runtime.stops.Load() != 0 || f.runtime.closes.Load() != 0 {
		t.Fatal("agent EOF stopped native Worker owner")
	}
}

func TestNativeWorkerProxyExactPairAndClosedEndpoints(t *testing.T) {
	f := newProxyFixture(t)
	for _, name := range []string{"node", "backend", "role", "bot", "source-node", "source-backend"} {
		t.Run(name, func(t *testing.T) {
			pair := f.pair
			switch name {
			case "node":
				pair.Target.NodeID = "other"
			case "backend":
				pair.Target.Backend = "caelis"
			case "role":
				pair.Target.Role = api.RoleBot
			case "bot":
				pair.BotID = "other"
			case "source-node":
				pair.SourceNode = "other"
			case "source-backend":
				pair.SourceBackend = "caelis"
			}
			if stream, err := f.client.OpenWorkerStream(t.Context(), pair); err == nil {
				stream.Close()
				t.Fatal("substituted native pair accepted")
			}
		})
	}
	body, _ := json.Marshal(workerProxyOpen{Pair: f.pair})
	body = append(body[:len(body)-1], []byte(`,"socket":"/arbitrary.sock"}`)...)
	for _, path := range []string{"/v1/node/worker/open", "/v1/node/worker/open?socket=/arbitrary.sock", "/v1/node/worker/shell"} {
		response := httptest.NewRecorder()
		Handler(f.service).ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
		if response.Code == http.StatusOK {
			t.Fatal("payload socket or unknown endpoint became authority", path)
		}
	}
	awaitProxySessions(t, f.proxy, 0)
}

func TestNativeWorkerProxyReadPollBoundAndOrderedChunks(t *testing.T) {
	f := newProxyFixture(t)
	opened, err := f.proxy.open(t.Context(), f.pair)
	if err != nil {
		t.Fatal(err)
	}
	defer f.proxy.close(opened.Handle)
	start := time.Now()
	read, err := f.proxy.read(t.Context(), workerProxyIO{Handle: opened.Handle})
	if err != nil || read.Closed || len(read.Data) != 0 || time.Since(start) > 250*time.Millisecond {
		t.Fatal("idle read poll blocked", read, err, time.Since(start))
	}
	if err = f.proxy.write(workerProxyIO{Handle: opened.Handle, Offset: 1, Data: []byte{1}}); err == nil {
		t.Fatal("out of order stream write accepted")
	}
	f.proxy.close(opened.Handle)
	opened, err = f.proxy.open(t.Context(), f.pair)
	if err != nil {
		t.Fatal(err)
	}
	defer f.proxy.close(opened.Handle)
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], workerwire.MaxRelayFrame+1)
	if err = f.proxy.write(workerProxyIO{Handle: opened.Handle, Data: prefix[:]}); err == nil {
		t.Fatal("oversized frame prefix accepted")
	}
	f.proxy.close(opened.Handle)
	opened, err = f.proxy.open(t.Context(), f.pair)
	if err != nil {
		t.Fatal(err)
	}
	defer f.proxy.close(opened.Handle)
	if err = f.proxy.write(workerProxyIO{Handle: opened.Handle, Data: make([]byte, workerChunk+1)}); err == nil {
		t.Fatal("oversized RPC chunk accepted")
	}
	if _, err = f.proxy.read(t.Context(), workerProxyIO{Handle: opened.Handle, Offset: 1}); err == nil {
		t.Fatal("read offset substitution accepted")
	}
	if f.runtime.stops.Load() != 0 || f.runtime.closes.Load() != 0 {
		t.Fatal("invalid observer bytes affected owner lifetime")
	}
	if _, err = f.client.Catalog(t.Context()); err != nil {
		t.Fatal("invalid observer bytes closed shared agent", err)
	}
}

func TestNativeWorkerProxyOwnerCancellationClosesOnlyObservation(t *testing.T) {
	f := newProxyFixture(t)
	stream, err := f.client.OpenWorkerStream(t.Context(), f.pair)
	if err != nil {
		t.Fatal(err)
	}
	f.cancel()
	awaitProxySessions(t, f.proxy, 0)
	if f.runtime.stops.Load() != 0 || f.runtime.closes.Load() != 0 {
		t.Fatal("proxy owner cancellation stopped Worker runtime")
	}
	_, err = stream.Read(make([]byte, 1))
	if err == nil || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("detached observer stayed readable", err)
	}
	if _, err = f.client.OpenWorkerStream(t.Context(), f.pair); err == nil {
		t.Fatal("stopped native proxy accepted observation")
	}
}

func TestNativeWorkerProxyRejectsForeignRawGrantBeforeForwarding(t *testing.T) {
	f := newProxyFixture(t)
	opened, err := f.proxy.open(t.Context(), f.pair)
	if err != nil {
		t.Fatal(err)
	}
	defer f.proxy.close(opened.Handle)
	source := api.WorkDispatchSource{NodeID: f.pair.SourceNode, Backend: f.pair.SourceBackend, BindingID: "thread", OperationID: "turn", Kind: "native_activation", Lease: api.WorkerLeaseGrant{BotID: "foreign-bot", BrokerNodeID: "broker", SourceNodeID: f.pair.SourceNode, Backend: f.pair.SourceBackend, Epoch: "epoch"}}
	body, err := json.Marshal(struct {
		Version int
		ID      uint64
		Pair    workerwire.Pair
		Method  string
		Current *api.WorkDispatchSource
	}{1, 1, f.pair, "admission", &source})
	if err != nil {
		t.Fatal(err)
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(body)))
	if err := f.proxy.write(workerProxyIO{Handle: opened.Handle, Data: append(prefix[:], body...)}); err == nil {
		t.Fatal("foreign raw grant forwarded")
	}
	if f.runtime.admissions.Load() != 0 {
		t.Fatal("foreign raw grant reached native admission")
	}
	if _, err := f.client.Catalog(t.Context()); err != nil {
		t.Fatal("grant rejection affected shared agent", err)
	}
}
