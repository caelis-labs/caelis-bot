package productrpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

type fakeExecutionPort struct {
	mu               sync.Mutex
	scope            productmanagement.Scope
	calls            int
	outcome          string
	entered, release chan struct{}
	cancelled        bool
}

func (f *fakeExecutionPort) ExecutionSettings(context.Context, productmanagement.Scope) (productmanagement.ExecutionView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return productmanagement.ExecutionView{Scope: f.scope, Conversation: productmanagement.Selection{Model: "public-model", Effort: "high"}, Revision: strings.Repeat("a", 64), Models: []api.ModelOption{{Model: "public-model", Efforts: []string{"high"}}}}, nil
}
func (f *fakeExecutionPort) ChangeExecutionSettings(ctx context.Context, c productmanagement.ExecutionCommand) (productmanagement.ExecutionResult, error) {
	f.mu.Lock()
	f.calls++
	entered, release, outcome, scope := f.entered, f.release, f.outcome, f.scope
	f.mu.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			f.mu.Lock()
			f.cancelled = true
			f.mu.Unlock()
			return productmanagement.ExecutionResult{}, ctx.Err()
		}
	}
	return productmanagement.ExecutionResult{Scope: scope, ID: c.ID, Outcome: outcome, Code: map[string]string{"unknown": "native-operation-unresolved"}[outcome]}, nil
}
func executionFixture(t *testing.T) (*Server, *Client, *fakeExecutionPort, Options) {
	t.Helper()
	f := &fakeExecutionPort{outcome: "accepted"}
	opts := Options{NodeID: "node", BotID: "bot", Token: strings.Repeat("t", 64), JournalFile: filepath.Join(t.TempDir(), "receipts.json"), Execution: func(scope productmanagement.Scope) (productmanagement.ExecutionPort, error) {
		f.scope = scope
		return f, nil
	}}
	s, c := openExecutionFixture(t, opts)
	return s, c, f, opts
}
func openExecutionFixture(t *testing.T, opts Options) (*Server, *Client) {
	t.Helper()
	s, err := NewServer(&fakeProduct{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s)
	t.Cleanup(h.Close)
	c, err := NewClient(ClientOptions{URL: h.URL, ExpectedNode: opts.NodeID, ExpectedBot: opts.BotID, Token: opts.Token})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if _, err = c.Connect(bounded(t)); err != nil {
		t.Fatal(err)
	}
	return s, c
}
func executionIntent(id string) productmanagement.ExecutionCommand {
	return productmanagement.ExecutionCommand{ID: id, Target: "conversation", ExpectedRevision: strings.Repeat("a", 64), Selection: productmanagement.Selection{Model: "public-model", Effort: "high"}}
}
func TestExecutionTypedStdioWorksWithoutHostManagementAndStableReplay(t *testing.T) {
	s, c, f, opts := executionFixture(t)
	if !s.Identity().Capabilities.Execution || s.Identity().Capabilities.RuntimeManagement {
		t.Fatal(s.Identity())
	}
	h := httptest.NewServer(s)
	defer h.Close()
	left, right := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = ProxyStdio(ctx, right, right, h.URL, opts.Token) }()
	remote, err := NewStdioClient(StdioOptions{ExpectedNode: "node", ExpectedBot: "bot"}, left)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err = remote.Connect(bounded(t)); err != nil {
		t.Fatal(err)
	}
	caps, err := remote.ManagementCapabilities(bounded(t))
	if err != nil || !caps.Execution || caps.Configuration || caps.Installation {
		t.Fatal(caps, err)
	}
	view, err := remote.ExecutionSettings(bounded(t))
	if err != nil || view.Conversation.Model != "public-model" || view.Work != nil {
		t.Fatal(view, err)
	}
	result, err := remote.ChangeExecutionSettings(bounded(t), executionIntent("settings-stable"))
	if err != nil || result.Outcome != "accepted" {
		t.Fatal(result, err)
	}
	_, err = c.ChangeExecutionSettings(bounded(t), executionIntent("settings-stable"))
	if err != nil {
		t.Fatal(err)
	}
	s2, c2 := openExecutionFixture(t, opts)
	if s2.Identity().Generation == s.Identity().Generation {
		t.Fatal("restart kept generation")
	}
	retained, err := c2.ChangeExecutionSettings(bounded(t), executionIntent("settings-stable"))
	if err != nil || retained.Outcome != "accepted" || retained.Generation != result.Generation {
		t.Fatal("original receipt adopted new generation", retained, err)
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if calls != 1 {
		t.Fatal("settings replay dispatched", calls)
	}
	altered := executionIntent("settings-stable")
	altered.Selection.Effort = "low"
	if _, err = c2.ChangeExecutionSettings(bounded(t), altered); err == nil {
		t.Fatal("same ID changed intent")
	}
	b, _ := os.ReadFile(opts.JournalFile)
	if bytes.Contains(b, []byte("approvalMode")) || bytes.Contains(b, []byte("apiKey")) || bytes.Contains(b, []byte("public-model")) {
		t.Fatal("settings payload/secret in digest-only journal", string(b))
	}
}
func TestExecutionObservationCancelRetainsUnknownAndFencesAfterRestart(t *testing.T) {
	s, c, f, opts := executionFixture(t)
	f.outcome = "unknown"
	f.entered, f.release = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan productmanagement.ExecutionResult, 1)
	go func() { r, _ := c.ChangeExecutionSettings(ctx, executionIntent("settings-uncertain")); done <- r }()
	<-f.entered
	cancel()
	select {
	case r := <-done:
		if r.ID != "settings-uncertain" || r.Outcome != "unknown" {
			t.Fatal(r)
		}
	case <-bounded(t).Done():
		t.Fatal("canceled observer stuck")
	}
	close(f.release)
	s.commands.Lock()
	s.commands.Unlock()
	original, err := c.Receipt(bounded(t), "settings-uncertain")
	if err != nil || original.Outcome != "unknown" || original.Execution == nil {
		t.Fatal(original, err)
	}
	// Matching values and a new connection provide no original effect receipt.
	if _, err := c.ExecutionSettings(bounded(t)); err != nil {
		t.Fatal(err)
	}
	denied, err := c.ChangeExecutionSettings(bounded(t), executionIntent("settings-new"))
	if err != nil || denied.Outcome != "rejected" || denied.Code != "execution-outcome-unresolved" {
		t.Fatal(denied, err)
	}
	_, next := openExecutionFixture(t, opts)
	if _, err := next.ExecutionSettings(bounded(t)); err != nil {
		t.Fatal(err)
	}
	denied, err = next.ChangeExecutionSettings(bounded(t), executionIntent("settings-after-restart"))
	if err != nil || denied.Outcome != "rejected" {
		t.Fatal(denied, err)
	}
	retained, err := next.ChangeExecutionSettings(bounded(t), executionIntent("settings-uncertain"))
	if err != nil || retained.Outcome != "unknown" || retained.Generation != original.Execution.Generation {
		t.Fatal(retained, err)
	}
	f.mu.Lock()
	calls, cancelled := f.calls, f.cancelled
	f.mu.Unlock()
	if calls != 1 || cancelled {
		t.Fatal("unknown retried or observer canceled native mutation", calls, cancelled)
	}
}
func TestExecutionCrashPendingAndReceiptPublicationFailureFence(t *testing.T) {
	for _, stage := range []string{"crash-before-receipt", "finish-write-failure"} {
		t.Run(stage, func(t *testing.T) {
			s, c, f, opts := executionFixture(t)
			if stage == "crash-before-receipt" {
				in := executionIntent("interrupted-original")
				in.Scope = managementScope(s.Identity().Scope)
				canonical := Command{Scope: Scope{BotID: in.BotID}, ID: in.ID, Kind: "configure-execution", Execution: &in}
				copy := in
				copy.Generation = ""
				canonical.Execution = &copy
				b, _ := json.Marshal(canonical)
				sum := sha256.Sum256(b)
				digest := hex.EncodeToString(sum[:])
				if _, _, err := s.journal.reserveCommand(in.ID, digest, nil, &in); err != nil {
					t.Fatal(err)
				}
			} else {
				original := s.journal.write
				n := 0
				s.journal.write = func(path string, doc journalDocument) error {
					n++
					if n == 2 {
						return errors.New("injected finish failure")
					}
					return original(path, doc)
				}
				r, err := c.ChangeExecutionSettings(bounded(t), executionIntent("interrupted-original"))
				if err != nil || r.Outcome != "unknown" {
					t.Fatal(r, err)
				}
			}
			_, next := openExecutionFixture(t, opts)
			rejected, err := next.ChangeExecutionSettings(bounded(t), executionIntent("fresh-after-crash"))
			if err != nil || rejected.Outcome != "rejected" || rejected.Code != "execution-outcome-unresolved" {
				t.Fatal(rejected, err)
			}
			r, err := next.Receipt(bounded(t), "interrupted-original")
			if err != nil || r.Outcome != "unknown" || r.Execution == nil || r.Code == "pending" {
				t.Fatal(r, err)
			}
			f.mu.Lock()
			calls := f.calls
			f.mu.Unlock()
			expected := 0
			if stage == "finish-write-failure" {
				expected = 1
			}
			if calls != expected {
				t.Fatal("reopened pending dispatched", calls)
			}
		})
	}
}

