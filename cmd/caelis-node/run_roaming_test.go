//go:build darwin || linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/memorytransfer"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodebroker"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// The real owned child speaks only standard discovery and an empty new thread.
// Any model turn, old thread resume, or work replay terminates this fixture.
func TestRoamingNativeHelper(t *testing.T) {
	sep := -1
	for i, a := range os.Args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return
	}
	args := os.Args[sep+1:]
	if len(args) < 2 {
		os.Exit(2)
	}
	root := args[0]
	if len(args) == 2 && args[1] == "--version" {
		_, _ = io.WriteString(os.Stdout, "codex-cli 0.153.4\n")
		os.Exit(0)
	}
	if strings.Join(args[1:], " ") != "app-server --listen stdio://" {
		os.Exit(3)
	}
	pid := strconv.Itoa(os.Getpid())
	if os.WriteFile(filepath.Join(root, "pid-"+pid), []byte(pid), 0600) != nil {
		os.Exit(4)
	}
	methods, err := os.OpenFile(filepath.Join(root, "methods"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(5)
	}
	defer methods.Close()
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if decoder.Decode(&request) != nil {
			os.Exit(0)
		}
		_, _ = methods.WriteString(request.Method + "\n")
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]string{"userAgent": "roaming-native-fixture"}
		case "initialized":
			continue
		case "account/read":
			result = map[string]any{"account": map[string]string{"type": "chatgpt"}, "requiresOpenaiAuth": true}
		case "model/list":
			result = map[string]any{"data": []any{map[string]any{"model": "fixture", "displayName": "Fixture", "isDefault": true}}}
		case "skills/list":
			result = map[string]any{"data": []any{}}
		case "thread/start":
			result = map[string]any{"thread": map[string]any{"id": "fresh-native-thread", "turns": []any{}}, "model": "fixture"}
		default:
			os.Exit(6)
		}
		if encoder.Encode(struct {
			ID     json.RawMessage `json:"id"`
			Result any             `json:"result"`
		}{request.ID, result}) != nil {
			os.Exit(7)
		}
	}
}

type roamingMetadata struct {
	NodeID, Socket, State string
	Endpoint              string
	Identity              productrpc.Identity
}
type roamingMetadataWriter struct{ ready chan roamingMetadata }

func (w roamingMetadataWriter) Write(b []byte) (int, error) {
	var m roamingMetadata
	if json.Unmarshal(b, &m) != nil {
		return 0, errors.New("invalid managed metadata")
	}
	w.ready <- m
	return len(b), nil
}

