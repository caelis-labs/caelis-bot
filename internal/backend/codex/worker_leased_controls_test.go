//go:build (darwin && cgo) || linux

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
	"github.com/coder/websocket"
)

// The native port is an actual owned Unix/WebSocket child under the independent
// watchdog. Its deliberately synthetic native responses require no credentials.
func TestLeasedControlProcessHelper(t *testing.T) {
	sep := -1
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return
	}
	args := os.Args[sep+1:]
	if len(args) != 4 || args[1] != "app-server" || args[2] != "--listen" {
		os.Exit(2)
	}
	store := args[0]
	listener, err := net.Listen("unix", strings.TrimPrefix(args[3], "unix://"))
	if err != nil {
		os.Exit(3)
	}
	threads := map[string]nativeThread{}
	nextThread, nextRun := 0, 0
	server := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		write := func(m wireMessage) bool {
			b, _ := json.Marshal(m)
			return ws.Write(r.Context(), websocket.MessageText, b) == nil
		}
		for {
			_, b, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var m wireMessage
			if json.Unmarshal(b, &m) != nil {
				return
			}
			var p map[string]any
			_ = json.Unmarshal(m.Params, &p)
			if m.Method == "" {
				_ = os.WriteFile(filepath.Join(store, "decision.json"), b, 0600)
				continue
			}
			var result any = map[string]any{}
			var event *wireMessage
			switch m.Method {
			case "initialize":
				result = map[string]string{"userAgent": "leased-control-fixture"}
			case "initialized":
				continue
			case "account/read":
				result = map[string]any{"account": map[string]string{"type": "apiKey"}, "requiresOpenaiAuth": true}
			case "config/read":
				result = map[string]any{"config": map[string]any{"model": "node-native-model", "model_reasoning_effort": "medium"}}
			case "model/list", "skills/list":
				result = map[string]any{"data": []any{}}
			case "thread/start":
				nextThread++
				thread := nativeThread{ID: fmt.Sprintf("thread-%d", nextThread)}
				threads[thread.ID] = thread
				result = threadExecutionResponse{Thread: thread, Model: "node-native-model", ModelProvider: "native-provider", ReasoningEffort: "medium"}
			case "thread/read", "thread/resume":
				id, _ := p["threadId"].(string)
				result = threadExecutionResponse{Thread: threads[id], Model: "node-native-model", ModelProvider: "native-provider", ReasoningEffort: "medium"}
			case "turn/start":
				nextRun++
				id, _ := p["threadId"].(string)
				clientID, _ := p["clientUserMessageId"].(string)
				turn := nativeTurn{ID: fmt.Sprintf("turn-%d", nextRun), Status: "inProgress", Items: []nativeItem{{ID: "user", Type: "userMessage", ClientID: clientID}}}
				thread := threads[id]
				thread.Turns = append(thread.Turns, turn)
				thread.Status.Type = "active"
				threads[id] = thread
				result = map[string]any{"turn": turn}
				if nextRun == 1 {
					event = &wireMessage{ID: raw("native-approval-original"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{"threadId": id, "turnId": turn.ID, "itemId": "command-original", "command": "synthetic", "cwd": p["cwd"], "availableDecisions": []string{"accept", "decline"}})}
				}
			case "turn/interrupt":
				countData, _ := os.ReadFile(filepath.Join(store, "interrupt-count"))
				count, _ := strconv.Atoi(string(countData))
				_ = os.WriteFile(filepath.Join(store, "interrupt-count"), []byte(strconv.Itoa(count+1)), 0600)
				_ = os.WriteFile(filepath.Join(store, "interrupt.json"), m.Params, 0600)
				id, _ := p["threadId"].(string)
				run, _ := p["turnId"].(string)
				mode, _ := os.ReadFile(filepath.Join(store, "mode"))
				thread := threads[id]
				if string(mode) == "unknown" {
					nextRun++
					turn := nativeTurn{ID: fmt.Sprintf("later-turn-%d", nextRun), Status: "inProgress"}
					thread.Turns = append(thread.Turns, turn)
					threads[id] = thread
					if !write(wireMessage{ID: m.ID, Error: &NativeError{Code: -32000, Message: "original interruption outcome unavailable"}}) {
						return
					}
					if !write(wireMessage{Method: "turn/started", Params: raw(map[string]any{"threadId": id, "turn": turn})}) {
						return
					}
					continue
				}
				turn := nativeTurn{ID: run, Status: "interrupted"}
				for i := range thread.Turns {
					if thread.Turns[i].ID == run {
						thread.Turns[i].Status = "interrupted"
						turn = thread.Turns[i]
					}
				}
				thread.Status.Type = "idle"
				threads[id] = thread
				event = &wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": id, "turn": turn})}
			case "thread/unsubscribe":
			default:
				if !write(wireMessage{ID: m.ID, Error: &NativeError{Code: -32601, Message: "unsupported fixture method"}}) {
					return
				}
				continue
			}
			if !write(wireMessage{ID: m.ID, Result: raw(result)}) {
				return
			}
			if event != nil && !write(*event) {
				return
			}
		}
	})}
	_ = server.Serve(listener)
	os.Exit(0)
}

