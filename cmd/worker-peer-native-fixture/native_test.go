//go:build workerpeere2e

package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "app-server" || os.Args[1] == "--version") {
		if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type fixtureClient struct {
	t        *testing.T
	conn     net.Conn
	encode   *json.Encoder
	messages chan nativeMessage
	next     int
}

func fixturePair(t *testing.T) (*nativeServer, *fixtureClient) {
	t.Helper()
	s, err := openNative(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	done := make(chan struct{})
	go func() { _ = s.serve(b, b); close(done) }()
	c := &fixtureClient{t: t, conn: a, encode: json.NewEncoder(a), messages: make(chan nativeMessage, 100)}
	go func() {
		defer close(c.messages)
		dec := json.NewDecoder(a)
		for {
			var m nativeMessage
			if dec.Decode(&m) != nil {
				return
			}
			c.messages <- m
		}
	}()
	t.Cleanup(func() {
		a.Close()
		b.Close()
		<-done
		s.close()
	})
	return s, c
}
func (c *fixtureClient) await(match func(nativeMessage) bool) nativeMessage {
	c.t.Helper()
	for {
		select {
		case m, ok := <-c.messages:
			if !ok {
				c.t.Fatal("native fixture connection closed")
			}
			if match(m) {
				return m
			}
		case <-time.After(5 * time.Second):
			c.t.Fatal("native fixture response timed out")
		}
	}
}
func (c *fixtureClient) call(method string, params any) nativeMessage {
	c.t.Helper()
	c.next++
	id := raw(c.next)
	if err := c.encode.Encode(nativeMessage{ID: id, Method: method, Params: raw(params)}); err != nil {
		c.t.Fatal(err)
	}
	return c.await(func(m nativeMessage) bool { return m.Method == "" && string(m.ID) == string(id) })
}
func (c *fixtureClient) thread() string {
	c.t.Helper()
	if m := c.call("initialize", map[string]any{}); m.Error != nil {
		c.t.Fatal(m.Error)
	}
	m := c.call("thread/start", map[string]any{"cwd": c.t.TempDir()})
	var result struct{ Thread nativeThread }
	if m.Error != nil || json.Unmarshal(m.Result, &result) != nil || result.Thread.ID == "" {
		c.t.Fatal("thread/start", m.Error)
	}
	return result.Thread.ID
}
func (c *fixtureClient) start(thread, clientID, scenario string) string {
	c.t.Helper()
	m := c.call("turn/start", map[string]any{"threadId": thread, "clientUserMessageId": clientID, "input": []nativeInput{{Type: "text", Text: controlMarker + string(raw(fixtureRequest{Action: "worker", Scenario: scenario}))}}})
	var result struct{ Turn nativeTurn }
	if m.Error != nil || json.Unmarshal(m.Result, &result) != nil || result.Turn.ID == "" {
		c.t.Fatal("turn/start", m.Error)
	}
	return result.Turn.ID
}

func TestNativeCSVHistoryContinueAndGuardedInterrupt(t *testing.T) {
	s, c := fixturePair(t)
	thread := c.thread()
	first := c.start(thread, "original-create", "csv")
	c.await(func(m nativeMessage) bool { return m.Method == "turn/completed" })
	m := c.call("thread/turns/list", map[string]any{"threadId": thread, "sortDirection": "asc", "itemsView": "full", "limit": 20})
	var history struct{ Data []nativeTurn }
	if m.Error != nil || json.Unmarshal(m.Result, &history) != nil || len(history.Data) != 1 || history.Data[0].ID != first || len(history.Data[0].Items) != 2 || history.Data[0].Items[0].ClientID != "original-create" {
		t.Fatal("original native identities/history changed", history, m.Error)
	}
	s.mu.Lock()
	workspace := s.state.Threads[thread].Cwd
	s.mu.Unlock()
	bytes, err := os.ReadFile(filepath.Join(workspace, "fixture-result.csv"))
	if err != nil || string(bytes) != "name,value\nfixture,42\n" {
		t.Fatal("native CSV bytes", err)
	}
	second := c.start(thread, "original-continue", "hold")
	if m := c.call("turn/interrupt", map[string]any{"threadId": thread, "turnId": second}); m.Error != nil {
		t.Fatal(m.Error)
	}
	c.await(func(m nativeMessage) bool { return m.Method == "turn/completed" })
	third := c.start(thread, "later-turn", "hold")
	if m := c.call("turn/interrupt", map[string]any{"threadId": thread, "turnId": second}); m.Error == nil {
		t.Fatal("late stop acquired a later turn")
	}
	s.mu.Lock()
	if !s.activeLocked(thread, third) || s.state.Effects["csv:"+thread+":"+first] != 1 || s.state.Effects["turn/interrupt:"+thread+":"+second] != 1 || s.state.ClientInputs["original-create"] != 1 {
		t.Fatal("native effect counts or original stop identity changed")
	}
	s.mu.Unlock()
	persisted, err := os.ReadFile(s.path)
	if err != nil || strings.Contains(string(persisted), "CAELIS_BOT_TOKEN") || strings.Contains(string(persisted), "CAELIS_BOT_ENDPOINT") {
		t.Fatal("MCP configuration leaked into persisted native fixture state", err)
	}
}

func TestNativeURLRequiresOriginalConsentAndIndependentNoncePost(t *testing.T) {
	for _, consentFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "consent-before-post", false: "post-before-consent"}[consentFirst], func(t *testing.T) {
			s, c := fixturePair(t)
			thread := c.thread()
			turn := c.start(thread, "original-url", "url")
			request := c.await(func(m nativeMessage) bool { return m.Method == "mcpServer/elicitation/request" })
			var params struct{ URL, ElicitationID string }
			if json.Unmarshal(request.Params, &params) != nil || params.URL == "" || params.ElicitationID == "" {
				t.Fatal("native URL shape", string(request.Params))
			}
			var id string
			_ = json.Unmarshal(request.ID, &id)
			resp, err := http.Get(params.URL)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			s.mu.Lock()
			nonce := s.state.Pending[id].Nonce
			if s.state.Pending[id].Completed || s.state.Effects["interaction:"+id] != 0 {
				t.Fatal("GET completed native URL interaction")
			}
			s.mu.Unlock()
			// An unrelated native RPC reply is never authority for this URL.
			_ = c.encode.Encode(nativeMessage{ID: raw(id + "-wrong"), Result: raw(map[string]string{"action": "accept"})})
			consent := func() {
				_ = c.encode.Encode(nativeMessage{ID: request.ID, Result: raw(map[string]string{"action": "accept"})})
				c.await(func(m nativeMessage) bool { return m.Method == "serverRequest/resolved" })
			}
			post := func(value string) int {
				req, _ := http.NewRequest("POST", params.URL, strings.NewReader(url.Values{"nonce": {value}}.Encode()))
				req.Header.Set("Origin", s.urlBase)
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				return response.StatusCode
			}
			if post("wrong-nonce") != 403 {
				t.Fatal("nonce mismatch accepted")
			}
			if consentFirst {
				consent()
			} else if post(nonce) != 200 {
				t.Fatal("original challenge POST rejected")
			}
			s.mu.Lock()
			if !s.activeLocked(thread, turn) || s.state.Effects["interaction:"+id] != 0 || s.state.Effects["csv:"+thread+":"+turn] != 0 {
				t.Fatal("one condition alone completed native work")
			}
			s.mu.Unlock()
			if consentFirst {
				if post(nonce) != 200 {
					t.Fatal("consent incorrectly closed the still-pending challenge")
				}
			} else {
				consent()
			}
			c.await(func(m nativeMessage) bool { return m.Method == "turn/completed" })
			s.mu.Lock()
			if s.state.Effects["interaction:"+id] != 1 || s.state.Effects["native-reply:"+id] != 1 || s.state.Effects["challenge-post:"+id] != 1 || s.state.Effects["csv:"+thread+":"+turn] != 1 {
				t.Fatal("original native URL effect counts changed", s.state.Effects)
			}
			s.mu.Unlock()
		})
	}
}

