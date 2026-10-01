package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func thinPairing() backend.ProductPairing {
	return backend.ProductPairing{Mode: "remote", Label: "Fixture Bot", SSH: "fixture-target", Endpoint: "http://127.0.0.1:12345", AuthFile: "/fixture/private/product-auth", NodeID: "node-fixture", BotID: "bot-fixture"}
}

type thinClientFixture struct {
	mu       sync.Mutex
	state    productrpc.State
	connect  func(context.Context) error
	command  func(context.Context, productrpc.Command) (productrpc.Result, error)
	commands []productrpc.Command
	lookups  []string
	result   productrpc.Result
	closed   atomic.Int32
}

func newThinClientFixture() *thinClientFixture {
	scope := productrpc.Scope{BotID: "bot-fixture", Generation: "generation-one"}
	return &thinClientFixture{state: productrpc.State{Scope: scope, Cursor: productrpc.Cursor{Generation: scope.Generation, Revision: "1"}, Snapshot: api.Snapshot{Connection: "ready", CanSend: true}, Initialization: api.BotInitialization{Status: "accepted"}, Draft: api.Draft{Revision: 1}}}
}
func (c *thinClientFixture) Connect(ctx context.Context) (productrpc.Identity, error) {
	if c.connect != nil {
		if err := c.connect(ctx); err != nil {
			return productrpc.Identity{}, err
		}
	}
	return productrpc.Identity{Version: 1, NodeID: "node-fixture", Scope: c.state.Scope, Capabilities: productrpc.Capabilities{Interrupt: true}}, nil
}
func (c *thinClientFixture) State(context.Context) (productrpc.State, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, nil
}
func (c *thinClientFixture) Watch(ctx context.Context, _ productrpc.Cursor) (productrpc.State, error) {
	<-ctx.Done()
	return productrpc.State{}, ctx.Err()
}
func (c *thinClientFixture) Command(ctx context.Context, command productrpc.Command) (productrpc.Result, error) {
	c.mu.Lock()
	c.commands = append(c.commands, command)
	c.mu.Unlock()
	if c.command != nil {
		return c.command(ctx, command)
	}
	return productrpc.Result{ID: command.ID, Outcome: "accepted"}, nil
}
func (c *thinClientFixture) Receipt(_ context.Context, id string) (productrpc.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lookups = append(c.lookups, id)
	result := c.result
	result.ID = id
	return result, nil
}
func (c *thinClientFixture) Upload(context.Context, string, []byte) (productrpc.Resource, error) {
	return productrpc.Resource{}, errors.New("not configured")
}
func (c *thinClientFixture) Download(context.Context, string) (productrpc.Resource, []byte, error) {
	return productrpc.Resource{}, nil, errors.New("not configured")
}
func (c *thinClientFixture) Close() { c.closed.Add(1) }

type thinCloserFixture struct{ closed atomic.Int32 }

func (c *thinCloserFixture) Close() error { c.closed.Add(1); return nil }