type leasedCodexControlFixture struct {
	worker       *WorkerClient
	client       *workerwire.Client
	source       *workerSourceFixture
	reader       *leaseReaderFixture
	store        string
	first, other api.WorkStart
}

func newLeasedCodexControlFixture(t *testing.T) leasedCodexControlFixture {
	t.Helper()
	dir := t.TempDir()
	store := filepath.Join(dir, "native")
	if err := os.Mkdir(store, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(dir, "codex")
	if err = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestLeasedControlProcessHelper$' -- "+quote(store)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	watchdog, _, _, _ := supervisorFixture(t)
	grant := leaseFixtureGrant()
	source := &workerSourceFixture{value: api.WorkDispatchSource{NodeID: grant.SourceNodeID, Backend: grant.Backend, BindingID: "resident-original", OperationID: "activation-original", Kind: "native_activation", Lease: grant}}
	reader := &leaseReaderFixture{lease: nodeplane.Lease{BotID: grant.BotID, NodeID: grant.SourceNodeID, Backend: api.NodeCodex, Epoch: grant.Epoch, TTLMs: 60000}}
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: "leased-worker", Backend: "codex", Role: api.RoleWorker}, BotID: api.ProfileBotID(grant.BotID), SourceNode: grant.SourceNodeID, SourceBackend: grant.Backend}
	w := NewWorker(WorkerOptions{Target: pair.Target, Directory: filepath.Join(dir, "worker"), Binary: binary, Pair: &pair, Source: workerwire.SourceProvider(), Lease: &WorkerLeaseOptions{HelperPath: watchdog, BrokerNodeID: grant.BrokerNodeID, RawBotID: grant.BotID, SourceNode: grant.SourceNodeID, SourceBackend: grant.Backend, Reader: reader, BindPower: func(context.Context, func(), func()) (func(), error) { return func() {}, nil }}})
	owner := nodeworker.New(w)
	t.Cleanup(func() {
		if err := owner.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err = owner.Start(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if !w.LeaseAwareAdmission() {
		t.Fatal("real independent native fence unavailable")
	}
	server, err := workerwire.NewServer(owner, pair)
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(testContext(t))
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, a) }()
	client, err := workerwire.NewClient(testContext(t), pair, source, b)
	if err != nil {
		cancel()
		_ = b.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("observer not detached")
		}
	})
	start := func(n int) api.WorkStart {
		in := api.WorkStart{ID: fmt.Sprintf("task-%032x", n), Source: source.value, RequestDigest: strings.Repeat("a", 64), Workspace: filepath.Join(w.workRoot(), fmt.Sprintf("task-%032x", n)), Instructions: "synthetic bounded worker policy", TaskStart: api.TaskStart{RequestID: fmt.Sprintf("request-%d", n), Title: "fixture task", Prompt: "fixture request", Target: &pair.Target}}
		if err := os.Mkdir(in.Workspace, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := client.StartWork(testContext(t), in); err != nil {
			t.Fatal(err)
		}
		return in
	}
	first := start(1)
	other := start(2)
	awaitState(t, w.engine, func(api.Snapshot) bool { return len(w.WorkApprovals()) == 1 })
	source.mu.Lock()
	source.err = api.ErrWorkSourceInactive
	source.mu.Unlock()
	return leasedCodexControlFixture{w, client, source, reader, store, first, other}
}
func fixtureInterruptCount(t *testing.T, store string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(store, "interrupt-count"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count, err := strconv.Atoi(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return count
}
func TestLeasedWorkerWireIdleControlsRetainExactOriginalAuthority(t *testing.T) {
	d := newLeasedCodexControlFixture(t)
	s := d.worker.engine
	s.mu.Lock()
	task := s.binding.Tasks[d.first.ID]
	thread, run := task.Thread, task.Run
	s.mu.Unlock()
	approval := d.worker.WorkApprovals()[0]
	// An arbitrary provider fault is visible before native bytes; only explicit
	// connected inactivity permits the target's private paired-control authority.
	failure := errors.New("source callback failed")
	d.source.mu.Lock()
	d.source.err = failure
	d.source.mu.Unlock()
	if _, err := d.client.StopWork(testContext(t), d.first.ID); !errors.Is(err, failure) {
		t.Fatal("source error suppressed", err)
	}
	if fixtureInterruptCount(t, d.store) != 0 {
		t.Fatal("fault dispatched native cancel")
	}
	d.source.mu.Lock()
	d.source.err = api.ErrWorkSourceInactive
	d.source.mu.Unlock()
	if err := d.client.DecideWork(testContext(t), approval, api.Decision{ID: approval.Approval.ID, Choice: approval.Approval.Choices[1].ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.client.StopWork(testContext(t), d.first.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, s, func(api.Snapshot) bool {
		v, e := d.worker.ReadWork(testContext(t), d.first.ID)
		return e == nil && v.Status == "interrupted"
	})
	data, err := os.ReadFile(filepath.Join(d.store, "decision.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reply wireMessage
	if json.Unmarshal(data, &reply) != nil || string(reply.ID) != `"native-approval-original"` || !strings.Contains(string(reply.Result), "decline") {
		t.Fatal("native approval identity changed", string(data))
	}
	data, err = os.ReadFile(filepath.Join(d.store, "interrupt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var interrupt map[string]string
	_ = json.Unmarshal(data, &interrupt)
	if interrupt["threadId"] != thread || interrupt["turnId"] != run || fixtureInterruptCount(t, d.store) != 1 {
		t.Fatal("native original turn changed", interrupt)
	}
	if v, err := d.worker.ReadWork(testContext(t), d.other.ID); err != nil || v.Status != "working" {
		t.Fatal("other worker turn changed", v, err)
	}
	// Read-only observation and pairing are insufficient without the original
	// source broker still granting the exact epoch.
	d.reader.mu.Lock()
	d.reader.err = errors.New("broker original lease revoked")
	d.reader.mu.Unlock()
	if _, err := d.client.StopWork(testContext(t), d.other.ID); err == nil {
		t.Fatal("idle control ignored live broker")
	}
	if fixtureInterruptCount(t, d.store) != 1 {
		t.Fatal("revoked lease sent native bytes")
	}
}
func TestLeasedWorkerWireUnknownCancelNeverTargetsLaterTurn(t *testing.T) {
	d := newLeasedCodexControlFixture(t)
	if err := os.WriteFile(filepath.Join(d.store, "mode"), []byte("unknown"), 0600); err != nil {
		t.Fatal(err)
	}
	s := d.worker.engine
	s.mu.Lock()
	original := s.binding.Tasks[d.first.ID].Run
	thread := s.binding.Tasks[d.first.ID].Thread
	s.mu.Unlock()
	if _, err := d.client.StopWork(testContext(t), d.first.ID); err == nil {
		t.Fatal("uncertain native cancellation hidden")
	}
	awaitState(t, s, func(api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.childRuns[thread] != "" && s.childRuns[thread] != original
	})
	if _, err := d.client.StopWork(testContext(t), d.first.ID); err == nil {
		t.Fatal("unknown receipt replayed")
	}
	if fixtureInterruptCount(t, d.store) != 1 {
		t.Fatal("later native turn cancelled by replay")
	}
	s.mu.Lock()
	receipt := s.binding.Tasks[d.first.ID].WorkerStop
	s.mu.Unlock()
	if receipt == nil || receipt.Run != original || receipt.Outcome != "unknown" {
		t.Fatal("original uncertain identity lost", receipt)
	}
}

func TestLeasedWorkerWireExpiredOriginalControlsLeaveOtherOwnerLive(t *testing.T) {
	for _, kind := range []string{"stop", "decision"} {
		t.Run(kind, func(t *testing.T) {
			expired := newLeasedCodexControlFixture(t)
			other := newLeasedCodexControlFixture(t)
			expired.reader.mu.Lock()
			expired.reader.lease.TTLMs = 15000
			expired.reader.mu.Unlock()
			if kind == "stop" {
				if _, err := expired.client.StopWork(testContext(t), expired.first.ID); err == nil {
					t.Fatal("expired original lease cancelled native turn")
				}
			} else {
				approval := expired.worker.WorkApprovals()[0]
				if err := expired.client.DecideWork(testContext(t), approval, api.Decision{ID: approval.Approval.ID, Choice: approval.Approval.Choices[1].ID}); err == nil {
					t.Fatal("expired original lease approved native request")
				}
			}
			if fixtureInterruptCount(t, expired.store) != 0 {
				t.Fatal("expired owner sent native cancel")
			}
			if _, err := os.Stat(filepath.Join(expired.store, "decision.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("expired owner sent native approval", err)
			}
			if expired.worker.LeaseAwareAdmission() {
				t.Fatal("expired original owner still reports authority")
			}
			if !other.worker.LeaseAwareAdmission() {
				t.Fatal("separate actual native owner stopped")
			}
			if v, err := other.worker.ReadWork(testContext(t), other.other.ID); err != nil || v.Status != "working" {
				t.Fatal("other actual owner task lost", v, err)
			}
			if _, err := other.client.StopWork(testContext(t), other.other.ID); err != nil {
				t.Fatal("other owner live lease control failed", err)
			}
			if fixtureInterruptCount(t, other.store) != 1 {
				t.Fatal("other owner lost exact native control")
			}
		})
	}
}
