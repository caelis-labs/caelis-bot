package productrpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type fakeProduct struct {
	mu                                    sync.Mutex
	snapshot                              api.Snapshot
	draft                                 api.Draft
	submits, decisions, stops, interrupts int
	lastDecision                          api.Decision
	entered, release                      chan struct{}
	cancelled                             bool
}

func (f *fakeProduct) Snapshot() api.Snapshot { f.mu.Lock(); defer f.mu.Unlock(); return f.snapshot }
func (*fakeProduct) BotInitialization() api.BotInitialization {
	return api.BotInitialization{Status: "accepted"}
}
func (f *fakeProduct) Draft() api.Draft { f.mu.Lock(); defer f.mu.Unlock(); return f.draft }
func (f *fakeProduct) SaveDraft(d api.Draft) (api.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d.Revision++
	f.draft = d
	return d, nil
}
func (f *fakeProduct) Submit(ctx context.Context, in api.Submission) (api.Receipt, error) {
	f.mu.Lock()
	f.submits++
	entered, release := f.entered, f.release
	f.mu.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			f.mu.Lock()
			f.cancelled = true
			f.mu.Unlock()
			return api.Receipt{ID: in.ID, Outcome: "unknown"}, ctx.Err()
		}
	}
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}
func (*fakeProduct) InitializeBot(context.Context, api.BotIntroduction) (api.BotInitialization, error) {
	return api.BotInitialization{Status: "accepted"}, nil
}
func (*fakeProduct) RetryBotIntroduction(context.Context) (api.BotInitialization, error) {
	return api.BotInitialization{Status: "accepted"}, nil
}
func (f *fakeProduct) Decide(_ context.Context, d api.Decision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decisions++
	f.lastDecision = d
	return nil
}
func (f *fakeProduct) InterruptTurn(_ context.Context, expected string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshot.CurrentTurn != expected {
		return ErrUnsupported
	}
	f.interrupts++
	return nil
}
func (*fakeProduct) LoadEarlier(context.Context) error { return nil }
func (f *fakeProduct) StopBot(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
}