func TestManagedForegroundUsesRealNativeProofAndFreshProductJournal(t *testing.T) {
	testManagedForeground(t, false, false)
}
func TestManagedForegroundClosedDisablePublishesAndStopsBeforeRestore(t *testing.T) {
	testManagedForeground(t, true, false)
}
func TestManagedForegroundClosedNativeStartUsesOriginalReceipt(t *testing.T) {
	testManagedForeground(t, true, true)
}
func testManagedForeground(t *testing.T, disable, startManaged bool) {
	root := canonicalWorkerTestRoot(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(root, "codex")
	if err = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestRoamingNativeHelper$' -- "+quote(root)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(root, "agent")
	if err = os.Mkdir(agentDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Existing enrollment is prepared by the same native agent, not claim fields.
	if _, err = nodeagent.New(nodeagent.Options{Directory: agentDir, NodeID: "native-node"}); err != nil {
		t.Fatal(err)
	}
	botID := "roaming-fixture"
	payload, ref, err := memorytransfer.EncodeNotebook(t.Context(), memorytransfer.NotebookSnapshot{BotID: botID, Epoch: "0", Version: "1", Files: []memorytransfer.NotebookFile{
		{File: memorytransfer.File{Path: "bot.json"}, Body: []byte(`{"version":1,"personalVersion":1,"id":"roaming-fixture","schedules":[]}`)},
		{File: memorytransfer.File{Path: "notebook-migration.json"}, Body: []byte(`{"version":1}`)},
		{File: memorytransfer.File{Path: "bot-initialization.json"}, Body: []byte(`{"version":1,"id":"accepted-intro","status":"accepted"}`)},
		{File: memorytransfer.File{Path: "Notebook/MEMORY.md"}, Body: []byte("# Memory\nCurrent notebook fixture.\n")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	target := api.WorkTarget{NodeID: "native-node", Backend: "codex", Role: api.RoleBot}
	registry := nodecoord.NewPeerRegistry()
	if err = registry.Register(target, pairedRuntimePeer{brokerPeer{NodeID: target.NodeID, Backend: api.NodeCodex, Socket: filepath.Join(agentDir, "agent.sock")}}); err != nil {
		t.Fatal(err)
	}
	broker, err := nodecoord.Open(nodecoord.Options{Directory: filepath.Join(root, "cache"), BotID: botID, BrokerNodeID: "native-broker", Verify: registry.VerifyClaim, VerifyRenew: registry.VerifyRenew, ReadOwnerEligibility: registry.ReadOwnerEligibility, ValidateSnapshot: memorytransfer.ValidateNotebookPayload})
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	if err = broker.SeedSnapshot(t.Context(), ref, payload); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	brokerReady := make(chan struct{})
	brokerDone := make(chan error, 1)
	socket := filepath.Join(root, "broker.sock")
	go func() { brokerDone <- nodebroker.ServeUnix(ctx, socket, broker, func() { close(brokerReady) }) }()
	select {
	case <-brokerReady:
	case err := <-brokerDone:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	token := strings.Repeat("f", 64)
	auth := filepath.Join(root, "auth-fixture")
	if err = os.WriteFile(auth, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	c := roamingCommand{NodeID: target.NodeID, BotID: botID, Backend: "codex", AgentDirectory: agentDir, GenerationRoot: filepath.Join(root, "generations"), BrokerNodeID: "native-broker", BrokerSocket: socket, AuthFile: auth, CodexBinary: binary, Listen: "127.0.0.1:0"}
	stopObserved := make(chan error, 1)
	c.reportStop = func(e error) { stopObserved <- e }
	metadata := make(chan roamingMetadata, 4)
	done := make(chan error, 1)
	// Only the platform power notification source is substituted. Managed APP,
	// native Guard, paired proof RPC, broker CAS and owned process remain real.

	var active roamingMetadata
	if startManaged {
		host := filepath.Join(root, "caelis-node")
		hostBytes := []byte("#!/bin/sh\nexec " + quote(executable) + " -test.run='^TestRoamingForegroundHelper$' -- \"$@\"\n")
		if err = os.WriteFile(host, hostBytes, 0700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(hostBytes)
		starter, e := nodeagent.NewManagedStarter(ctx, nodeagent.ManagedStartConfig{Version: 1, NodeID: target.NodeID, Directory: agentDir, Helper: host, HelperSHA256: hex.EncodeToString(sum[:]), CodexBinary: binary, Bindings: []nodeagent.ManagedBrokerBinding{{BotID: botID, BrokerNodeID: "native-broker", Socket: socket, AuthFile: auth}}})
		if e != nil {
			t.Fatal(e)
		}
		defer starter.Close()
		service, e := nodeagent.New(nodeagent.Options{Directory: agentDir, NodeID: target.NodeID, ManagedStart: starter, RuntimeOwner: starter, ManagedProduct: starter})
		if e != nil {
			t.Fatal(e)
		}
		listener, e := net.Listen("unix", filepath.Join(agentDir, "agent.sock"))
		if e != nil {
			t.Fatal(e)
		}
		defer listener.Close()
		if e = os.Chmod(filepath.Join(agentDir, "agent.sock"), 0600); e != nil {
			t.Fatal(e)
		}
		go func() { done <- nodeagent.Serve(ctx, listener, service) }()
		mainClient, e := nodeagent.Dial(ctx, filepath.Join(agentDir, "agent.sock"), target.NodeID)
		if e != nil {
			t.Fatal(e)
		}
		defer mainClient.Close()
		status, e := mainClient.ManagedRoamingStatus(ctx, target.NodeID)
		if e != nil || len(status.Bindings) != 1 {
			t.Fatalf("native managed preflight %+v %v", status, e)
		}
		request := nodeagent.ManagedStartRequest{NodeID: target.NodeID, OperationID: "original-start", BotID: botID, BrokerNodeID: "native-broker"}
		receipt, e := mainClient.StartManagedRoaming(ctx, request)
		if e != nil || receipt.Outcome != "accepted" {
			t.Fatalf("native start %+v %v", receipt, e)
		}
		duplicate, e := mainClient.StartManagedRoaming(ctx, request)
		if e != nil || duplicate != receipt {
			t.Fatalf("original start receipt %+v %v", duplicate, e)
		}
		request.BotID = "foreign"
		if _, e = mainClient.StartManagedRoaming(ctx, request); e == nil {
			t.Fatal("changed original start request admitted")
		}
		locator, e := mainClient.ReadManagedProduct(ctx, target)
		if e != nil {
			t.Fatal(e)
		}
		active.Endpoint, active.Identity = locator.Endpoint, locator.Identity
	} else {
		go func() {
			done <- runRoamingCommand(ctx, c, roamingMetadataWriter{metadata}, func(context.Context, func(), func()) (func(), error) { return func() {}, nil })
		}()
		for active.Endpoint == "" {
			select {
			case active = <-metadata:
			case err := <-done:
				t.Fatalf("managed startup: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}

	}
	client, err := productrpc.NewClient(productrpc.ClientOptions{URL: active.Endpoint, ExpectedNode: target.NodeID, ExpectedBot: productrpc.ProfileBotID(botID), Token: token})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	identity, err := client.Connect(ctx)
	if err != nil || identity != active.Identity {
		t.Fatalf("product pairing %+v %v", identity, err)
	}
	state, err := client.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for state.Snapshot.Connection != "ready" {
		watchCtx, watchCancel := context.WithTimeout(ctx, 3*time.Second)
		previous := state
		state, err = client.Watch(watchCtx, state.Cursor)
		watchCancel()
		if err != nil {
			methods, _ := os.ReadFile(filepath.Join(root, "methods"))
			t.Fatalf("watch failure %v connection=%s phase=%s message=%s methods=%s", err, previous.Snapshot.Connection, previous.Snapshot.Phase, previous.Snapshot.Message, methods)
		}
	}
	if len(state.Snapshot.Items) != 0 || len(state.TaskSummaries) != 0 || state.Initialization.Status != "accepted" {
		t.Fatalf("old execution state adopted: %+v", state)
	}
	lease, err := broker.CurrentLease(ctx, botID)
	if err != nil || lease.NodeID != target.NodeID {
		t.Fatalf("real owner authority %+v %v", lease, err)
	}
	// A fresh generation cannot turn an absent previous command into execution.
	receipt, err := client.Receipt(ctx, "original-source-operation")
	if err != nil || receipt.Outcome != "unknown" {
		t.Fatalf("old receipt replay %+v %v", receipt, err)
	}
	agentClient, e := nodeagent.Dial(ctx, filepath.Join(agentDir, "agent.sock"), target.NodeID)
	if e != nil {
		t.Fatal(e)
	}
	defer agentClient.Close()
	locator, e := agentClient.ReadManagedProduct(ctx, target)
	if e != nil || locator.Identity != identity || locator.Endpoint != active.Endpoint || locator.BotID != botID || locator.AuthFile != auth || locator.Lease.Epoch != lease.Epoch {
		t.Fatalf("native locator %+v %v", locator, e)
	}
	transport, e := nodeagent.ManagedProductTransport(agentClient, target, identity)
	if e != nil {
		t.Fatal(e)
	}
	outgoingProduct, e := productrpc.NewClient(productrpc.ClientOptions{URL: "http://127.0.0.1:1", ExpectedNode: target.NodeID, ExpectedBot: identity.BotID, Token: nodeagent.ProductProxyBearer, HTTP: &http.Client{Transport: transport}})
	if e != nil {
		t.Fatal(e)
	}
	defer outgoingProduct.Close()
	proxyIdentity, e := outgoingProduct.Connect(ctx)
	if e != nil || proxyIdentity != identity {
		t.Fatalf("outgoing paired product %+v %v", proxyIdentity, e)
	}
	proxyState, e := outgoingProduct.State(ctx)
	if e != nil || proxyState.Scope != identity.Scope {
		t.Fatalf("outgoing product state %+v %v", proxyState, e)
	}
	proxyReceipt, e := outgoingProduct.Receipt(ctx, "original-source-operation")
	if e != nil || proxyReceipt.Outcome != "unknown" {
		t.Fatalf("original receipt relayed %+v %v", proxyReceipt, e)
	}
	resource, e := outgoingProduct.Upload(ctx, "current.txt", []byte("bounded current attachment"))
	if e != nil {
		t.Fatal(e)
	}
	downloaded, body, e := outgoingProduct.Download(ctx, resource.ID)
	if e != nil || downloaded != resource || string(body) != "bounded current attachment" {
		t.Fatalf("paired resource roundtrip %+v %v", downloaded, e)
	}
	if _, e = agentClient.ProxyManagedProduct(ctx, nodeagent.ManagedProductRequest{Target: target, Identity: identity, Method: "POST", Path: "/arbitrary-shell"}); e == nil {
		t.Fatal("generic product proxy path admitted")
	}
	if disable {
		request := nodeagent.ManagedDisableRequest{Target: target, Lease: lease, OperationID: "disable-original"}
		final, e := agentClient.PrepareManagedDisable(ctx, request)
		if e != nil || final.Version != "2" || final.Epoch != lease.Epoch {
			t.Fatalf("closed native disable %+v %v", final, e)
		}
		durable, e := agentClient.ReconcileManagedDisable(ctx, request)
		if e != nil || durable.Outcome != "accepted" || durable.Snapshot != final {
			t.Fatalf("durable native disable %+v %v", durable, e)
		}
		repeated, e := agentClient.PrepareManagedDisable(ctx, request)
		if e != nil || repeated != final {
			t.Fatalf("disable original receipt %+v %v", repeated, e)
		}
		if _, e = agentClient.ReadManagedProduct(ctx, target); e == nil {
			t.Fatal("disabled native locator remained authoritative")
		}
		cancel()
	} else {
		result, err := client.Command(ctx, productrpc.Command{ID: "explicit-stop", Kind: "stop-bot"})
		if err != nil || result.Outcome != "accepted" {
			select {
			case stopError := <-stopObserved:
				t.Fatalf("owned stop %+v %v native=%v", result, err, stopError)
			default:
				t.Fatalf("owned stop %+v %v", result, err)
			}
		}
	}
	select {
	case err = <-done:
		if err != nil && !(disable && errors.Is(err, context.Canceled)) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native owner did not exit")
	}
	if _, err = broker.CurrentLease(context.Background(), botID); err == nil {
		t.Fatal("stopped owner retained authority")
	}
	methods, err := os.ReadFile(filepath.Join(root, "methods"))
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range strings.Fields(string(methods)) {
		switch method {
		case "initialize", "initialized", "account/read", "model/list", "thread/start", "skills/list":
		default:
			t.Fatalf("replayed native operation %s", method)
		}
	}
	cancel()
	select {
	case <-brokerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("private broker cleanup exceeded")
	}
}

func TestRoamingFlagsRequirePinnedScopeAndLoopback(t *testing.T) {
	valid := []string{"serve-roaming", "--node-id", "n", "--bot-id", "b", "--broker-node-id", "c", "--agent-directory", "/private/a", "--generations", "/private/g", "--broker-socket", "/private/s", "--auth-file", "/private/token"}
	if _, err := parseRoamingCommand(valid, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, tail := range [][]string{{"--backend", "caelis"}, {"--listen", "0.0.0.0:1"}, {"--broker-node-id", ""}, {"--join-target", "peer"}} {
		if _, err := parseRoamingCommand(append(append([]string{}, valid...), tail...), io.Discard); err == nil {
			t.Fatalf("invalid native pairing accepted: %v", tail)
		}
	}
}

func TestRoamingForegroundHelper(t *testing.T) {
	sep := -1
	for i, a := range os.Args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	command, err := parseRoamingCommand(os.Args[sep+1:], io.Discard)
	if err != nil {
		os.Exit(2)
	}
	err = runRoamingCommand(ctx, command, os.Stdout, func(context.Context, func(), func()) (func(), error) { return func() {}, nil })
	if err != nil && !errors.Is(err, context.Canceled) {
		os.Exit(3)
	}
	os.Exit(0)
}