func TestNativeCLIWithProductionCodexWorkerAdapter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKER_PEER_NATIVE_FIXTURE", "1")
	t.Setenv("WORKER_PEER_FIXTURE_ROOT", root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "empty-native-home"))
	private, workspace := filepath.Join(root, "native-owner"), filepath.Join(root, "tasks", "task-"+strings.Repeat("a", 32))
	for _, dir := range []string{private, workspace} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := codex.NewWorkOwner(codex.SessionOptions{Binary: os.Args[0], Directory: private, StateFile: filepath.Join(root, "worker-binding.json"), WorkRoot: filepath.Join(root, "tasks")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := owner.Close(ctx); err != nil {
			t.Error("owned fixture native cleanup", err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := owner.Connect(ctx); err != nil {
		t.Fatal("production native handshake/readiness", err)
	}
	id := "task-" + strings.Repeat("a", 32)
	requestID := "fixture-real-adapter-create"
	task, err := owner.StartWork(ctx, api.WorkStart{ID: id, TaskStart: api.TaskStart{RequestID: requestID, Title: "Synthetic CSV", Prompt: controlMarker + string(raw(fixtureRequest{Action: "worker", Scenario: "csv"}))}, Workspace: workspace, Instructions: botpolicy.WorkerInstructions})
	if err != nil || task.ID != id {
		t.Fatal("production Worker start", task, err)
	}
	for {
		task, err = owner.ReadWork(ctx, id)
		if err != nil {
			t.Fatal("production native observation", err)
		}
		if task.Status == "completed" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("native Worker did not complete", task)
		case <-time.After(10 * time.Millisecond):
		}
	}
	history, err := owner.ReadWorkConversation(ctx, id)
	if err != nil || len(history) != 2 || len(history[1].Artifacts) != 1 {
		t.Fatal("production native history/artifact projection", history, err)
	}
	file, err := owner.ReadWorkFile(ctx, id, history[1].Artifacts[0].ID)
	if err != nil || string(file.Data) != "name,value\nfixture,42\n" {
		t.Fatal("production task-scoped CSV bytes", err)
	}
	data, err := os.ReadFile(filepath.Join(private, ".worker-peer-native-fixture", "state.json"))
	var evidence nativeState
	if err != nil || json.Unmarshal(data, &evidence) != nil || evidence.ClientInputs[requestID] != 1 || evidence.PID == os.Getpid() {
		t.Fatal("real child/native original-input evidence", err)
	}
}

func TestProductionResidentStopRecoversOnlyAfterOwnedNativeExit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKER_PEER_NATIVE_FIXTURE", "1")
	t.Setenv("WORKER_PEER_FIXTURE_ROOT", root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "empty-home"))
	dir := filepath.Join(root, "resident")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "binding.json")
	s := codex.NewSession(codex.SessionOptions{Binary: os.Args[0], Directory: dir, StateFile: state})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	var before map[string]json.RawMessage
	_ = json.Unmarshal(original, &before)
	if receipt, err := s.Submit(ctx, api.Submission{ID: "resident-original-hold", Text: controlMarker + string(raw(fixtureRequest{Action: "worker", Scenario: "hold"}))}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !s.Snapshot().CanInterrupt && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !s.Snapshot().CanInterrupt {
		t.Fatal("native hold did not start")
	}
	if err := s.Interrupt(ctx); err != nil {
		t.Fatal("real owned native Stop recovery", err)
	}
	if got := s.Snapshot(); got.Connection != "ready" || !got.CanSend {
		t.Fatal("resident stuck after confirmed stop", got.Connection, got.Phase)
	}
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	var after struct {
		OwnerEndpoint string `json:"ownerEndpoint"`
		Lost          []struct {
			Endpoint    string       `json:"endpoint"`
			LastReceipt *api.Receipt `json:"lastReceipt"`
		} `json:"lostOwners"`
	}
	if err = json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	var endpoint string
	_ = json.Unmarshal(before["ownerEndpoint"], &endpoint)
	if len(after.Lost) != 1 || after.Lost[0].Endpoint != endpoint || after.OwnerEndpoint == endpoint || after.Lost[0].LastReceipt == nil || after.Lost[0].LastReceipt.ID != "resident-original-hold" {
		t.Fatal("original owner/receipt lost or silently rebound", after)
	}
}