func fixture(t *testing.T) (*Server, *Client, *fakeProduct, *httptest.Server, Options) {
	t.Helper()
	f := &fakeProduct{snapshot: api.Snapshot{CurrentTurn: "native-turn", Approvals: []api.Approval{{ID: "native-approval", Target: "command-a", Status: "pending", Choices: []api.Choice{{ID: "allow", Scope: "turn"}}}}, Items: []api.Item{{ID: "native-item", TurnKey: "native-turn", Artifacts: []api.Artifact{{ID: "native-artifact", Name: "result.txt"}}}}, References: []api.Reference{{ID: "native-reference", Name: "guide"}}}}
	opts := Options{NodeID: "node-fixture", BotID: "bot-fixture", Token: strings.Repeat("x", 64), JournalFile: filepath.Join(t.TempDir(), "product", "receipts.json"), Capabilities: Capabilities{Interrupt: true}}
	s, err := NewServer(f, opts)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s)
	t.Cleanup(h.Close)
	c, err := NewClient(ClientOptions{URL: h.URL, Token: opts.Token, ExpectedNode: opts.NodeID, ExpectedBot: opts.BotID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if _, err := c.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s, c, f, h, opts
}

func bounded(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestProjectionAndExactApprovalRejectChangedTarget(t *testing.T) {
	_, c, f, _, _ := fixture(t)
	state, err := c.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(state)
	for _, native := range []string{"native-turn", "native-approval", "native-item", "native-artifact", "native-reference"} {
		if bytes.Contains(b, []byte(native)) {
			t.Fatalf("native binding leaked: %s", native)
		}
	}
	approval := state.Snapshot.Approvals[0]
	f.mu.Lock()
	f.snapshot.Approvals[0].Target = "command-b"
	f.mu.Unlock()
	r, err := c.Command(t.Context(), Command{ID: "decision-stale", Kind: "decide", Decision: &ApprovalDecision{Target: state.ApprovalTargets[approval.ID], Decision: api.Decision{ID: approval.ID, Choice: "allow"}}})
	if err != nil || r.Outcome != "rejected" || r.Code != "stale-approval" {
		t.Fatal(r, err)
	}
	state, err = c.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	approval = state.Snapshot.Approvals[0]
	r, err = c.Command(t.Context(), Command{ID: "decision-live", Kind: "decide", Decision: &ApprovalDecision{Target: state.ApprovalTargets[approval.ID], Decision: api.Decision{ID: approval.ID, Choice: "allow"}}})
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.decisions != 1 || f.lastDecision.ID != "native-approval" || f.lastDecision.Choice != "allow" {
		t.Fatal("exact native decision lost", f.lastDecision)
	}
}

func TestLostResponseAndClientDetachRetainOriginalReceipt(t *testing.T) {
	s, c, f, h, opts := fixture(t)
	f.entered, f.release = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(bounded(t))
	defer cancel()
	done := make(chan Result, 1)
	go func() {
		r, _ := c.Command(ctx, Command{ID: "original-request", Kind: "submit", Submission: &api.Submission{ID: "original-request", Text: "private fixture prompt"}})
		done <- r
	}()
	select {
	case <-f.entered:
	case <-bounded(t).Done():
		t.Fatal("command not dispatched")
	}
	c.Close()
	select {
	case r := <-done:
		if r.ID != "original-request" || r.Outcome != "unknown" {
			t.Fatal(r)
		}
	case <-bounded(t).Done():
		t.Fatal("client did not detach")
	}
	s.stateMu.Lock()
	changed := s.changed
	s.stateMu.Unlock()
	close(f.release)
	select {
	case <-changed:
	case <-bounded(t).Done():
		t.Fatal("admitted command did not finish after detach")
	}
	c2, err := NewClient(ClientOptions{URL: h.URL, Token: opts.Token, ExpectedNode: opts.NodeID, ExpectedBot: opts.BotID})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if _, err := c2.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, err := c2.Receipt(t.Context(), "original-request")
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	r, err = c2.Command(t.Context(), Command{ID: "original-request", Kind: "submit", Submission: &api.Submission{ID: "original-request", Text: "private fixture prompt"}})
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.submits != 1 || f.stops != 0 || f.cancelled {
		t.Fatalf("dispatches=%d stops=%d cancellation=%v", f.submits, f.stops, f.cancelled)
	}
}

func TestRestartChangesGenerationAndNeverReplaysUnknownIntent(t *testing.T) {
	s, c, f, _, opts := fixture(t)
	state, err := c.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	command := Command{Scope: s.identity.Scope, ID: "crashed-request", Kind: "submit", Submission: &api.Submission{ID: "crashed-request", Text: "not a replay authorization"}}
	canonical := command
	canonical.Generation = ""
	b, _ := json.Marshal(canonical)
	digest := sha256.Sum256(b)
	if _, fresh, err := s.journal.reserve(command.ID, hex.EncodeToString(digest[:])); err != nil || !fresh {
		t.Fatal(fresh, err)
	}
	s2, err := NewServer(f, opts)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s2)
	defer h.Close()
	c2, err := NewClient(ClientOptions{URL: h.URL, Token: opts.Token, ExpectedNode: opts.NodeID, ExpectedBot: opts.BotID})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if _, err := c2.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	view, err := c2.Watch(t.Context(), state.Cursor)
	if err != nil || !view.Reset || view.Generation == state.Generation {
		t.Fatal(view, err)
	}
	r, err := c2.Command(t.Context(), command)
	if err != nil || r.Outcome != "unknown" {
		t.Fatal(r, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.submits != 0 {
		t.Fatal("crashed intent replayed")
	}
}

func TestExplicitStopAndStaleInterruptRemainDistinctFromDetach(t *testing.T) {
	_, c, f, _, _ := fixture(t)
	state, err := c.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.snapshot.CurrentTurn = "new-native-turn"
	f.mu.Unlock()
	r, err := c.Command(t.Context(), Command{ID: "stale-cancel", Kind: "interrupt", Turn: state.Snapshot.CurrentTurn})
	if err != nil || r.Outcome != "rejected" || r.Code != "stale-turn" {
		t.Fatal(r, err)
	}
	r, err = c.Command(t.Context(), Command{ID: "stop-bot", Kind: "stop-bot"})
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	c.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.interrupts != 0 || f.stops != 1 {
		t.Fatalf("interrupts=%d stops=%d", f.interrupts, f.stops)
	}
}

func TestSchemaAndAuthenticationFailBeforeDispatch(t *testing.T) {
	s, _, f, _, opts := fixture(t)
	for _, tc := range []struct {
		name, body, token, origin string
		status                    int
	}{
		{"unauthorized", `{}`, "wrong", "", 401},
		{"browser", `{}`, opts.Token, "http://hostile.invalid", 401},
		{"unknown-field", `{"botId":"bot-fixture","generation":"` + s.identity.Generation + `","id":"a","kind":"submit","apiKey":"private"}`, opts.Token, "", 400},
		{"unknown-command", `{"botId":"bot-fixture","generation":"` + s.identity.Generation + `","id":"a","kind":"execute-shell"}`, opts.Token, "", 400},
		{"large-body", strings.Repeat(" ", MaxCommandBytes+1), opts.Token, "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/commands", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer "+tc.token)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.submits != 0 || f.stops != 0 {
		t.Fatal("invalid request dispatched")
	}
}

type memoryResources struct {
	meta  Resource
	data  []byte
	calls int
}

func (m *memoryResources) Upload(_ context.Context, r Resource, reader io.Reader) (Resource, error) {
	m.calls++
	m.data, _ = io.ReadAll(reader)
	r.ID = "uploaded-private-handle"
	m.meta = r
	return r, nil
}
func (m *memoryResources) Open(_ context.Context, id string) (Resource, io.ReadCloser, error) {
	if id != m.meta.ID {
		return Resource{}, nil, ErrUnsupported
	}
	return m.meta, io.NopCloser(bytes.NewReader(m.data)), nil
}

func TestResourcesRequireDigestAndNeverExportPath(t *testing.T) {
	s, c, _, _, _ := fixture(t)
	resources := &memoryResources{}
	s.opts.Resources = resources
	s.identity.Capabilities.Files = true
	meta, err := c.Upload(t.Context(), "notes.txt", []byte("private synthetic bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID == resources.meta.ID {
		t.Fatal("native upload id exposed")
	}
	if _, _, err := c.Download(t.Context(), resources.meta.ID); err == nil {
		t.Fatal("raw native upload id accepted")
	}
	if _, _, err := c.Download(t.Context(), "native-artifact"); err == nil {
		t.Fatal("raw native artifact id accepted")
	}
	got, b, err := c.Download(t.Context(), meta.ID)
	if err != nil || got.SHA256 != meta.SHA256 || string(b) != "private synthetic bytes" {
		t.Fatal(got, err)
	}
	if _, err := c.Upload(t.Context(), "../notes.txt", nil); err == nil {
		t.Fatal("native path accepted")
	}
	r := httptest.NewRequest("PUT", "/v1/resources", strings.NewReader("corrupt"))
	r.Header.Set("Authorization", "Bearer "+s.opts.Token)
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("X-Product-Bot", s.identity.BotID)
	r.Header.Set("X-Product-Generation", s.identity.Generation)
	r.Header.Set("X-Resource-Name", base64.RawURLEncoding.EncodeToString([]byte("bad.txt")))
	r.Header.Set("X-Resource-Size", "7")
	r.Header.Set("X-Resource-SHA256", strings.Repeat("0", 64))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 || resources.calls != 1 {
		t.Fatal("corrupt upload reached store", w.Code, resources.calls)
	}
}

func TestReceiptsPersistNoDraftOrPrivateNativeMessage(t *testing.T) {
	s, c, _, _, opts := fixture(t)
	command := Command{ID: "private-submit", Kind: "submit", Submission: &api.Submission{ID: "private-submit", Text: "private user prompt"}}
	if r, err := c.Command(t.Context(), command); err != nil || r.Outcome != "accepted" {
		t.Fatalf("submit: %+v %v", r, err)
	}
	if err := s.journal.finish(command.ID, Result{ID: command.ID, Outcome: "accepted", Submission: &api.Receipt{ID: command.ID, Outcome: "accepted", Message: "private native message"}, Draft: &api.Draft{Text: "private draft"}, Initialization: &api.BotInitialization{Status: "private initialization"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(opts.JournalFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private user prompt", "private native message", "private draft", "private initialization"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatal("private payload persisted in receipt journal")
		}
	}
	r, err := c.Receipt(t.Context(), command.ID)
	if err != nil || r.Outcome != "accepted" || r.Draft != nil || r.Initialization != nil || r.Submission == nil || r.Submission.Message != "" {
		t.Fatalf("receipt: %+v %v", r, err)
	}
	command.Submission.Text = "different intent"
	if _, err := c.Command(t.Context(), command); err == nil {
		t.Fatal("conflicting stable id accepted")
	}
}

func TestDraftCASIsReconciledWithoutBusinessReceipt(t *testing.T) {
	s, c, f, _, _ := fixture(t)
	command := Command{ID: "draft-1", Kind: "save-draft", Draft: &api.Draft{Text: "first", Revision: 0}}
	r, err := c.Command(t.Context(), command)
	if err != nil || r.Outcome != "accepted" || r.Draft == nil || r.Draft.Revision != 1 {
		t.Fatalf("draft: %+v %v", r, err)
	}
	if _, ok := s.journal.lookup(command.ID); ok {
		t.Fatal("draft consumes business receipt")
	}
	command.Draft.Text = "stale replacement"
	r, err = c.Command(t.Context(), command)
	if err != nil || r.Outcome != "rejected" || r.Code != "stale-draft" || f.Draft().Text != "first" {
		t.Fatalf("stale draft: %+v %v", r, err)
	}
	state, err := c.State(t.Context())
	if err != nil || state.Draft.Text != "first" || state.Draft.Revision != 1 {
		t.Fatalf("reconcile draft: %+v %v", state.Draft, err)
	}
}

func TestClientDetachCancelsResourceObservationAndStopFencesUploads(t *testing.T) {
	s, c, _, _, _ := fixture(t)
	store := &memoryResources{}
	s.opts.Resources = store
	s.identity.Capabilities.Files = true
	if r, err := c.Command(t.Context(), Command{ID: "stop-files", Kind: "stop-bot"}); err != nil || r.Outcome != "accepted" {
		t.Fatalf("stop: %+v %v", r, err)
	}
	if _, err := c.Upload(t.Context(), "blocked.txt", []byte("blocked")); err == nil {
		t.Fatal("upload admitted after stop")
	}
	c.Close()
	if _, _, err := c.Download(t.Context(), "resource-1"); err == nil {
		t.Fatal("closed observer allowed resource request")
	}
}

func TestReservationPublicationFailureNeverDispatches(t *testing.T) {
	for _, afterRename := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-rename", true: "directory-sync"}[afterRename], func(t *testing.T) {
			s, c, f, _, opts := fixture(t)
			s.journal.write = func(path string, doc journalDocument) error {
				if afterRename {
					if err := durableJournalWrite(path, doc); err != nil {
						return err
					}
				}
				return errors.New("injected receipt publication failure")
			}
			command := Command{ID: "uncertain-publication", Kind: "submit", Submission: &api.Submission{ID: "uncertain-publication", Text: "synthetic"}}
			if _, err := c.Command(t.Context(), command); err == nil {
				t.Fatal("unconfirmed durability admitted")
			}
			if f.submits != 0 {
				t.Fatal("native dispatch preceded durable reservation")
			}
			if r, err := c.Command(t.Context(), command); err != nil || r.Outcome != "unknown" {
				t.Fatalf("same id became fresh: %+v %v", r, err)
			}
			command.ID = "another-publication"
			command.Submission.ID = command.ID
			if _, err := c.Command(t.Context(), command); err == nil {
				t.Fatal("fresh admission after durability failure")
			}
			command.ID = "uncertain-publication"
			command.Submission.ID = command.ID
			if f.submits != 0 {
				t.Fatal("publication retry dispatched native intent")
			}
			if afterRename {
				reopened, err := openJournal(opts.JournalFile, opts.BotID)
				if err != nil {
					t.Fatal(err)
				}
				r, ok := reopened.lookup(command.ID)
				if !ok || r.Outcome != "unknown" {
					t.Fatal("uncertain publication lost no-replay marker")
				}
			}
		})
	}
}
