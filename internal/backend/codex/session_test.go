package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Wire fixtures use the real transport and session projection. They control
// event/reply order explicitly instead of retrying a timing-dependent test.
type sessionFixture struct {
	modelPages     map[string]any
	pages          map[string]turnPage
	pageCalls      []string
	failPage       bool
	resumeExcluded bool
	mu             sync.Mutex
	writeMu        sync.Mutex
	peer           net.Conn
	history        []nativeTurn
	workers        map[string]nativeThread
	mode           string
	starts         int
	cleaned        int
	started        chan struct{}
	answers        chan wireMessage
	lastParams     map[string]json.RawMessage
	connections    int
	loginReply     chan struct{}
}

func sessionPair(t *testing.T, mode string) (*Session, *sessionFixture) {
	t.Helper()
	dir := t.TempDir()
	s := NewSession(SessionOptions{Directory: filepath.Join(dir, "work"), StateFile: filepath.Join(dir, "binding.json")})
	f := &sessionFixture{mode: mode, started: make(chan struct{}, 8), answers: make(chan wireMessage, 8), loginReply: make(chan struct{})}
	s.start = func(context.Context, Options) (*Client, error) {
		a, b := net.Pipe()
		f.mu.Lock()
		f.peer = b
		f.connections++
		f.mu.Unlock()
		go f.serve(b)
		return &Client{newTransportOptions(a, nil, true)}, nil
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Close(ctx)
		f.mu.Lock()
		if f.peer != nil {
			f.peer.Close()
		}
		f.mu.Unlock()
	})
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	return s, f
}
func raw(value any) json.RawMessage { b, _ := json.Marshal(value); return b }
func (f *sessionFixture) emitTo(peer net.Conn, m wireMessage) {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	_ = json.NewEncoder(peer).Encode(m)
}
func (f *sessionFixture) emit(m wireMessage) {
	f.mu.Lock()
	peer := f.peer
	f.mu.Unlock()
	f.emitTo(peer, m)
}
func (f *sessionFixture) serve(peer net.Conn) {
	defer peer.Close()
	d := json.NewDecoder(peer)
	for {
		var m wireMessage
		if d.Decode(&m) != nil {
			return
		}
		if m.Method == "" {
			f.answers <- m
			continue
		}
		var result any = map[string]any{}
		switch m.Method {
		case "account/read":
			result = map[string]any{"account": map[string]string{"type": "apiKey"}, "requiresOpenaiAuth": true}
		case "thread/start", "thread/resume", "thread/read":
			f.mu.Lock()
			history := append([]nativeTurn(nil), f.history...)
			if m.Method == "thread/resume" {
				var p struct {
					ExcludeTurns bool `json:"excludeTurns"`
				}
				_ = json.Unmarshal(m.Params, &p)
				f.resumeExcluded = p.ExcludeTurns
				if p.ExcludeTurns {
					history = nil
				}
			}
			f.mu.Unlock()
			result = map[string]any{"thread": nativeThread{ID: "thread-native", Turns: history}}
			if m.Method == "thread/read" {
				var params struct {
					ThreadID string `json:"threadId"`
				}
				_ = json.Unmarshal(m.Params, &params)
				f.mu.Lock()
				worker, ok := f.workers[params.ThreadID]
				f.mu.Unlock()
				if ok {
					result = map[string]any{"thread": worker}
				}
			}
		case "thread/turns/list":
			var p struct {
				Cursor        string `json:"cursor"`
				Limit         int    `json:"limit"`
				SortDirection string `json:"sortDirection"`
				ItemsView     string `json:"itemsView"`
			}
			_ = json.Unmarshal(m.Params, &p)
			f.mu.Lock()
			page, ok := f.pages[p.Cursor]
			fail := f.failPage
			f.pageCalls = append(f.pageCalls, p.Cursor)
			f.mu.Unlock()
			if !ok {
				f.emitTo(peer, wireMessage{ID: m.ID, Error: &NativeError{Code: -32601, Message: "unsupported"}})
				continue
			}
			if fail || p.Limit != historyPageSize || p.SortDirection != "desc" || p.ItemsView != "full" {
				f.emitTo(peer, wireMessage{ID: m.ID, Error: &NativeError{Code: -32000, Message: "synthetic read failure"}})
				continue
			}
			result = page
		case "account/login/start":
			f.emitTo(peer, wireMessage{Method: "account/login/completed", Params: raw(map[string]any{"loginId": "fixture-login", "success": true})})
			<-f.loginReply
			result = map[string]string{"type": "chatgpt", "loginId": "fixture-login", "authUrl": "https://example.com/fixture"}
		case "model/list":
			var p struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(m.Params, &p)
			f.mu.Lock()
			result = f.modelPages[p.Cursor]
			f.mu.Unlock()
			if result == nil {
				result = map[string]any{"data": []any{}}
			}
		case "skills/list":
			result = map[string]any{"data": []any{}}
		case "turn/start":
			var params map[string]json.RawMessage
			_ = json.Unmarshal(m.Params, &params)
			var clientID string
			_ = json.Unmarshal(params["clientUserMessageId"], &clientID)
			f.mu.Lock()
			f.starts++
			f.lastParams = params
			mode := f.mode
			f.mu.Unlock()
			f.started <- struct{}{}
			user := nativeItem{ID: "user", Type: "userMessage", ClientID: clientID, Content: []nativeInput{{Type: "text", Text: "synthetic"}}}
			turn := nativeTurn{ID: "run-native", Status: "completed", Items: []nativeItem{user, {ID: "answer", Type: "agentMessage", Text: "final synthetic result"}}}
			if mode == "disconnect" {
				f.mu.Lock()
				f.history = []nativeTurn{turn}
				f.mu.Unlock()
				return
			}
			if mode == "block" {
				continue
			}
			f.emitTo(peer, wireMessage{Method: "turn/started", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: turn.ID, Status: "inProgress"}})})
			if mode == "early-terminal" {
				f.emitTo(peer, wireMessage{Method: "item/agentMessage/delta", Params: raw(map[string]any{"threadId": "thread-native", "turnId": turn.ID, "itemId": "answer", "delta": "partial"})})
				f.emitTo(peer, wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": turn})})
			}
			result = map[string]any{"turn": nativeTurn{ID: turn.ID, Status: "inProgress"}}
		case "turn/steer":
			var params map[string]json.RawMessage
			_ = json.Unmarshal(m.Params, &params)
			f.mu.Lock()
			f.lastParams = params
			f.mu.Unlock()
			result = map[string]string{"turnId": "run-native"}
		case "turn/interrupt":
			var params struct {
				ThreadID string `json:"threadId"`
				TurnID   string `json:"turnId"`
			}
			_ = json.Unmarshal(m.Params, &params)
			f.emitTo(peer, wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": params.ThreadID, "turn": nativeTurn{ID: params.TurnID, Status: "interrupted"}})})
		case "thread/backgroundTerminals/list":
			result = map[string]any{"data": []any{}, "nextCursor": nil}
		case "thread/backgroundTerminals/clean":
			f.mu.Lock()
			f.cleaned++
			f.mu.Unlock()
		default:
			f.emitTo(peer, wireMessage{ID: m.ID, Error: &NativeError{Code: -32601, Message: "not in fixture"}})
			continue
		}
		f.emitTo(peer, wireMessage{ID: m.ID, Result: raw(result)})
	}
}
func awaitState(t *testing.T, s *Session, predicate func(api.Snapshot) bool) api.Snapshot {
	t.Helper()
	ctx := testContext(t)
	for {
		s.mu.Lock()
		changed := s.changed
		s.mu.Unlock()
		view := s.Snapshot()
		if predicate(view) {
			return view
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal("state transition not observed", view.Connection, view.Phase)
		}
	}
}
func sendSynthetic(t *testing.T, s *Session, id string) api.Receipt {
	t.Helper()
	r, err := s.Submit(testContext(t), api.Submission{ID: id, Text: "synthetic"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestTerminalEventBeforeStartReplyAndDuplicateCompletion(t *testing.T) {
	s, f := sessionPair(t, "early-terminal")
	if r := sendSynthetic(t, s, "submit-early-terminal"); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	view := awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	if !view.CanSend || view.CanInterrupt {
		t.Fatal("late start reply resurrected a finished run")
	}
	turn := nativeTurn{ID: "run-native", Status: "completed", Items: []nativeItem{{ID: "answer", Type: "agentMessage", Text: "final synthetic result"}}}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": turn})})
	f.emit(wireMessage{Method: "item/agentMessage/delta", Params: raw(map[string]any{"threadId": "thread-native", "turnId": turn.ID, "itemId": "answer", "delta": "late duplicate"})})
	view = awaitState(t, s, func(v api.Snapshot) bool { return v.Revision > view.Revision+1 })
	count := 0
	for _, item := range view.Items {
		if item.Kind == "assistant" {
			count++
			if item.Text != "final synthetic result" {
				t.Fatal(item.Text)
			}
		}
	}
	if count != 1 {
		t.Fatal("completion replay duplicated content")
	}
}
func TestUnknownSendRecoversByNativeClientIdentityWithoutReplay(t *testing.T) {
	s, f := sessionPair(t, "disconnect")
	r := sendSynthetic(t, s, "submit-unknown-proof")
	if r.Outcome != "unknown" {
		t.Fatal(r)
	}
	if s.Snapshot().CanSend {
		t.Fatal("unknown send allows accidental retry")
	}
	b, err := os.ReadFile(s.opts.StateFile)
	if err != nil || !strings.Contains(string(b), "submit-unknown-proof") {
		t.Fatal("pending identity not durable")
	}
	if err = s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	view := s.Snapshot()
	if !view.CanSend || view.LastReceipt.ID != r.ID || view.LastReceipt.Outcome != "accepted" {
		t.Fatal(view.Phase, view.LastReceipt)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts != 1 {
		t.Fatal("mutation replayed")
	}
}
func TestSteerUsesExpectedRunAndInterruptWaitsForTerminal(t *testing.T) {
	s, f := sessionPair(t, "hold")
	if sendSynthetic(t, s, "first-input").Outcome != "accepted" {
		t.Fatal("start")
	}
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSteer })
	if sendSynthetic(t, s, "second-input").Outcome != "accepted" {
		t.Fatal("steer")
	}
	f.mu.Lock()
	target := string(f.lastParams["expectedTurnId"])
	f.mu.Unlock()
	if target != `"run-native"` {
		t.Fatal(target)
	}
	if err := s.Interrupt(testContext(t)); err != nil {
		t.Fatal(err)
	}
	view := awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "interrupted" })
	if !view.CanSend {
		t.Fatal("terminal observation missing")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connections != 2 || f.starts != 1 {
		t.Fatal("interrupt must recycle owner and resume without replay")
	}
}
func approvalMessage(id string) wireMessage {
	return wireMessage{ID: raw(id), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "itemId": "command", "command": "echo synthetic", "cwd": "/fixture", "availableDecisions": []string{"accept", "decline"}})}
}
func TestApprovalKeepsNativeTargetChoicesAndRejectsStaleOrDuplicateButtons(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "approval-run")
	f.emit(approvalMessage("native-reused"))
	view := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	p := view.Approvals[0]
	if len(p.Choices) != 2 || !strings.Contains(p.Details, "echo synthetic") {
		t.Fatal(p)
	}
	if s.Decide(testContext(t), api.Decision{ID: p.ID, Choice: "acceptForSession"}) == nil {
		t.Fatal("unoffered authority accepted")
	}
	if err := s.Decide(testContext(t), api.Decision{ID: p.ID, Choice: p.Choices[0].ID}); err != nil {
		t.Fatal(err)
	}
	answer := <-f.answers
	if string(answer.ID) != `"native-reused"` || string(answer.Result) != `{"decision":"accept"}` {
		t.Fatal("native target/decision changed")
	}
	if s.Decide(testContext(t), api.Decision{ID: p.ID, Choice: p.Choices[0].ID}) == nil {
		t.Fatal("approval replayed")
	}
	f.emit(wireMessage{Method: "serverRequest/resolved", Params: raw(map[string]any{"threadId": "thread-native", "requestId": "native-reused"})})
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.Approvals[0].Status == "resolved" && v.Phase == "working" && v.CanSteer
	})
	f.emit(approvalMessage("native-reused"))
	view = awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 2 })
	if view.Approvals[1].ID == p.ID {
		t.Fatal("native ID reuse aliased the product handle")
	}
	if s.Decide(testContext(t), api.Decision{ID: p.ID, Choice: p.Choices[0].ID}) == nil {
		t.Fatal("stale button approved new request")
	}
}
func TestTransportGenerationPreventsApprovalRaceAfterNativeIDReuse(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	rpc := newTransportOptions(a, nil, true)
	defer rpc.close()
	writeWire(t, b, approvalMessage("reuse"))
	first := <-rpc.events
	writeWire(t, b, wireMessage{Method: "serverRequest/resolved", Params: raw(map[string]string{"requestId": "reuse"})})
	<-rpc.events
	writeWire(t, b, approvalMessage("reuse"))
	next := <-rpc.events
	if next.Sequence == first.Sequence {
		t.Fatal("generation not advanced")
	}
	if err := rpc.respond(testContext(t), first.RequestID, first.Sequence, map[string]string{"decision": "accept"}, nil); err == nil {
		t.Fatal("stale response crossed native generation")
	}
	ctx := testContext(t)
	done := make(chan error, 1)
	go func() {
		done <- rpc.respond(ctx, next.RequestID, next.Sequence, map[string]string{"decision": "decline"}, nil)
	}()
	answer := readWire(t, json.NewDecoder(b))
	if string(answer.Result) != `{"decision":"decline"}` {
		t.Fatal(answer)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestShutdownCancelsPendingObservationWithoutDeadlockAndCleansTerminals(t *testing.T) {
	s, f := sessionPair(t, "block")
	ctx := testContext(t)
	receipt := make(chan api.Receipt, 1)
	go func() {
		r, _ := s.Submit(ctx, api.Submission{ID: "pending-shutdown", Text: "synthetic"}, nil)
		receipt <- r
	}()
	<-f.started
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-receipt:
		if r.Outcome != "unknown" {
			t.Fatal(r)
		}
	case <-ctx.Done():
		t.Fatal("shutdown deadlocked behind a pending request")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cleaned != 1 {
		t.Fatal("background cleanup omitted")
	}
}
func TestPermissionProjectionNeverGrantsUnrequestedScope(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "permission-run")
	f.emit(wireMessage{ID: raw(900), Method: "item/permissions/requestApproval", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "itemId": "permission", "cwd": "/fixture", "permissions": map[string]any{"fileSystem": map[string]any{"read": []string{"/fixture/only"}, "write": nil}, "network": nil}})})
	view := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) > 0 })
	if err := s.Decide(testContext(t), api.Decision{ID: view.Approvals[0].ID, Choice: "allow", Answers: map[string][]string{"network": {"true"}}}); err != nil {
		t.Fatal(err)
	}
	response := <-f.answers
	var data struct {
		Permissions map[string]json.RawMessage
		Scope       string
	}
	_ = json.Unmarshal(response.Result, &data)
	if data.Scope != "turn" || len(data.Permissions) != 1 || data.Permissions["network"] != nil {
		t.Fatal("grant expanded")
	}
}
func TestAttachmentCopiesAndFormsValidateBeforeNativeDispatch(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	defer s.cancelLife()
	source := filepath.Join(t.TempDir(), "资料 with spaces.txt")
	if err := os.WriteFile(source, []byte("synthetic only"), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := s.prepareInput(api.Submission{Text: "read"}, []api.InputFile{{Name: filepath.Base(source), Path: source}}, nil)
	if err != nil || len(input) != 2 {
		t.Fatal(err)
	}
	text := input[1]["text"].(string)
	if strings.Contains(text, source) || !strings.Contains(text, ".attachments") {
		t.Fatal("original path used instead of retained copy")
	}
	if _, err = s.prepareInput(api.Submission{}, []api.InputFile{{Name: "dir", Path: t.TempDir()}}, nil); err == nil {
		t.Fatal("directory accepted")
	}
	large := filepath.Join(t.TempDir(), "large")
	f, _ := os.Create(large)
	_ = f.Truncate(maxInputFile + 1)
	f.Close()
	if _, err = s.prepareInput(api.Submission{}, []api.InputFile{{Name: "large", Path: large}}, nil); err == nil {
		t.Fatal("oversize input accepted")
	}
	minimum := float64(1)
	schema := &formSchema{Type: "object", Required: []string{"count"}, Properties: map[string]formField{"count": {Type: "integer", Minimum: &minimum}, "name": {Type: "string", Enum: []string{"allowed"}}}}
	for _, answers := range []map[string][]string{{}, {"count": {"0"}}, {"count": {"1.5"}}, {"count": {"1"}, "name": {"not allowed"}}} {
		if _, err := formAnswers(schema, answers); err == nil {
			t.Fatal("invalid form accepted")
		}
	}
	if _, err := formAnswers(schema, map[string][]string{"count": {"2"}}); err != nil {
		t.Fatal(err)
	}
	if safeWebURL("javascript:alert(1)") || safeWebURL("file:///private/file") {
		t.Fatal("unsafe URL accepted")
	}
}
func TestCorruptBindingNeverCreatesReplacementConversation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binding.json")
	_ = os.WriteFile(path, []byte("not json"), 0600)
	s := NewSession(SessionOptions{Directory: t.TempDir(), StateFile: path})
	defer s.cancelLife()
	s.start = func(context.Context, Options) (*Client, error) {
		t.Fatal("started despite corrupt binding")
		return nil, errors.New("invalid")
	}
	if s.Connect(testContext(t)) == nil {
		t.Fatal("corrupt binding hidden")
	}
}