func TestExecutionClosedPayloadStaleScopeAndUnavailableCapability(t *testing.T) {
	s, c, f, _ := executionFixture(t)
	scope := s.Identity().Scope
	in := executionIntent("stale-settings")
	in.Scope = managementScope(scope)
	in.Generation = "old-generation"
	var result Result
	if err := c.request(bounded(t), "POST", "/v1/commands", Command{Scope: scope, ID: in.ID, Kind: "configure-execution", Execution: &in}, &result); err == nil {
		t.Fatal("stale nested scope admitted")
	}
	payload := map[string]any{"botId": scope.BotID, "generation": scope.Generation, "id": "secret-injection", "kind": "configure-execution", "execution": map[string]any{"botId": scope.BotID, "generation": scope.Generation, "id": "secret-injection", "target": "conversation", "expectedRevision": strings.Repeat("a", 64), "selection": map[string]any{"model": "public-model", "effort": "high", "approvalMode": "full-access", "apiKey": "synthetic-secret"}}}
	if err := c.request(bounded(t), "POST", "/v1/commands", payload, &result); err == nil {
		t.Fatal("unknown secret/policy fields accepted")
	}
	if _, ok := s.journal.lookup("secret-injection"); ok {
		t.Fatal("closed payload journaled")
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if calls != 0 {
		t.Fatal("invalid wire reached provider")
	}
	opts := Options{NodeID: "other-node", BotID: "other-bot", Token: strings.Repeat("z", 64), JournalFile: filepath.Join(t.TempDir(), "receipts.json"), Capabilities: Capabilities{Execution: true}}
	absent, remote := openExecutionFixture(t, opts)
	caps, err := remote.ManagementCapabilities(bounded(t))
	if err != nil || caps.Execution || absent.Identity().Capabilities.Execution {
		t.Fatal("unbound capability trusted", caps, err)
	}
	if _, err = remote.ExecutionSettings(bounded(t)); err == nil {
		t.Fatal("unbound settings exposed")
	}
	rejected, err := remote.ChangeExecutionSettings(bounded(t), executionIntent("unavailable-settings"))
	if err != nil || rejected.Outcome != "rejected" || rejected.Code != "execution-unavailable" {
		t.Fatal(rejected, err)
	}
}

func TestExecutionQueuedFreshMutationCannotPassUnknownOriginal(t *testing.T) {
	s, c, f, _ := executionFixture(t)
	f.outcome = "unknown"
	f.entered, f.release = make(chan struct{}), make(chan struct{})
	reserved := make(chan struct{})
	write := s.journal.write
	s.journal.write = func(path string, doc journalDocument) error {
		err := write(path, doc)
		if entry, ok := doc.Entries["queued-settings"]; ok && entry.Result.Code == "pending" {
			select {
			case <-reserved:
			default:
				close(reserved)
			}
		}
		return err
	}
	first, second := make(chan productmanagement.ExecutionResult, 1), make(chan productmanagement.ExecutionResult, 1)
	go func() {
		r, _ := c.ChangeExecutionSettings(bounded(t), executionIntent("original-settings"))
		first <- r
	}()
	select {
	case <-f.entered:
	case <-bounded(t).Done():
		t.Fatal("first native settings not admitted")
	}
	go func() { r, _ := c.ChangeExecutionSettings(bounded(t), executionIntent("queued-settings")); second <- r }()
	select {
	case <-reserved:
	case <-bounded(t).Done():
		t.Fatal("second command never reserved")
	}
	close(f.release)
	select {
	case r := <-first:
		if r.Outcome != "unknown" {
			t.Fatal(r)
		}
	case <-bounded(t).Done():
		t.Fatal("original stalled")
	}
	select {
	case r := <-second:
		if r.Outcome != "rejected" || r.Code != "execution-outcome-unresolved" {
			t.Fatal(r)
		}
	case <-bounded(t).Done():
		t.Fatal("queued command stalled")
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if calls != 1 {
		t.Fatal("queued command escaped original receipt fence", calls)
	}
}