func TestThinProductSelectionNeverOpensLocalRuntimeOrPersonalData(t *testing.T) {
	root := t.TempDir()
	if err := localstate.Write(filepath.Join(root, "product-connection.json"), productPairingDocument{Version: 1, Pairing: thinPairing()}); err != nil {
		t.Fatal(err)
	}
	// Existing invalid local stores would fail the local constructor. They are
	// preserved untouched and never interpreted in explicit remote mode.
	for _, name := range []string{"runtime.json", "bot.json", "bot-initialization.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("invalid-local-fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	resolved := 0
	a, err := newApplication(root, Host{}, func(string) (providerFactory, error) {
		resolved++
		return providerFactory{}, errors.New("local resolver must not run")
	})
	if err != nil || resolved != 0 || a.product == nil || a.personal != nil || a.initialization != nil || a.tasks != nil || a.companion != nil || a.workerNodes != nil {
		t.Fatal("remote APP constructed a local Bot/runtime", err, resolved)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.PreparePersonal(); err != nil || !a.HasRuntimeChoice() || a.NeedsSetup() {
		t.Fatal("remote mode entered local setup", err)
	}
	for _, name := range []string{"personal", "Notebook", "tasks.json", "Codex", "Caelis"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("remote APP opened local resident data", name)
		}
	}
}

func TestThinProductQuitCancelsBlockedCommandAndPreservesOriginalReceipt(t *testing.T) {
	c := newThinClientFixture()
	started := make(chan struct{})
	c.command = func(ctx context.Context, command productrpc.Command) (productrpc.Result, error) {
		close(started)
		<-ctx.Done()
		return productrpc.Result{ID: command.ID, Outcome: "unknown"}, ctx.Err()
	}
	transport := &thinCloserFixture{}
	e, err := newProductEngine(t.TempDir(), thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) { return c, transport, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := e.Submit(t.Context(), api.Submission{ID: "original-request", Text: "fixture-only"}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("command not admitted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	closed := make(chan error, 1)
	go func() { closed <- e.Close(ctx) }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Quit waited for native mutation instead of detaching")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled command reported success")
		}
	case <-ctx.Done():
		t.Fatal("command cancellation did not resolve")
	}
	if transport.closed.Load() != 1 || c.closed.Load() != 1 {
		t.Fatal("owned client transport not detached exactly once")
	}
	for _, command := range c.commands {
		if command.Kind == "stop-bot" {
			t.Fatal("Quit stopped target resident Bot")
		}
	}
	b, err := os.ReadFile(filepath.Join(e.root, "product-client-receipts.json"))
	if err != nil || strings.Contains(string(b), "fixture-only") || !strings.Contains(string(b), "original-request") {
		t.Fatal("original unknown receipt metadata not preserved privately", err)
	}
}

func TestThinProductDisconnectInvalidatesDelayedHandshake(t *testing.T) {
	c := newThinClientFixture()
	entered, release := make(chan struct{}), make(chan struct{})
	c.connect = func(context.Context) error { close(entered); <-release; return nil }
	transport := &thinCloserFixture{}
	e, err := newProductEngine(t.TempDir(), thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) { return c, transport, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close(t.Context()) })
	done := make(chan error, 1)
	go func() { done <- e.Connect(t.Context()) }()
	<-entered
	if err := e.detach(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("cancelled Connect adopted after explicit disconnect")
	}
	state, issue := e.connectionState()
	if state != "offline" || issue != "detached" || transport.closed.Load() != 1 || c.closed.Load() != 1 {
		t.Fatal("delayed Connect revived disconnected native owner", state, issue)
	}
}

func TestThinProductReconnectQueriesOriginalReceiptWithoutReplay(t *testing.T) {
	first, next := newThinClientFixture(), newThinClientFixture()
	first.command = func(_ context.Context, command productrpc.Command) (productrpc.Result, error) {
		return productrpc.Result{ID: command.ID, Outcome: "unknown"}, errors.New("synthetic lost response")
	}
	next.result = productrpc.Result{Outcome: "accepted"}
	count := 0
	e, err := newProductEngine(t.TempDir(), thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		count++
		if count == 1 {
			return first, &thinCloserFixture{}, nil
		}
		return next, &thinCloserFixture{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close(t.Context()) })
	if err := e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if receipt, err := e.Submit(t.Context(), api.Submission{ID: "stable-send", Text: "fixture message"}, nil); err == nil || receipt.Outcome != "unknown" {
		t.Fatal("response loss lost original uncertainty", receipt, err)
	}
	if err := e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(next.lookups, "stable-send") || len(next.commands) != 0 || e.Snapshot().LastReceipt.ID != "stable-send" || e.Snapshot().LastReceipt.Outcome != "accepted" {
		t.Fatal("reconnect replayed rather than checking original receipt")
	}
}

func TestThinProductPairingCASAndStrictSSHDoNotTransferCredentials(t *testing.T) {
	c := newProductPairingController(t.TempDir(), backend.ProductPairing{Mode: "local"}, nil)
	pairing := thinPairing()
	saved, err := c.SavePairing(pairing, 1)
	if err != nil || !saved.RestartRequired || saved.ActiveMode != "local" {
		t.Fatal("saving pairing connected or switched without restart", err)
	}
	if _, err := c.SavePairing(pairing, 1); err == nil {
		t.Fatal("stale native pairing revision accepted")
	}
	args, err := productSSHArgs(pairing)
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range []string{"BatchMode=yes", "StrictHostKeyChecking=yes", "UpdateHostKeys=no", "ForwardAgent=no", "ForwardX11=no", "ForwardX11Trusted=no", "PermitLocalCommand=no", "ClearAllForwardings=yes", "ForkAfterAuthentication=no", "ControlMaster=no", "ControlPath=none", "ConnectTimeout=10"} {
		if !slices.Contains(args, option) {
			t.Fatal("missing native SSH authority fence", option)
		}
	}
	for _, invalid := range []backend.ProductPairing{func() backend.ProductPairing { p := pairing; p.Endpoint = "https://external.invalid"; return p }(), func() backend.ProductPairing { p := pairing; p.SSH = "-oForwardAgent=yes"; return p }(), func() backend.ProductPairing { p := pairing; p.AuthFile = "relative"; return p }()} {
		if _, err := productSSHArgs(invalid); err == nil {
			t.Fatal("invalid pairing reached SSH")
		}
	}
	b, _ := json.Marshal(saved)
	if strings.Contains(string(b), "token") || strings.Contains(string(b), "credential") {
		t.Fatal("renderer settings include product credentials")
	}
}

