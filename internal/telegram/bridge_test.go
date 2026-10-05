package telegram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tg "github.com/mymmrac/telego"
)

type fakeClient struct {
	mu                               sync.Mutex
	sends, edits, documents, answers int
	texts                            []string
	documentBytes                    []string
	sendErr                          error
	webhook                          bool
	takeovers                        int
	updates                          chan []tg.Update
	download                         []byte
}

func (f *fakeClient) Me(context.Context) (*tg.User, error) {
	return &tg.User{ID: 123, Username: "test_bot"}, nil
}
func (f *fakeClient) Webhook(context.Context) (bool, error) { return f.webhook, nil }
func (f *fakeClient) TakeOver(context.Context) error        { f.takeovers++; return nil }
func (f *fakeClient) Updates(ctx context.Context, _ int) ([]tg.Update, error) {
	select {
	case v := <-f.updates:
		return v, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (f *fakeClient) Send(_ context.Context, _ int64, text string, _ *tg.InlineKeyboardMarkup) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends++
	f.texts = append(f.texts, text)
	return f.sends, f.sendErr
}
func (f *fakeClient) Edit(_ context.Context, _ int64, _ int, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits++
	f.texts = append(f.texts, text)
	return nil
}
func (f *fakeClient) Document(_ context.Context, _ int64, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.documents++
	f.documentBytes = append(f.documentBytes, string(data))
	return nil
}
func (f *fakeClient) Download(_ context.Context, _ string, path string) error {
	return os.WriteFile(path, f.download, 0600)
}
func (f *fakeClient) Answer(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers++
	return nil
}
func (f *fakeClient) Commands(context.Context) error { return nil }
func testBridge(t *testing.T, h Host) (*Bridge, *fakeClient) {
	t.Helper()
	if h.Snapshot == nil {
		h.Snapshot = func() api.Snapshot { return api.Snapshot{Connection: "connected"} }
	}
	b, e := Open(t.TempDir(), h)
	if e != nil {
		t.Fatal(e)
	}
	f := &fakeClient{updates: make(chan []tg.Update, 10), download: []byte("attachment fixture")}
	b.newClient = func(string) (client, error) { return f, nil }
	b.saveSecret = func(string, string) error { return nil }
	b.loadSecret = func(string) (string, error) { return "fixture-token", nil }
	b.deleteSecret = func(string) error { return nil }
	t.Cleanup(b.Close)
	return b, f
}
func message(id int, chat, user int64, text string) tg.Update {
	return tg.Update{UpdateID: id, Message: &tg.Message{MessageID: id, Chat: tg.Chat{ID: chat, Type: "private"}, From: &tg.User{ID: user, FirstName: "Owner"}, Text: text}}
}
func paired(b *Bridge) {
	b.state.ChatID, b.state.UserID, b.state.BotID, b.state.Enabled = 10, 20, 123, true
}
func TestPairingRequiresNoncePrivateChatAndDesktopConfirmation(t *testing.T) {
	b, f := testBridge(t, Host{})
	b.state.Bot = "test_bot"
	b.nonce = "secret-nonce"
	b.expires = time.Now().Add(time.Minute)
	b.input(t.Context(), f, message(1, 10, 20, "/start wrong"))
	if b.Status().Candidate != "" {
		t.Fatal("wrong nonce accepted")
	}
	group := message(2, 10, 20, "/start secret-nonce")
	group.Message.Chat.Type = "group"
	b.input(t.Context(), f, group)
	if b.Status().Candidate != "" {
		t.Fatal("group accepted")
	}
	b.input(t.Context(), f, message(3, 10, 20, "/start secret-nonce"))
	if b.Status().Paired || b.Status().Candidate == "" {
		t.Fatal("pairing did not wait for desktop")
	}
	b.input(t.Context(), f, message(4, 99, 88, "/start secret-nonce"))
	if b.candidate.From.ID != 20 {
		t.Fatal("candidate was replaced")
	}
	s, e := b.Confirm()
	if e != nil || !s.Paired || b.state.UserID != 20 {
		t.Fatal("confirm failed", e)
	}
	if _, e = b.Confirm(); e == nil {
		t.Fatal("consumed nonce accepted")
	}
}
func TestOwnerInputUsesOriginalIDAndNeverReplaysUnknownAfterRestart(t *testing.T) {
	calls := 0
	var got api.Submission
	h := Host{Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		calls++
		got = in
		return api.Receipt{ID: in.ID, Outcome: "unknown"}, errors.New("lost response")
	}}
	b, f := testBridge(t, h)
	paired(b)
	b.input(t.Context(), f, message(1, 10, 999, "ignored"))
	if calls != 0 {
		t.Fatal("unpaired sender reached Bot")
	}
	u := message(2, 10, 20, "original input")
	b.input(t.Context(), f, u)
	b.input(t.Context(), f, u)
	if calls != 1 || got.ID != "telegram:123:2" || got.Text != "original input" {
		t.Fatal("input identity or deduplication failed")
	}
	restored, e := Open(filepath.Dir(b.path), h)
	if e != nil {
		t.Fatal(e)
	}
	restored.input(t.Context(), f, u)
	if calls != 1 {
		t.Fatal("uncertain input replayed after restart")
	}
	data, _ := os.ReadFile(b.path)
	if strings.Contains(string(data), "original input") || strings.Contains(string(data), "fixture-token") {
		t.Fatal("private text or token persisted in bridge metadata")
	}
}
func TestStreamingEditsOneMessageAndChunksUnicode(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{{ID: "a", Kind: "assistant", Text: "first"}}})
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{{ID: "a", Kind: "assistant", Text: "first and final"}}})
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{{ID: "a", Kind: "assistant", Text: "first and final"}}})
	if f.sends != 1 || f.edits != 1 {
		t.Fatal("stream did not coalesce on stable message", f.sends, f.edits)
	}
	text := strings.Repeat("🦉你好", 1500)
	parts := splitText(text)
	if strings.Join(parts, "") != text {
		t.Fatal("text lost")
	}
	for _, p := range parts {
		if len(utf16.Encode([]rune(p))) > 4000 {
			t.Fatal("UTF16 Telegram limit exceeded")
		}
	}
}
func TestUnknownTelegramCreateIsNotRetried(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	f.sendErr = &transportError{issue: "network"}
	b.sendText(t.Context(), f, "item:a", 10, "text", nil)
	b.sendText(t.Context(), f, "item:a", 10, "more", nil)
	restored, e := Open(filepath.Dir(b.path), b.host)
	if e != nil {
		t.Fatal(e)
	}
	restored.sendText(t.Context(), f, "item:a", 10, "more", nil)
	if f.sends != 1 || b.Status().Issue != "delivery_uncertain" {
		t.Fatal("unknown send duplicated")
	}
}
func TestAttachmentDownloadIsBoundedAndFilenameCannotEscape(t *testing.T) {
	for _, name := range []string{".", "..", "../..", "\\..", "\x00"} {
		if !filepath.IsLocal(safeName(name)) || safeName(name) == "." {
			t.Fatalf("unsafe attachment filename %q", name)
		}
	}
	var input api.InputFile
	var inputBytes []byte
	b, f := testBridge(t, Host{Submit: func(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		input = files[0]
		inputBytes, _ = os.ReadFile(input.Path)
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	}})
	paired(b)
	u := message(1, 10, 20, "")
	u.Message.Document = &tg.Document{FileID: "file", FileName: "../../fixture.txt", FileSize: 12}
	b.input(t.Context(), f, u)
	if input.Name != "fixture.txt" || !strings.HasPrefix(input.Path, b.root+string(os.PathSeparator)) {
		t.Fatal("untrusted filename escaped storage")
	}
	if string(inputBytes) != "attachment fixture" {
		t.Fatal("attachment bytes lost")
	}
	u = message(2, 10, 20, "")
	u.Message.Document = &tg.Document{FileID: "large", FileName: "big.txt", FileSize: maxInputBytes + 1}
	if !b.input(t.Context(), f, u) || b.state.Inputs["telegram:123:2"] != "rejected" {
		t.Fatal("oversized attachment accepted")
	}
}
func TestApprovalUsesNativeChoiceAndStaleButtonDoesNotApprove(t *testing.T) {
	var decision api.Decision
	calls := 0
	snapshot := api.Snapshot{Approvals: []api.Approval{{ID: "native-request", Status: "pending", Choices: []api.Choice{{ID: "once", Label: "Allow once"}}}}}
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot { return snapshot }, Decide: func(_ context.Context, d api.Decision) error { calls++; decision = d; return nil }})
	paired(b)
	q := &tg.CallbackQuery{ID: "cb", From: tg.User{ID: 20}, Message: &tg.Message{Chat: tg.Chat{ID: 10}}, Data: callbackID("native-request", "once")}
	b.callback(t.Context(), f, q)
	b.callback(t.Context(), f, q)
	if calls != 1 || decision.ID != "native-request" || decision.Choice != "once" {
		t.Fatal("native choice or dedupe lost")
	}
	snapshot.Approvals[0].Status = "resolved"
	q.ID = "cb2"
	b.callback(t.Context(), f, q)
	if calls != 1 {
		t.Fatal("stale button approved")
	}
	snapshot.Approvals = nil
	snapshot.Reviews = []api.Review{{ID: "review", Status: "rejected"}}
	b.mirror(t.Context(), f, snapshot)
	if f.sends != 0 {
		t.Fatal("automatic review exposed as approval")
	}
}
func TestExistingWebhookRequiresExplicitTakeoverAndCloseCancelsPolling(t *testing.T) {
	b, f := testBridge(t, Host{})
	f.webhook = true
	if _, e := b.Connect(t.Context(), "fixture-token", false); e == nil || f.takeovers != 0 {
		t.Fatal("silent webhook takeover")
	}
	if _, e := b.Connect(t.Context(), "fixture-token", true); e != nil || f.takeovers != 1 {
		t.Fatal("explicit takeover failed", e)
	}
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("polling did not cancel on quit")
	}
}
func TestDesktopFileCopiedBeforeComposerConsumesOriginal(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	source := filepath.Join(t.TempDir(), "example.txt")
	os.WriteFile(source, []byte("original bytes"), 0600)
	b.Accepted(api.Submission{ID: "desktop-1", Text: "input"}, []api.InputFile{{Name: "example.txt", Path: source}}, api.Receipt{Outcome: "accepted"})
	os.Remove(source)
	var path string
	for _, relative := range b.state.PendingFiles {
		path = filepath.Join(b.root, relative)
	}
	data, e := os.ReadFile(path)
	if e != nil || string(data) != "original bytes" {
		t.Fatal("copy was too late")
	}
	b.sendFile(t.Context(), f, "upload", path)
	b.sendFile(t.Context(), f, "upload", path)
	if f.documents != 1 {
		t.Fatal("attachment duplicated")
	}
}