func TestNativeURLRevokesOnOriginalDeclineCancelOrInterrupt(t *testing.T) {
	for _, ending := range []string{"decline", "cancel", "interrupt"} {
		t.Run(ending, func(t *testing.T) {
			s, c := fixturePair(t)
			thread := c.thread()
			turn := c.start(thread, "original-url-lifetime", "url")
			request := c.await(func(m nativeMessage) bool { return m.Method == "mcpServer/elicitation/request" })
			var params struct{ URL string }
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Fatal(err)
			}
			var id string
			_ = json.Unmarshal(request.ID, &id)
			s.mu.Lock()
			nonce := s.state.Pending[id].Nonce
			s.mu.Unlock()
			origin, err := url.Parse(params.URL)
			if err != nil {
				t.Fatal(err)
			}
			post := func(target, nonce, source string) int {
				r, err := http.NewRequest(http.MethodPost, target, strings.NewReader(url.Values{"nonce": {nonce}}.Encode()))
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Origin", source)
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				response, err := http.DefaultClient.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, response.Body)
				response.Body.Close()
				return response.StatusCode
			}
			base := origin.Scheme + "://" + origin.Host
			if post(params.URL, nonce, "http://unrelated.invalid") != 403 || post(params.URL, nonce+"-wrong", base) != 403 || post(params.URL+"-other-request", nonce, base) != 410 {
				t.Fatal("challenge escaped original request/origin/nonce")
			}
			if ending == "interrupt" {
				if reply := c.call("turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}); reply.Error != nil {
					t.Fatal(reply.Error)
				}
			} else {
				_ = c.encode.Encode(nativeMessage{ID: request.ID, Result: raw(map[string]string{"action": ending})})
			}
			c.await(func(m nativeMessage) bool { return m.Method == "turn/completed" })
			if post(params.URL, nonce, base) != 410 {
				t.Fatal("ended original challenge remained writable")
			}
			_ = c.encode.Encode(nativeMessage{ID: request.ID, Result: raw(map[string]string{"action": "accept"})})
			// A real subsequent RPC is an ordering barrier for the late reply.
			c.call("thread/turns/list", map[string]any{"threadId": thread, "sortDirection": "asc", "itemsView": "full", "limit": 20})
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.state.Effects["interaction:"+id] != 0 || s.state.Effects["challenge-post:"+id] != 0 || s.state.Effects["csv:"+thread+":"+turn] != 0 {
				t.Fatal("late decision revived ended original challenge", s.state.Effects)
			}
		})
	}
}