type thinProductPort struct {
	productrpc.Port
	mu       sync.Mutex
	snapshot api.Snapshot
	decision api.Decision
	stops    int
	server   *productrpc.Server
}

func (p *thinProductPort) Snapshot() api.Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshot
}
func (p *thinProductPort) BotInitialization() api.BotInitialization {
	return api.BotInitialization{Status: "accepted"}
}
func (p *thinProductPort) Draft() api.Draft { return api.Draft{Revision: 1} }
func (p *thinProductPort) Decide(_ context.Context, d api.Decision) error {
	p.mu.Lock()
	p.decision = d
	p.snapshot.Approvals = nil
	p.mu.Unlock()
	p.server.NotifySnapshot()
	return nil
}
func (p *thinProductPort) StopBot(context.Context) error {
	p.mu.Lock()
	p.stops++
	p.mu.Unlock()
	return nil
}

func TestThinProductActualFramedProjectionApprovesExactTargetAndDetachesOnly(t *testing.T) {
	port := &thinProductPort{snapshot: api.Snapshot{Connection: "ready", CanSend: true, Approvals: []api.Approval{{ID: "native-approval", Target: "native-exact-target", Status: "pending", Choices: []api.Choice{{ID: "once", Label: "Allow once"}}}}}}
	token := strings.Repeat("synthetic-target-only-", 3)
	server, err := productrpc.NewServer(port, productrpc.Options{NodeID: "node-fixture", BotID: "bot-fixture", Token: token, JournalFile: filepath.Join(t.TempDir(), "journal.json")})
	if err != nil {
		t.Fatal(err)
	}
	port.server = server
	h := httptest.NewServer(server)
	t.Cleanup(h.Close)
	e, err := newProductEngine(t.TempDir(), thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		local, remote := net.Pipe()
		t.Cleanup(func() { _ = remote.Close() })
		go func() { _ = productrpc.ProxyStdio(t.Context(), remote, remote, h.URL, token) }()
		client, err := productrpc.NewStdioClient(productrpc.StdioOptions{ExpectedNode: "node-fixture", ExpectedBot: "bot-fixture"}, local)
		return client, local, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot := e.Snapshot()
	if len(snapshot.Approvals) != 1 || snapshot.Approvals[0].ID == "native-approval" {
		t.Fatal("native approval ID leaked into remote APP")
	}
	if err := e.Decide(t.Context(), api.Decision{ID: snapshot.Approvals[0].ID, Choice: "once"}); err != nil {
		t.Fatal(err)
	}
	port.mu.Lock()
	decision := port.decision
	port.mu.Unlock()
	if decision.ID != "native-approval" || decision.Choice != "once" {
		t.Fatal("framed product approval lost exact native target", decision)
	}
	if err := e.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.stops != 0 {
		t.Fatal("thin APP Quit stopped resident Bot")
	}
}

func TestThinProductUnknownOriginalReceiptFencesNewSendAfterReadyIdleReconnect(t *testing.T) {
	first, next := newThinClientFixture(), newThinClientFixture()
	first.command = func(_ context.Context, c productrpc.Command) (productrpc.Result, error) {
		return productrpc.Result{ID: c.ID, Outcome: "unknown"}, errors.New("lost response")
	}
	next.result = productrpc.Result{Outcome: "unknown"}
	next.state.Snapshot.CanSteer = true
	count := 0
	e, err := newProductEngine(t.TempDir(), thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		count++
		if count == 1 {
			return first, &thinCloserFixture{}, nil
		}
		return next, &thinCloserFixture{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close(t.Context()) })
	if err := e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, _ = e.Submit(t.Context(), api.Submission{ID: "original", Text: "retained draft"}, nil)
	if err := e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot := e.Snapshot()
	if snapshot.CanSend || snapshot.CanSteer || snapshot.Phase != "unknown" {
		t.Fatal("ready-idle remote state erased original receipt uncertainty", snapshot)
	}
	if receipt, err := e.Submit(t.Context(), api.Submission{ID: "replacement", Text: "retained draft"}, nil); err == nil || receipt.Outcome != "rejected" || len(next.commands) != 0 {
		t.Fatal("unknown original was silently replaced", receipt, err)
	}
}