func TestUnknownLiveSendIsObservedWithoutRestartingOwner(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "live-submit")
	s.mu.Lock()
	s.binding.Pending = &pendingSubmission{ID: "late-proof"}
	s.state.Phase = "unknown"
	s.update()
	s.mu.Unlock()
	f.mu.Lock()
	f.history = []nativeTurn{{ID: "run-native", Status: "completed", Items: []nativeItem{{ID: "native-user", Type: "userMessage", ClientID: "late-proof"}}}}
	f.mu.Unlock()
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if view := s.Snapshot(); !view.CanSend || view.LastReceipt.Outcome != "accepted" {
		t.Fatal(view.Phase, view.LastReceipt)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connections != 1 || f.starts != 1 {
		t.Fatal("live owner replaced or input replayed")
	}
}
func TestLoginCompletionBeforeReplyDoesNotRemainPending(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.mu.Lock()
	s.state.Connection = "login"
	revision := s.state.Revision
	s.update()
	s.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := s.Login(testContext(t)); done <- err }()
	awaitState(t, s, func(v api.Snapshot) bool { return v.Revision > revision+1 })
	close(f.loginReply)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "ready" && !v.LoginPending })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts != 0 {
		t.Fatal("login recovery replayed work")
	}
}
func TestNonFiniteElicitationAndUnsupportedConstraintsAreNotAccepted(t *testing.T) {
	for _, number := range []string{"NaN", "+Inf", "-Inf"} {
		if _, err := formAnswers(&formSchema{Type: "object", Properties: map[string]formField{"n": {Type: "number"}}}, map[string][]string{"n": {number}}); err == nil {
			t.Fatal(number)
		}
	}
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "form-run")
	f.emit(wireMessage{ID: raw("form-native"), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{"threadId": "thread-native", "mode": "form", "serverName": "fixture", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"email": map[string]any{"type": "string", "format": "email"}}}})})
	view := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) > 0 })
	for _, choice := range view.Approvals[0].Choices {
		if choice.ID == "accept" {
			t.Fatal("constraint silently dropped")
		}
	}
}