func TestReadyBackendLoopMirrorsNewOutputAndFailure(t *testing.T) {
	var mu sync.Mutex
	view := api.Snapshot{Connection: "ready", Items: []api.Item{{ID: "old", Kind: "assistant", Text: "private old history"}}}
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot { mu.Lock(); defer mu.Unlock(); return view }})
	paired(b)
	b.launch(f)
	await := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("bridge loop did not deliver")
	}
	// Wait until startup has recorded its history boundary, then publish new output.
	await(func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.state.Messages["item:old"].Skip })
	mu.Lock()
	view.Items = append(view.Items, api.Item{ID: "new", Kind: "assistant", Text: "stream begin"})
	mu.Unlock()
	await(func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.sends == 1 })
	mu.Lock()
	view.Items[1].Text = "stream final"
	view.Phase = "failed"
	view.LastReceipt = api.Receipt{ID: "original", Outcome: "accepted"}
	mu.Unlock()
	await(func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.edits == 1 && f.sends == 2 })
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, text := range f.texts {
		if strings.Contains(text, "private old history") {
			t.Fatal("earlier history leaked")
		}
	}
}
func TestRateLimitHonorsServerDelayAndRetriesOnlyConfirmedRejection(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	f.sendErr = &transportError{issue: "rate_limited", code: 429, retry: 60}
	b.sendText(t.Context(), f, "new", 10, "stream", nil)
	b.sendText(t.Context(), f, "new", 10, "stream", nil)
	if f.sends != 1 || !b.backingOff() {
		t.Fatal("ignored Telegram retry_after")
	}
	b.mu.Lock()
	b.retryUntil = time.Time{}
	b.mu.Unlock()
	f.sendErr = nil
	b.sendText(t.Context(), f, "new", 10, "stream", nil)
	if f.sends != 2 {
		t.Fatal("known unaccepted send could not resume")
	}
}