func TestLateOlderTerminalCannotCompleteCurrentTurn(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	defer s.cancelLife()
	s.applyTurn(nativeTurn{ID: "older", Status: "completed"}, false)
	s.applyTurn(nativeTurn{ID: "current", Status: "inProgress"}, false)
	s.applyTurn(nativeTurn{ID: "older", Status: "completed"}, false)
	if s.run != "current" || s.state.Phase != "working" {
		t.Fatal("old terminal changed active turn")
	}
}

func TestKnownLocalArtifactHasReadableTextAndOpaqueAction(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(SessionOptions{Directory: dir})
	defer s.cancelLife()
	path := filepath.Join(dir, "result with spaces.txt")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	s.applyItem("run", nativeItem{ID: "answer", Type: "agentMessage", Text: "Done: [result](<" + path + ">)"}, true)
	view := s.Snapshot().Items[0]
	if view.Text != "Done: result" || len(view.Artifacts) != 1 {
		t.Fatal(view)
	}
	if actual, err := s.Artifact(view.Artifacts[0].ID); err != nil || actual != path {
		t.Fatal(actual, err)
	}
}

func TestReadSnapshotCannotOverwriteAlreadyStreamedText(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "first-live-input")
	s.mu.Lock()
	s.applyItem("run-native", nativeItem{ID: "streaming", Type: "agentMessage", Text: "new streamed text"}, false)
	s.binding.Pending = &pendingSubmission{ID: "late-steer"}
	s.state.Phase = "unknown"
	s.update()
	s.mu.Unlock()
	f.mu.Lock()
	f.history = []nativeTurn{{ID: "run-native", Status: "inProgress", Items: []nativeItem{{ID: "steered", Type: "userMessage", ClientID: "late-steer"}, {ID: "streaming", Type: "agentMessage", Text: "old snapshot"}}}}
	f.mu.Unlock()
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	view := s.Snapshot()
	if !view.CanSteer {
		t.Fatal("native receipt not reconciled")
	}
	for _, item := range view.Items {
		if item.Kind == "assistant" && item.Text != "new streamed text" {
			t.Fatal("snapshot overwrote live stream")
		}
	}
}
func TestInterruptedTurnDoesNotLeaveUnfinishedToolAsRunning(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	defer s.cancelLife()
	s.applyTurn(nativeTurn{ID: "run", Status: "inProgress"}, false)
	s.applyItem("run", nativeItem{ID: "tool", Type: "commandExecution", Status: "inProgress"}, false)
	s.applyTurn(nativeTurn{ID: "run", Status: "interrupted"}, false)
	if view := s.Snapshot(); view.Phase != "interrupted" || view.Items[0].Status != "unconfirmed" {
		t.Fatal(view.Phase, view.Items)
	}
	s.applyItem("run", nativeItem{ID: "tool", Type: "commandExecution", Status: "completed"}, true)
	if s.Snapshot().Items[0].Status != "completed" {
		t.Fatal("late native receipt lost")
	}
}

func TestFileApprovalCarriesNativeDiffAndDenial(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "file-approval")
	f.emit(wireMessage{Method: "item/started", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "item": nativeItem{ID: "file-native", Type: "fileChange", Changes: []nativeChange{{Path: "/fixture/result.txt", Diff: "+synthetic"}}}})})
	f.emit(wireMessage{ID: raw(88), Method: "item/fileChange/requestApproval", Params: raw(map[string]string{"threadId": "thread-native", "turnId": "run-native", "itemId": "file-native"})})
	v := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	if !strings.Contains(v.Approvals[0].Details, "/fixture/result.txt") || !strings.Contains(v.Approvals[0].Details, "+synthetic") {
		t.Fatal("file approval context omitted")
	}
	if err := s.Decide(testContext(t), api.Decision{ID: v.Approvals[0].ID, Choice: "decline"}); err != nil {
		t.Fatal(err)
	}
	response := <-f.answers
	if string(response.ID) != "88" || string(response.Result) != `{"decision":"decline"}` {
		t.Fatal("denial misrouted")
	}
}