func TestOwnedArtifactsExportOnceAndEarlierPaginationStaysPrivate(t *testing.T) {
	owned := filepath.Join(t.TempDir(), "result.txt")
	os.WriteFile(owned, []byte("result"), 0600)
	b, f := testBridge(t, Host{Artifact: func(id string) (string, error) {
		if id != "owned-result" {
			t.Fatal("unowned resolver input")
		}
		return owned, nil
	}})
	paired(b)
	b.state.Messages["item:known"] = delivery{Skip: true}
	s := api.Snapshot{Items: []api.Item{{ID: "earlier", Kind: "assistant", Text: "old private conversation"}, {ID: "known", Kind: "assistant", Text: "known"}, {ID: "new-file", Kind: "assistant", Text: "result", Artifacts: []api.Artifact{{ID: "owned-result", Name: "result.txt"}}}}}
	b.mirror(t.Context(), f, s)
	b.mirror(t.Context(), f, s)
	if f.sends != 1 || f.documents != 1 {
		t.Fatal("pagination leaked or artifact duplicated", f.sends, f.documents)
	}
}

func awaitBridge(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("bridge did not reach expected state")
}

func TestDelayedBackendRecoveryBaselinesBeforeQueuedInput(t *testing.T) {
	var mu sync.Mutex
	view := api.Snapshot{Connection: "connecting"}
	unready := make(chan struct{}, 1)
	b, f := testBridge(t, Host{
		Snapshot: func() api.Snapshot {
			mu.Lock()
			defer mu.Unlock()
			if !ready(view) {
				select {
				case unready <- struct{}{}:
				default:
				}
			}
			return view
		},
		Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
			mu.Lock()
			defer mu.Unlock()
			view.Items = append(view.Items, api.Item{ID: "fresh", Kind: "assistant", Text: "fresh reply"})
			return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
		},
	})
	paired(b)
	f.updates <- []tg.Update{message(1, 10, 20, "new work")}
	b.launch(f)
	select {
	case <-unready:
	case <-time.After(2 * time.Second):
		t.Fatal("startup did not inspect backend")
	}
	b.mu.Lock()
	acceptedBeforeRecovery := len(b.state.Inputs) != 0
	b.mu.Unlock()
	if acceptedBeforeRecovery {
		t.Fatal("input was dispatched before recovery boundary")
	}
	mu.Lock()
	view = api.Snapshot{Connection: "ready", Items: []api.Item{{ID: "old", Kind: "assistant", Text: "private recovered history"}}}
	mu.Unlock()
	// Submit publishes its reply immediately, before the first output tick.
	awaitBridge(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.sends == 1 })
	b.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.state.Messages["item:old"].Skip || b.state.Messages["item:fresh"].Skip || b.state.Inputs["telegram:123:1"] != "accepted" {
		t.Fatal("recovered history and fresh reply were not separated")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.texts) != 1 || f.texts[0] != "fresh reply" {
		t.Fatal("fresh reply missing or private history published")
	}
}

func TestDesktopSameNamedAttachmentsKeepSeparateBytesAndCleanup(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	files := make([]api.InputFile, 2)
	for index, data := range []string{"first report", "second report"} {
		path := filepath.Join(t.TempDir(), "report.txt")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		files[index] = api.InputFile{Name: "report.txt", Path: path}
	}
	b.Accepted(api.Submission{ID: "two-files"}, files, api.Receipt{Outcome: "accepted"})
	if len(b.state.PendingFiles) != 2 {
		t.Fatal("same-name attachments collapsed")
	}
	var staged []string
	for _, relative := range b.state.PendingFiles {
		staged = append(staged, filepath.Join(b.root, relative))
	}
	if staged[0] == staged[1] || filepath.Base(staged[0]) != "report.txt" || filepath.Base(staged[1]) != "report.txt" {
		t.Fatal("attachment identity or display name lost")
	}
	for _, file := range files {
		os.Remove(file.Path)
	}
	b.flushFiles(t.Context(), f)
	b.flushFiles(t.Context(), f)
	if f.documents != 2 || len(b.state.PendingFiles) != 0 {
		t.Fatal("attachments not delivered exactly once")
	}
	seen := map[string]bool{}
	for _, data := range f.documentBytes {
		seen[data] = true
	}
	if !seen["first report"] || !seen["second report"] {
		t.Fatal("same-name bytes overwritten")
	}
	for _, path := range staged {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("private upload copy was not cleaned")
		}
	}
}

type blockedDocumentClient struct {
	*fakeClient
	started, finished chan struct{}
}

func (f *blockedDocumentClient) Document(ctx context.Context, _ int64, _ string) error {
	close(f.started)
	defer close(f.finished)
	<-ctx.Done()
	return ctx.Err()
}

func TestBlockedUploadDoesNotBlockControlsAndCloseJoinsWorkers(t *testing.T) {
	stopped := make(chan struct{}, 1)
	decided := make(chan api.Decision, 1)
	b, f := testBridge(t, Host{
		Snapshot: func() api.Snapshot {
			return api.Snapshot{Connection: "ready", Approvals: []api.Approval{{ID: "pending", Status: "pending", Choices: []api.Choice{{ID: "once", Label: "Allow once"}}}}}
		},
		Interrupt: func(context.Context) error { stopped <- struct{}{}; return nil },
		Decide:    func(_ context.Context, d api.Decision) error { decided <- d; return nil },
	})
	paired(b)
	source := filepath.Join(t.TempDir(), "upload.txt")
	os.WriteFile(source, []byte("fixture"), 0600)
	b.Accepted(api.Submission{ID: "desktop-upload"}, []api.InputFile{{Name: "upload.txt", Path: source}}, api.Receipt{Outcome: "accepted"})
	blocked := &blockedDocumentClient{f, make(chan struct{}), make(chan struct{})}
	b.launch(blocked)
	select {
	case <-blocked.started:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not start")
	}
	f.updates <- []tg.Update{
		message(1, 10, 20, "/stop"), message(2, 10, 20, "/status"),
		{UpdateID: 3, CallbackQuery: &tg.CallbackQuery{ID: "choice", From: tg.User{ID: 20}, Message: &tg.Message{Chat: tg.Chat{ID: 10}}, Data: callbackID("pending", "once")}},
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("slow upload blocked interrupt")
	}
	select {
	case d := <-decided:
		if d.ID != "pending" || d.Choice != "once" {
			t.Fatal("approval identity lost")
		}
	case <-time.After(time.Second):
		t.Fatal("slow upload blocked approval")
	}
	awaitBridge(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.answers == 1 && f.sends == 2 })
	select {
	case <-blocked.finished:
		t.Fatal("upload finished before controls were handled")
	default:
	}
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel and join workers")
	}
	select {
	case <-blocked.finished:
	default:
		t.Fatal("Close returned while upload was still running")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.Offset != 4 || b.state.Inputs["telegram:123:1"] != "handled" || b.state.Messages["upload:desktop-upload:0"].IDs[0] != -1 {
		t.Fatal("control offset or uncertain upload receipt lost")
	}
}

func TestCloseCancelsWaitingForBackendRecovery(t *testing.T) {
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot { return api.Snapshot{Connection: "connecting"} }})
	b.launch(f)
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on backend recovery")
	}
}

func TestRemoteInputCannotEchoDuringConcurrentOutput(t *testing.T) {
	published := make(chan api.Submission, 1)
	release := make(chan struct{})
	b, f := testBridge(t, Host{Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		published <- in
		<-release
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	}})
	paired(b)
	done := make(chan struct{})
	go func() { defer close(done); b.input(t.Context(), f, message(1, 10, 20, "remote input")) }()
	in := <-published
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{{ID: "user", Kind: "user", RequestID: in.ID, Text: in.Text}, {ID: "reply", Kind: "assistant", Text: "reply while submit completes"}}})
	close(release)
	<-done
	if f.sends != 1 || len(f.texts) != 1 || f.texts[0] != "reply while submit completes" {
		t.Fatal("concurrent output echoed Telegram input")
	}
}
