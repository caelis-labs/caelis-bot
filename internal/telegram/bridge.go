package telegram

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/secretstore"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
	tg "github.com/mymmrac/telego"
)

type Host struct {
	Snapshot    func() api.Snapshot
	Diagnostics func(diagnosticlog.Record)
	Recovery    func() api.RecoveryState
	Recover     func(context.Context, string) error
	Submit      func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)
	Interrupt   func(context.Context) error
	Decide      func(context.Context, api.Decision) error
	Artifact    func(string) (string, error)
	ScreenImage func(string) ([]byte, error)
	Chinese     func() bool
	TextControl *textchannel.Store
	RecordInput func(textchannel.Inbound, bool)
}
type Status struct {
	Enabled   bool   `json:"enabled"`
	Bot       string `json:"bot"`
	Owner     string `json:"owner"`
	Paired    bool   `json:"paired"`
	Candidate string `json:"candidate"`
	PairURL   string `json:"pairURL"`
	Issue     string `json:"issue"`
}
type delivery struct {
	IDs          []int    `json:"ids,omitempty"` // -1 unknown create, -2 definitely rejected create.
	Hashes       []string `json:"hashes,omitempty"`
	Rejected     []string `json:"rejected,omitempty"` // Exact known-rejected edit; retry only if content/markup changes.
	Keyboard     string   `json:"keyboard,omitempty"`
	Closed       bool     `json:"closed,omitempty"`       // Native terminal/disappeared; old callback stays fenced.
	Terminal     string   `json:"terminal,omitempty"`     // Short terminal status, not native payload.
	ApprovalBody []string `json:"approvalBody,omitempty"` // Private, bounded original approval card parts for original-message edits.
	Skip         bool     `json:"skip,omitempty"`
}
type document struct {
	Version        int                 `json:"version"`
	Enabled        bool                `json:"enabled"`
	BotID          int64               `json:"botId"`
	Bot            string              `json:"bot"`
	ChatID         int64               `json:"chatId"`
	UserID         int64               `json:"userId"`
	Owner          string              `json:"owner"`
	Offset         int                 `json:"offset"`
	Ingress        []tg.Update         `json:"ingress,omitempty"`        // Durable receive queue, independent of Runtime dispatch.
	SecretPrompts  map[int]string      `json:"secretPrompts,omitempty"`  // secret values never enter the durable queue
	SecretMessages map[int]bool        `json:"secretMessages,omitempty"` // quoted secret messages never re-enter model input
	Inputs         map[string]string   `json:"inputs"`                   // Original stable request and native receipt outcome only.
	PendingFiles   map[string]string   `json:"pendingFiles,omitempty"`
	Messages       map[string]delivery `json:"messages"` // IDs and digests; approval cards also retain only their sent text.
}
type Bridge struct {
	configMu            sync.Mutex
	mu                  sync.Mutex
	path, root, account string
	host                Host
	state               document
	issue               string
	loadErr             error
	candidate           *tg.Message
	nonce               string
	expires             time.Time
	cancel              context.CancelFunc
	done                chan struct{}
	newClient           func(string) (client, error)
	secrets             secretstore.Store
	retryUntil          time.Time
	closed              bool
	baselineReady       bool
	noticeBaselineReady bool
	secretIngress       map[int]string
	recoveryNonce       string
	recoveryClaim       string
	recoveryWait        sync.WaitGroup
	typingPoll          time.Duration
	typingRenew         time.Duration
}

func Open(root string, host Host) (*Bridge, error) {
	b := &Bridge{path: filepath.Join(root, "telegram.json"), root: filepath.Join(root, "Telegram"), account: digest(root), host: host, newClient: newClient, recoveryNonce: rand.Text(), secretIngress: map[int]string{},
		secrets: &secretstore.FileStore{Root: filepath.Join(root, "Credentials"), Namespace: "telegram", Legacy: secretstore.Functions{LoadFunc: loadSecret, DeleteFunc: deleteSecret}}}
	b.state = document{Version: 1, Inputs: map[string]string{}, Messages: map[string]delivery{}}
	f, e := os.Open(b.path)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		b.loadErr = errors.New("telegram_state_unavailable")
	}
	if e == nil {
		decoder := json.NewDecoder(f)
		if decoder.Decode(&b.state) != nil || decoder.Decode(new(any)) != io.EOF || b.state.Version != 1 || b.state.Inputs == nil || b.state.Messages == nil {
			b.loadErr = errors.New("telegram_state_unavailable")
		}
		_ = f.Close()
	}
	if b.loadErr != nil {
		b.state = document{Version: 1, Inputs: map[string]string{}, Messages: map[string]delivery{}}
		b.issue = "storage"
	}
	if b.state.PendingFiles == nil {
		b.state.PendingFiles = map[string]string{}
	}
	if b.state.SecretPrompts == nil {
		b.state.SecretPrompts = map[int]string{}
	}
	if b.state.SecretMessages == nil {
		b.state.SecretMessages = map[int]bool{}
	}
	return b, b.loadErr
}
func digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func (b *Bridge) saveLocked() error {
	if b.loadErr != nil {
		return b.loadErr
	}
	if e := localstate.Write(b.path, b.state); e != nil {
		b.issue = "storage"
		return errors.New("storage")
	}
	if b.issue == "storage" {
		b.issue = ""
	}
	return nil
}
func (b *Bridge) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := Status{Enabled: b.state.Enabled, Bot: b.state.Bot, Owner: b.state.Owner, Paired: b.state.ChatID != 0, Issue: b.issue}
	if b.state.Enabled && b.state.ChatID == 0 && b.nonce != "" && time.Now().After(b.expires) {
		s.Issue = "pairing_expired"
	}
	if b.nonce != "" && time.Now().Before(b.expires) {
		s.PairURL = "https://t.me/" + b.state.Bot + "?start=" + b.nonce
		if b.candidate != nil {
			s.Candidate = ownerName(b.candidate.From)
		}
	}
	return s
}
func ownerName(u *tg.User) string {
	if u == nil {
		return ""
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if u.Username != "" {
		name += " (@" + u.Username + ")"
	}
	return name
}
func (b *Bridge) Start() {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	if b.loadErr != nil {
		_, exists := os.Stat(b.path)
		restored, err := Open(filepath.Dir(b.path), b.host)
		if exists == nil && err == nil {
			b.state, b.loadErr, b.issue = restored.state, nil, ""
		}
	}
	enabled := b.state.Enabled && !b.closed
	if enabled && b.done != nil {
		select {
		case <-b.done:
		default:
			enabled = false
		}
	}
	b.mu.Unlock()
	if enabled {
		b.resume()
	}
}
func (b *Bridge) resume() {
	token, e := b.secrets.Load(b.account)
	if e != nil {
		b.setIssue("credential")
		return
	}
	c, e := b.newClient(token)
	if e != nil {
		b.setIssue(issueOf(e))
		return
	}
	b.launch(c)
}
func (b *Bridge) stop() {
	b.mu.Lock()
	cancel, done := b.cancel, b.done
	b.cancel, b.done = nil, nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
func (b *Bridge) setIssue(issue string) { b.mu.Lock(); b.issue = issue; b.mu.Unlock() }

// Connect validates before replacing an existing configuration. Webhook takeover
// requires the explicit setup action; a conflicting poller is never displaced.
func (b *Bridge) Connect(ctx context.Context, token string, takeOver bool) (Status, error) {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return b.Status(), errors.New("closed")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		var e error
		token, e = b.secrets.Load(b.account)
		if e != nil {
			b.setIssue("credential")
			return b.Status(), errors.New("credential")
		}
	}
	c, e := b.newClient(token)
	if e != nil {
		b.setIssue(issueOf(e))
		return b.Status(), errors.New(issueOf(e))
	}
	me, e := c.Me(ctx)
	if e != nil {
		b.setIssue(issueOf(e))
		return b.Status(), errors.New(issueOf(e))
	}
	webhook, e := c.Webhook(ctx)
	if e != nil {
		b.setIssue(issueOf(e))
		return b.Status(), errors.New(issueOf(e))
	}
	if webhook && !takeOver {
		b.setIssue("webhook")
		return b.Status(), errors.New("webhook")
	}
	b.stop()
	if webhook {
		if e = c.TakeOver(ctx); e != nil {
			b.setIssue(issueOf(e))
			return b.Status(), errors.New(issueOf(e))
		}
	}
	if e = b.secrets.Save(b.account, token); e != nil {
		b.setIssue("credential")
		return b.Status(), errors.New("credential")
	}
	b.mu.Lock()
	if b.state.BotID != me.ID {
		b.state = document{Version: 1, Inputs: map[string]string{}, Messages: map[string]delivery{}}
		b.secretIngress = map[int]string{}
	}
	b.state.Enabled, b.state.BotID, b.state.Bot = true, me.ID, me.Username
	b.issue = ""
	e = b.saveLocked()
	b.mu.Unlock()
	if e != nil {
		return b.Status(), e
	}
	b.launch(c)
	return b.Status(), nil
}
func (b *Bridge) launch(c client) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.done = make(chan struct{})
	b.baselineReady = false
	b.recoveryNonce = rand.Text()
	b.recoveryClaim = ""
	done := b.done
	if b.state.ChatID == 0 {
		var data [18]byte
		_, e := rand.Read(data[:])
		if e != nil {
			b.issue = "pairing_failed"
			b.mu.Unlock()
			cancel()
			close(done)
			return
		}
		b.nonce = base64.RawURLEncoding.EncodeToString(data[:])
		b.expires = time.Now().Add(15 * time.Minute)
		b.candidate = nil
	}
	b.mu.Unlock()
	go func() { defer close(done); b.run(ctx, c) }()
}
func (b *Bridge) Confirm() (Status, error) {
	b.mu.Lock()
	if b.candidate == nil || b.nonce == "" || time.Now().After(b.expires) {
		b.issue = "pairing_expired"
		b.mu.Unlock()
		return b.Status(), errors.New("pairing_expired")
	}
	m := b.candidate
	b.state.ChatID, b.state.UserID, b.state.Owner = m.Chat.ID, m.From.ID, ownerName(m.From)
	// Pairing grants access only to subsequent conversation, not old private history.
	b.baselineLocked(b.host.Snapshot())
	if b.host.TextControl != nil {
		for _, notice := range b.host.TextControl.Notices() {
			b.state.Messages["notice:"+notice.ID] = delivery{Skip: true}
		}
	}
	e := b.saveLocked()
	if e != nil {
		b.state.ChatID, b.state.UserID = 0, 0
	} else {
		b.nonce = ""
		b.candidate = nil
	}
	b.mu.Unlock()
	return b.Status(), e
}
func (b *Bridge) Disconnect() (Status, error) {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.stop()
	b.mu.Lock()
	b.state.Enabled = false
	b.nonce = ""
	b.candidate = nil
	b.issue = ""
	e := b.saveLocked()
	b.mu.Unlock()
	return b.Status(), e
}
func (b *Bridge) Forget() (Status, error) {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.stop()
	if e := b.secrets.Delete(b.account); e != nil {
		return b.Status(), errors.New("credential")
	}
	b.mu.Lock()
	b.state = document{Version: 1, Inputs: map[string]string{}, Messages: map[string]delivery{}}
	b.secretIngress = map[int]string{}
	b.issue = ""
	b.nonce = ""
	b.candidate = nil
	e := b.saveLocked()
	b.mu.Unlock()
	return b.Status(), e
}
func (b *Bridge) Close() {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.stop()
}
func (b *Bridge) text(en, zh string) string {
	if b.host.Chinese != nil && b.host.Chinese() {
		return zh
	}
	return en
}

// Accepted runs before the desktop consumes selected files. Stage private copies
// immediately; the bridge cannot retain arbitrary mutable composer paths.
func (b *Bridge) Accepted(in api.Submission, files []api.InputFile, _ api.Receipt) {
	if strings.HasPrefix(in.ID, "telegram:") || in.Dream || in.Scheduled || in.ScreenInput {
		return
	}
	b.mu.Lock()
	chat, bot := b.state.ChatID, b.state.BotID
	active := b.state.Enabled && chat != 0
	b.mu.Unlock()
	if !active {
		return
	}
	for index, f := range files {
		key := fmt.Sprintf("upload:%s:%d", in.ID, index)
		path, err := b.copyFile(key, f)
		if err != nil {
			b.setIssue("file_unavailable")
			continue
		}
		relative, err := filepath.Rel(b.root, path)
		if err != nil {
			continue
		}
		b.mu.Lock()
		if b.state.ChatID != chat || b.state.BotID != bot || !b.state.Enabled || len(b.state.PendingFiles) >= 32 {
			if len(b.state.PendingFiles) >= 32 {
				b.issue = "sync_busy"
			}
			b.mu.Unlock()
			os.Remove(path)
			continue
		}
		if b.state.PendingFiles == nil {
			b.state.PendingFiles = map[string]string{}
		}
		b.state.PendingFiles[key] = relative
		_ = b.saveLocked()
		b.mu.Unlock()
	}
}

func (b *Bridge) copyFile(id string, f api.InputFile) (string, error) {
	dir := filepath.Join(b.root, "outgoing", digest(id))
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	src, e := os.Open(f.Path)
	if e != nil {
		return "", e
	}
	defer src.Close()
	info, e := src.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > maxOutputBytes {
		return "", errors.New("file_unavailable")
	}
	dst, e := os.CreateTemp(dir, "file-*")
	if e != nil {
		return "", e
	}
	path := dst.Name()
	n, e := io.Copy(dst, io.LimitReader(src, maxOutputBytes+1))
	ce := dst.Close()
	if e != nil || ce != nil || n > maxOutputBytes {
		os.Remove(path)
		return "", errors.New("file_unavailable")
	}
	nameDir := filepath.Join(dir, digest(f.Name)[:12])
	if e = os.MkdirAll(nameDir, 0700); e != nil {
		return "", e
	}
	named := filepath.Join(nameDir, safeName(f.Name))
	if e = os.Rename(path, named); e != nil {
		return "", e
	}
	return named, nil
}
func safeName(s string) string {
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
	if s == "." || s == ".." || s == "/" || s == "" {
		s = "attachment"
	}
	r := []rune(s)
	if len(r) > 100 {
		s = string(r[:100])
	}
	return s
}
func (b *Bridge) baselineLocked(s api.Snapshot) {
	for _, i := range s.Items {
		key := itemKey(i)
		if _, ok := b.state.Messages[key]; !ok {
			b.state.Messages[key] = delivery{Skip: true}
		}
	}
	if key := phaseNoticeKey(s); key != "" {
		if _, exists := b.state.Messages[key]; !exists {
			b.state.Messages[key] = delivery{Skip: true}
		}
	}
}

// LastReceipt can predate the current Runtime turn. A lifecycle status is
// attributable only when its original user request is in that exact turn.
func phaseNoticeKey(s api.Snapshot) string {
	if s.LastReceipt.ID == "" || s.CurrentTurn == "" {
		return ""
	}
	switch s.Phase {
	case "failed", "interrupted", "unknown":
	default:
		return ""
	}
	for _, item := range s.Items {
		if item.Kind == "user" && item.RequestID == s.LastReceipt.ID && item.TurnKey == s.CurrentTurn {
			return "status:" + s.CurrentTurn + ":" + s.Phase
		}
	}
	return ""
}
func ready(s api.Snapshot) bool { return s.Connection == "ready" || s.Connection == "connected" }

func itemKey(i api.Item) string {
	if i.Kind == "user" && i.RequestID != "" {
		return "input:" + i.RequestID
	}
	return "item:" + i.ID
}

type pollResult struct {
	updates []tg.Update
	err     error
	ack     chan int
}

func (b *Bridge) run(ctx context.Context, c client) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	poll := make(chan pollResult)
	b.mu.Lock()
	offset := b.state.Offset
	b.mu.Unlock()
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		for ctx.Err() == nil {
			v, e := c.Updates(ctx, offset)
			r := pollResult{v, e, make(chan int, 1)}
			select {
			case poll <- r:
			case <-ctx.Done():
				return
			}
			select {
			case offset = <-r.ack:
			case <-ctx.Done():
				return
			}
		}
	}()
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		b.output(ctx, c)
	}()
	typingDone := make(chan struct{})
	go func() {
		defer close(typingDone)
		b.typing(ctx, c)
	}()
	queueDone := make(chan struct{})
	var queueWorkers sync.WaitGroup
	for _, kind := range []int{0, 1, 2} {
		queueWorkers.Go(func() { b.processIngress(ctx, c, kind) })
	}
	go func() { queueWorkers.Wait(); close(queueDone) }()
	defer func() {
		cancel()
		<-pollDone
		<-outputDone
		<-typingDone
		<-queueDone
		b.recoveryWait.Wait()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-poll:
			if r.err != nil {
				issue := issueOf(r.err)
				b.setIssue(issue)
				if issue == "occupied" || issue == "invalid_token" || issue == "blocked" {
					return
				}
				timer := time.NewTimer(3 * time.Second)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					return
				}
				r.ack <- offset
				continue
			}
			// Acknowledge receipt before any disk or native admission wait. This
			// neutral callback answer grants no authority and sends no native effect.
			for _, u := range r.updates {
				if u.CallbackQuery != nil {
					b.answer(ctx, c, u.CallbackQuery.ID, b.text("Received; checking this action.", "已收到，正在核对此操作。"))
				}
			}
			b.mu.Lock()
			seen := map[int]bool{}
			for _, u := range b.state.Ingress {
				seen[u.UpdateID] = true
			}
			next := offset
			for _, u := range r.updates {
				if !seen[u.UpdateID] {
					b.state.Ingress = append(b.state.Ingress, b.queuedUpdateLocked(u))
					seen[u.UpdateID] = true
				}
				next = u.UpdateID + 1
			}
			prior := b.state.Offset
			b.state.Offset = next
			err := b.saveLocked()
			if err != nil {
				b.state.Offset = prior
			} else {
				offset = next
			}
			b.mu.Unlock()
			if err != nil {
				// Keep polling/control feedback alive without acknowledging lost input.
				for _, u := range r.updates {
					if u.CallbackQuery != nil {
						b.answer(ctx, c, u.CallbackQuery.ID, b.text("Storage is temporarily unavailable; this action was not sent. It will be checked again.", "本地存储暂时不可用；此操作尚未发送，将继续核对。"))
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}

			b.mu.Lock()
			if b.issue == "network" {
				b.issue = ""
			}
			b.mu.Unlock()
			r.ack <- offset
		}
	}
}

// queuedUpdateLocked retains the native update identity but never writes a
// Runtime-marked secret answer to telegram.json. Only the live worker has it.
func (b *Bridge) queuedUpdateLocked(u tg.Update) tg.Update {
	m := u.Message
	if m == nil || b.host.TextControl == nil || m.From == nil || m.From.ID != b.state.UserID || m.Chat.ID != b.state.ChatID {
		return u
	}
	if old := m.ReplyToMessage; old != nil && (b.state.SecretMessages[old.MessageID] || b.host.TextControl.IsSecretCommand(old.Text)) {
		copyMessage, copyOld := *m, *old
		copyOld.Text, copyOld.Caption = "", ""
		copyMessage.ReplyToMessage = &copyOld
		if copyMessage.Quote != nil {
			quote := *copyMessage.Quote
			quote.Text = ""
			copyMessage.Quote = &quote
		}
		u.Message = &copyMessage
		m = &copyMessage
	}
	in := textchannel.Inbound{Channel: "telegram", Conversation: fmt.Sprint(m.Chat.ID), Text: m.Text}
	if m.ReplyToMessage != nil {
		in.Reference = &textchannel.Reference{Conversation: in.Conversation, MessageID: fmt.Sprint(m.ReplyToMessage.MessageID)}
	}
	if prompt := b.host.TextControl.SecretPrompt(in); prompt != "" {
		if b.state.SecretPrompts == nil {
			b.state.SecretPrompts = map[int]string{}
		}
		if b.state.SecretMessages == nil {
			b.state.SecretMessages = map[int]bool{}
		}
		b.secretIngress[u.UpdateID] = m.Text
		b.state.SecretPrompts[u.UpdateID] = prompt
		b.state.SecretMessages[m.MessageID] = true
		copyMessage := *m
		copyMessage.Text = ""
		u.Message = &copyMessage
	}
	return u
}

func (b *Bridge) waitPollBoundary(ctx context.Context) bool {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		s := b.host.Snapshot()
		recovery := b.recoveryState()
		if recovery.Automatic || recovery.InProgress {
			select {
			case <-ctx.Done():
				return false
			case <-ticker.C:
			}
			continue
		}
		if ready(s) {
			return b.ensureBaseline(s)
		}
		if s.Connection != "" && s.Connection != "connecting" {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
	return false
}

func (b *Bridge) ensureBaseline(s api.Snapshot) bool {
	if !ready(s) {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.baselineReady {
		return true
	}
	b.baselineLocked(s)
	if b.saveLocked() != nil {
		return false
	}
	b.baselineReady = true
	return true
}

// One output worker bounds concurrency and reads the newest snapshot on each
// tick, coalescing stream edits without accumulating work. Slow sends/uploads
// cannot block incoming stop, status or approval actions. Both workers join on
// cancellation before the lifecycle owner replaces their configuration.
func (b *Bridge) output(ctx context.Context, c client) {
	_ = c.Commands(ctx)
	ticker := time.NewTicker(1100 * time.Millisecond)
	defer ticker.Stop()
	b.reflectOutput(ctx, c)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil || b.backingOff() {
				continue
			}
			b.reflectOutput(ctx, c)
		}
	}
}

func (b *Bridge) reflectOutput(ctx context.Context, c client) {
	if ctx.Err() != nil || b.backingOff() {
		return
	}
	s := b.host.Snapshot()
	b.mirrorRecovery(ctx, c, s)
	b.mirrorNotices(ctx, c)
	if state := b.recoveryState(); state.Automatic || state.InProgress {
		return
	}
	if b.ensureBaseline(s) {
		b.mirror(ctx, c, s)
		if ctx.Err() == nil {
			b.flushFiles(ctx, c)
		}
	}
}
func (b *Bridge) seedNoticeBaseline() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.noticeBaselineReady {
		return true
	}
	if b.host.TextControl != nil {
		for _, notice := range b.host.TextControl.Notices() {
			if notice.SeenAt == 0 {
				key := "notice:" + notice.ID
				if _, known := b.state.Messages[key]; !known {
					b.state.Messages[key] = delivery{Skip: true}
				}
			}
		}
	}
	if b.saveLocked() != nil {
		return false
	}
	b.noticeBaselineReady = true
	return true
}
func (b *Bridge) mirrorNotices(ctx context.Context, c client) {
	if b.host.TextControl == nil || !b.seedNoticeBaseline() {
		return
	}
	b.mu.Lock()
	chat := b.state.ChatID
	b.mu.Unlock()
	if chat == 0 {
		return
	}
	for _, notice := range b.host.TextControl.Notices() {
		out := textchannel.Outbound{Channel: "telegram", Conversation: fmt.Sprint(chat), ID: "notice:" + notice.ID, Text: notice.Text}
		b.sendText(ctx, c, out.ID, chat, out.Text, nil)
	}
}

func (b *Bridge) sendInputFeedback(ctx context.Context, c client, chat int64, request, body string) {
	if b.host.TextControl == nil {
		_, _ = c.Send(ctx, chat, plainText(body), nil)
		return
	}
	b.host.TextControl.Publish(textchannel.Inbound{Channel: "telegram", Conversation: fmt.Sprint(chat), ID: request}, body)
	b.mirrorNotices(ctx, c)
}
func (b *Bridge) input(ctx context.Context, c client, u tg.Update) bool {
	ok, _ := b.inputResult(ctx, c, u)
	return ok
}

// inputResult leaves the polled update unacknowledged only after a proven
// pre-dispatch recovery refusal. The original Telegram update is retried.
func (b *Bridge) inputResult(ctx context.Context, c client, u tg.Update) (bool, bool) {
	if q := u.CallbackQuery; q != nil {
		return b.callback(ctx, c, q), false
	}
	m := u.Message
	if m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != "private" {
		return true, false
	}
	b.mu.Lock()
	chat, owner, bot := b.state.ChatID, b.state.UserID, b.state.BotID
	nonce := b.nonce
	expires := b.expires
	b.mu.Unlock()
	if chat == 0 {
		if nonce != "" && time.Now().Before(expires) && m.Text == "/start "+nonce {
			b.mu.Lock()
			if b.candidate == nil || b.candidate.From.ID == m.From.ID {
				b.candidate = m
			}
			b.mu.Unlock()
			_, _ = c.Send(ctx, m.Chat.ID, plainText(b.text("Confirm this account in Caelis Bot on your Mac to finish connecting.", "请在 Mac 的 Caelis Bot 中确认这是你的账号，即可完成连接。")), nil)
		}
		return true, false
	}
	if m.Chat.ID != chat || m.From.ID != owner {
		return true, false
	}
	request := fmt.Sprintf("telegram:%d:%d", bot, m.MessageID)
	controlIn := textchannel.Inbound{Channel: "telegram", Conversation: fmt.Sprint(chat), ID: request, Text: m.Text}
	if m.ReplyToMessage != nil {
		controlIn.Reference = &textchannel.Reference{Conversation: fmt.Sprint(chat), MessageID: fmt.Sprint(m.ReplyToMessage.MessageID)}
	}
	referenced := b.host.TextControl != nil && b.host.TextControl.HasCardReference(controlIn)
	b.mu.Lock()
	lostSecretPrompt := b.state.SecretPrompts[u.UpdateID]
	b.mu.Unlock()
	if lostSecretPrompt != "" && m.Text == "" {
		if b.host.TextControl != nil {
			b.host.TextControl.RecoverSecretInput(controlIn, lostSecretPrompt)
			b.mirrorNotices(ctx, c)
		}
		return true, false
	}
	command := strings.Split(m.Text, " ")[0]
	var ingressFence string
	if command != "/start" && command != "/stop" && command != "/status" && !textchannel.IsCommand(m.Text) && !referenced {
		state := b.recoveryState()
		connecting := b.host.Snapshot != nil && b.host.Snapshot().Connection == "connecting"
		if state.Automatic || state.InProgress || connecting {
			return true, true
		}
		ingressFence = state.Fence
	}
	b.mu.Lock()
	previous := b.state.Inputs[request]
	exists := previous != "" && previous != "deferred"
	if !exists {
		b.state.Inputs[request] = "dispatching"
		// Output may observe the native user item while Submit is still running.
		// Record its Telegram origin before dispatch so it cannot echo back as Mac input.
		b.state.Messages["input:"+request] = delivery{Skip: true}
		if b.saveLocked() != nil {
			if previous == "" {
				delete(b.state.Inputs, request)
			} else {
				b.state.Inputs[request] = previous
			}
			delete(b.state.Messages, "input:"+request)
			b.mu.Unlock()
			return false, false
		}
	}
	b.mu.Unlock()
	if exists {
		if textchannel.IsCommand(m.Text) && b.host.TextControl != nil {
			_ = b.host.TextControl.Handle(ctx, controlIn, b.host.Snapshot())
			b.mirrorNotices(ctx, c)
		} else if referenced {
			_, _ = b.host.TextControl.HandleReference(ctx, controlIn, b.host.Snapshot())
			b.mirrorNotices(ctx, c)
		}
		return true, false
	}
	if b.host.RecordInput != nil {
		visible := controlIn
		if visible.Text == "" {
			visible.Text = m.Caption
		}
		if visible.Text == "" {
			visible.Text = "[附件]"
		}
		b.host.RecordInput(visible, b.host.TextControl != nil && b.host.TextControl.SecretPrompt(controlIn) != "")
	}
	outcome := "handled"
	switch command {
	case "/start":
		b.sendInputFeedback(ctx, c, chat, request, b.text("Connected. Send a message or file. /stop stops work; /status checks it.", "已连接。直接发送消息或文件即可。/stop 停止工作，/status 查看状态。"))
	case "/stop":
		b.recoveryWait.Add(1)
		go func() {
			defer b.recoveryWait.Done()
			work, stop := context.WithTimeout(ctx, 25*time.Second)
			defer stop()
			err := b.host.Interrupt(work)
			text := b.text("Stop requested. Checking the original work.", "停止请求已提交，正在核对原工作。")
			outcome := "handled"
			if err != nil {
				outcome = "unknown"
				text = b.text("Stop could not be confirmed yet. Bot is online; check the original work with /status.", "停止结果暂未确认。Bot 在线，请用 /status 核对原工作。")
			}
			b.mu.Lock()
			b.state.Inputs[request] = outcome
			_ = b.saveLocked()
			b.mu.Unlock()
			feedback, finish := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer finish()
			if b.host.TextControl != nil {
				b.host.TextControl.Publish(textchannel.Inbound{Channel: "telegram", Conversation: fmt.Sprint(chat), ID: request}, text)
				b.mirrorNotices(feedback, c)
			} else {
				_, _ = c.Send(feedback, chat, plainText(text), nil)
			}
		}()
		return true, false
	case "/status":
		s := b.host.Snapshot()
		state := b.recoveryState()
		text := b.text("Caelis Bot is online; the Runtime is connecting.", "Caelis Bot 在线，Runtime 正在连接。")
		if ready(s) && !state.Automatic && !state.InProgress {
			text = b.text("Caelis Bot is online.", "Caelis Bot 在线。")
			if s.Phase == "working" || s.Phase == "sending" {
				text = b.text("Caelis Bot is online and working.", "Caelis Bot 在线，正在工作。")
			}
		} else if !state.Automatic && !state.InProgress {
			text = b.text("Caelis Bot is online; the Runtime is temporarily offline. Recovery remains available.", "Caelis Bot 在线，Runtime 暂时离线；可以继续恢复连接。")
		}
		if b.host.TextControl != nil {
			b.host.TextControl.Publish(textchannel.Inbound{Channel: "telegram", Conversation: fmt.Sprint(chat), ID: request}, text)
			b.mirrorNotices(ctx, c)
		} else {
			_, _ = c.Send(ctx, chat, plainText(text), nil)
		}
		if state.Manual {
			b.mirrorRecovery(ctx, c, s)
		}
	case "/approve", "/answer":
		if b.host.TextControl != nil {
			_ = b.host.TextControl.Handle(ctx, textchannel.Inbound{Channel: "telegram", Conversation: fmt.Sprint(chat), ID: request, Text: m.Text}, b.host.Snapshot())
			b.mirrorNotices(ctx, c)
		} else {
			b.sendInputFeedback(ctx, c, chat, request, "控制命令暂不可用，请在 Mac 中处理。")
		}

	default:
		if referenced {
			_, _ = b.host.TextControl.HandleReference(ctx, controlIn, b.host.Snapshot())
			b.mirrorNotices(ctx, c)
			break
		}
		if textchannel.IsCommand(m.Text) {
			if b.host.TextControl != nil {
				_ = b.host.TextControl.Handle(ctx, textchannel.Inbound{Channel: "telegram", Conversation: fmt.Sprint(chat), ID: request, Text: m.Text}, b.host.Snapshot())
				b.mirrorNotices(ctx, c)
			} else {
				b.sendInputFeedback(ctx, c, chat, request, "未知命令。请复制当前请求中的完整 /approve 或 /answer 命令。")
			}
			break
		}
		if !b.ensureBaseline(b.host.Snapshot()) {
			outcome = "rejected"
			b.sendInputFeedback(ctx, c, chat, request, b.text("The local Runtime is not connected yet. Check /status or Caelis Bot on your Mac; this message was not sent to the Runtime.", "本机 Runtime 尚未连接。请查看 /status 或 Mac 上的 Caelis Bot；这条消息未发送给 Runtime。"))
			break
		}
		files, stickerNote, e := b.download(ctx, c, request, m)
		if e != nil {
			outcome = "rejected"
			b.sendInputFeedback(ctx, c, chat, request, b.downloadNotice(e))
		} else {
			text := m.Text
			if text == "" {
				text = m.Caption
			}
			if stickerNote != "" {
				text = strings.TrimSpace(text + "\n" + stickerNote)
			}
			if strings.TrimSpace(text) == "" && len(files) == 0 {
				outcome = "rejected"
				b.sendInputFeedback(ctx, c, chat, request, b.text("Send text, a photo or a file.", "请发送文字、图片或文件。"))
			} else {
				if b.host.TextControl != nil {
					if err := b.host.TextControl.RecordOrigin(request, textchannel.Origin{Channel: "telegram", Conversation: fmt.Sprint(chat)}); err != nil {
						outcome = "rejected"
						b.sendInputFeedback(ctx, c, chat, request, "来源记录不可用，消息未提交。")
						break
					}
				}
				quoted := b.quotedMessage(m, bot, owner)
				if m.ReplyToMessage != nil && quoted == nil {
					outcome = "rejected"
					b.sendInputFeedback(ctx, c, chat, request, b.text("The quoted text is unavailable. Send the text you want me to use as a new message.", "引用内容无法读取，请把需要我参考的文字作为新消息发送。"))
					break
				}
				receipt, submitErr := b.host.Submit(ctx, api.Submission{ID: request, Text: text, Quoted: quoted, IngressFence: ingressFence}, files)
				if errors.Is(submitErr, api.ErrRecoveryPending) {
					b.mu.Lock()
					b.state.Inputs[request] = "deferred"
					delete(b.state.Messages, "input:"+request)
					e := b.saveLocked()
					b.mu.Unlock()
					return e == nil, true
				}
				outcome = receipt.Outcome
				if outcome != "accepted" && outcome != "rejected" && outcome != "unknown" {
					outcome = "unknown"
				}
				if outcome != "accepted" {
					reason := receipt.Message
					if reason == "" && submitErr != nil && outcome == "rejected" {
						reason = submitErr.Error() // Only an exact allowlisted code is projected.
					}
					notice := b.rejectionNotice(reason)
					if outcome == "unknown" {
						notice = b.text("Delivery is uncertain. Check the original message on your Mac; it will not be sent again automatically.", "发送结果暂不确定。请在 Mac 查看原消息，系统不会自动重复发送。")
					}
					b.sendInputFeedback(ctx, c, chat, request, notice)
				}
			}
		}
	}
	if outcome == "accepted" || outcome == "rejected" {
		os.RemoveAll(filepath.Join(b.root, "incoming", digest(request)))
	}
	b.mu.Lock()
	b.state.Inputs[request] = outcome
	b.state.Messages["input:"+request] = delivery{Skip: true}
	e := b.saveLocked()
	b.mu.Unlock()
	return e == nil, false
}

func (b *Bridge) quotedMessage(m *tg.Message, bot, owner int64) *api.QuotedMessage {
	if m == nil || m.ReplyToMessage == nil {
		return nil
	}
	old := m.ReplyToMessage
	q := &api.QuotedMessage{HostID: fmt.Sprint(old.MessageID), Role: "unknown", Text: old.Text}
	if q.Text == "" {
		q.Text = old.Caption
	}
	if m.Quote != nil {
		q.Text, q.Excerpt = m.Quote.Text, true
	}
	b.mu.Lock()
	secret := b.state.SecretMessages[old.MessageID]
	var delivered delivery
	var matches int
	for key, delivered := range b.state.Messages {
		for _, id := range delivered.IDs {
			if id == old.MessageID {
				q.LocalID = key
				matches++
				break
			}
		}
	}
	if matches == 1 {
		delivered = b.state.Messages[q.LocalID]
	} else {
		q.LocalID = ""
	}
	input := fmt.Sprintf("telegram:%d:%d", bot, old.MessageID)
	acceptedInput := b.state.Inputs[input] == "accepted"
	b.mu.Unlock()
	if old.From != nil {
		if old.From.ID == owner {
			q.Role, q.LocalID = "user", fmt.Sprintf("telegram:%d:%d", bot, old.MessageID)
		}
		if old.From.ID == bot && old.From.IsBot {
			q.Role = "assistant"
		}
	}
	if secret || b.host.TextControl != nil && b.host.TextControl.IsSecretCommand(q.Text) {
		return nil
	}
	// Telegram can supply the reply ID but omit the old text even when its
	// client displays a quote. Recover only an exact, single confirmed Bot
	// delivery (or the original accepted user input) from the shared IM view.
	// The hash prevents a later edited item from becoming a different quote.
	if q.Text == "" && b.host.Snapshot != nil {
		for _, item := range b.host.Snapshot().Items {
			if matches == 1 && item.Kind == "assistant" && itemKey(item) == q.LocalID && len(delivered.IDs) == 1 && delivered.IDs[0] == old.MessageID && len(delivered.Hashes) == 1 && delivered.Hashes[0] == digest(item.Text) && item.Text != "" {
				q.Text, q.Role = item.Text, "assistant"
				break
			}
			if acceptedInput && item.Kind == "user" && item.RequestID == input && item.Text != "" {
				q.Text, q.Role, q.LocalID = item.Text, "user", input
				break
			}
		}
	}
	if q.Text == "" {
		return nil
	}
	return api.BoundQuote(q)
}
func (b *Bridge) download(ctx context.Context, c client, request string, m *tg.Message) ([]api.InputFile, string, error) {
	if m.Sticker != nil {
		return b.downloadSticker(ctx, c, request, m.Sticker)
	}
	id, name, size := "", "", int64(0)
	switch {
	case m.Document != nil:
		id, name, size = m.Document.FileID, m.Document.FileName, m.Document.FileSize
	case len(m.Photo) > 0:
		p := m.Photo[len(m.Photo)-1]
		id, name, size = p.FileID, "photo.jpg", int64(p.FileSize)
	case m.Audio != nil:
		id, name, size = m.Audio.FileID, m.Audio.FileName, m.Audio.FileSize
	case m.Voice != nil:
		id, name, size = m.Voice.FileID, "voice.ogg", m.Voice.FileSize
	case m.Video != nil:
		id, name, size = m.Video.FileID, m.Video.FileName, m.Video.FileSize
	}
	if id == "" {
		return nil, "", nil
	}
	if size > maxInputBytes {
		return nil, "", errors.New("file_too_large")
	}
	dir := filepath.Join(b.root, "incoming", digest(request))
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, "", e
	}
	path := filepath.Join(dir, safeName(name))
	if e := c.Download(ctx, id, path); e != nil {
		return nil, "", e
	}
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() {
		return nil, "", errors.New("download_failed")
	}
	if info.Size() > maxInputBytes {
		return nil, "", errors.New("file_too_large")
	}
	return []api.InputFile{{Name: filepath.Base(path), Path: path}}, "", nil
}

func (b *Bridge) downloadSticker(ctx context.Context, c client, request string, sticker *tg.Sticker) ([]api.InputFile, string, error) {
	id, size := sticker.FileID, int64(sticker.FileSize)
	name := "sticker.webp"
	note := "贴纸"
	if sticker.IsAnimated || sticker.IsVideo {
		if sticker.Thumbnail == nil || sticker.Thumbnail.FileID == "" {
			return nil, "", errors.New("sticker_preview_unavailable")
		}
		id, size = sticker.Thumbnail.FileID, int64(sticker.Thumbnail.FileSize)
		name = "sticker-preview"
		if sticker.IsVideo {
			note = "视频贴纸的单帧预览；无法据此判断完整动作"
		} else {
			note = "动画贴纸的单帧预览；无法据此判断完整动作"
		}
	}
	if size > maxInputBytes {
		return nil, "", errors.New("file_too_large")
	}
	dir := filepath.Join(b.root, "incoming", digest(request))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, name)
	if err := c.Download(ctx, id, path); err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", errors.New("download_failed")
	}
	if len(data) > maxInputBytes {
		return nil, "", errors.New("file_too_large")
	}
	mime := http.DetectContentType(data)
	ext := map[string]string{"image/webp": ".webp", "image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif"}[mime]
	if ext == "" || len(data) < 16 {
		return nil, "", errors.New("sticker_preview_invalid")
	}
	if filepath.Ext(path) != ext {
		target := filepath.Join(dir, "sticker-preview"+ext)
		if err := os.Rename(path, target); err != nil {
			return nil, "", err
		}
		path = target
	}
	if sticker.Emoji != "" {
		note += "（关联 emoji：" + sticker.Emoji + "）"
	}
	return []api.InputFile{{Name: filepath.Base(path), Path: path}}, note, nil
}

func (b *Bridge) downloadNotice(err error) string {
	switch err.Error() {
	case "sticker_preview_unavailable":
		return b.text("This animated sticker has no viewable preview frame. Please send an image instead.", "这张动态贴纸没有可查看的预览帧，请改发图片。")
	case "sticker_preview_invalid":
		return b.text("The sticker preview is not a supported image.", "贴纸预览不是可识别的图片。")
	case "file_too_large":
		return b.text("The attachment exceeds the 8 MB limit.", "附件超过 8 MB 限制。")
	default:
		return b.text("The attachment could not be downloaded. No message was sent to Bot.", "附件下载失败，消息未发送给 Bot。")
	}
}

// Only Bot-owned, exact refusal messages may be projected to the companion.
// Native errors can contain private paths or provider output and are never sent.
func (b *Bridge) rejectionNotice(message string) string {
	switch message {
	case "当前无法发送，请先处理待确认事项或恢复连接":
		return b.text("Not sent: reconnect Codex or resolve the pending decision in Caelis Bot on your Mac.", "未发送：请在 Mac 的 Caelis Bot 恢复 Codex 连接或处理待确认事项。")
	case "请先完成 Bot 初始化，并等待介绍发送完成", "Bot initialization required":
		return b.text("Not sent: finish Bot setup on your Mac first.", "未发送：请先在 Mac 完成 Bot 初始化。")
	case "runtime setup required", "请先完成运行时设置":
		return b.text("Not sent: finish Runtime connection setup on your Mac first.", "未发送：请先在 Mac 完成运行时连接设置。")
	case "当前 Bot 模型的图片能力不可用，请切换到支持图片的模型":
		return b.text("Not sent: the current Bot model does not support image input.", "未发送：当前 Bot 模型不支持图片输入。")
	case "图片预览存储暂不可用，消息未发送":
		return b.text("Not sent: image storage is unavailable on your Mac.", "未发送：Mac 上的图片存储暂不可用。")
	default:
		return b.text("Not sent: Bot rejected the message. Check the original request on your Mac before trying again.", "未发送：Bot 拒绝了消息。再次尝试前请在 Mac 核对原请求。")
	}
}

// The callback binds the current ordered native choices and their scopes to
// the original approval generation. Adapter response payloads are immutable
// within that ID (Codex includes transport sequence; Caelis hashes the native
// approval). A stale button cannot borrow a choice ID after its visible scope
// or ordering has changed.
func approvalFingerprint(a api.Approval) string {
	value, _ := json.Marshal(struct {
		ID      string       `json:"id"`
		TurnKey string       `json:"turnKey"`
		Choices []api.Choice `json:"choices"`
	}{a.ID, a.TurnKey, a.Choices})
	return digest(string(value))
}
func callbackID(a api.Approval, choice string) string {
	return "a:" + digest(approvalFingerprint(a) + "\x00" + choice)[:40]
}
func keyboardDigest(keys *tg.InlineKeyboardMarkup) string {
	if keys == nil {
		return ""
	}
	encoded, _ := json.Marshal(keys)
	return digest(string(encoded))
}

func (b *Bridge) approvalChoiceText(choice api.Choice) (string, bool) {
	label := choice.Label // Runtime-provided labels are display text, never keys.
	if strings.TrimSpace(label) == "" && choice.LabelKey != "" {
		locale := i18n.English
		if b.host.Chinese != nil && b.host.Chinese() {
			locale = i18n.Chinese
		}
		var found bool
		label, found = i18n.Lookup(locale, choice.LabelKey, nil)
		if !found {
			return "", false
		}
	}
	if strings.TrimSpace(choice.ID) == "" || !utf8.ValidString(label) {
		return "", false
	}
	visible := false
	for _, r := range label {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return "", false
		}
		visible = visible || unicode.IsGraphic(r) && !unicode.IsSpace(r)
	}
	return label, visible
}

func (b *Bridge) approvalKeyboard(a api.Approval) (*tg.InlineKeyboardMarkup, bool) {
	keys := &tg.InlineKeyboardMarkup{}
	seen := make(map[string]bool, len(a.Choices))
	for _, choice := range a.Choices {
		label, ok := b.approvalChoiceText(choice)
		if !ok || seen[choice.ID] {
			return nil, false // Never silently omit an offered native decision.
		}
		seen[choice.ID] = true
		keys.InlineKeyboard = append(keys.InlineKeyboard, []tg.InlineKeyboardButton{{Text: label, CallbackData: callbackID(a, choice.ID)}})
	}
	return keys, len(keys.InlineKeyboard) > 0
}

func asyncQuestionCallbackID(q textchannel.AsyncButtonQuestion, index int) string {
	return "q:" + digest(q.Fingerprint + "\x00" + q.ShortID + "\x00" + fmt.Sprint(index))[:40]
}

// TG renders ordered choices as buttons. The shared text card remains the
// fallback for free input or any option that TG cannot display faithfully.
func asyncQuestionButtons(questions []textchannel.AsyncButtonQuestion) (string, *tg.InlineKeyboardMarkup, bool) {
	if len(questions) == 0 {
		return "", nil, false
	}
	keys := &tg.InlineKeyboardMarkup{}
	var lines []string
	for _, q := range questions {
		line := "[" + q.ShortID + "] " + q.Title
		switch q.State {
		case "accepted":
			line += "\n回答已提交。"
		case "dispatching", "unknown":
			line += "\n回答投递结果待核对；不会自动重发。"
		case "":
			if len(q.Options) == 0 {
				line += "\n回答请输入 /answer " + q.ShortID + " 后接完整回答。"
			}
			for index, option := range q.Options {
				label := option
				if len(questions) > 1 {
					label = "[" + q.ShortID + "] " + label
				}
				if !utf8.ValidString(label) || strings.TrimSpace(label) == "" || utf16Length(label) > 64 {
					return "", nil, false
				}
				for _, r := range label {
					if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
						return "", nil, false
					}
				}
				keys.InlineKeyboard = append(keys.InlineKeyboard, []tg.InlineKeyboardButton{{Text: label, CallbackData: asyncQuestionCallbackID(q, index)}})
			}
		default:
			return "", nil, false
		}
		lines = append(lines, line)
	}
	if len(keys.InlineKeyboard) == 0 {
		body := strings.Join(lines, "\n\n")
		return body, nil, utf16Length(body) <= 4000
	}
	body := strings.Join(lines, "\n\n")
	return body, keys, utf16Length(body) <= 4000
}

func (b *Bridge) asyncQuestionCallback(ctx context.Context, c client, query *tg.CallbackQuery, message *tg.Message, chat int64) bool {
	if b.host.TextControl == nil || !strings.HasPrefix(query.Data, "q:") {
		return false
	}
	for _, itemID := range b.host.TextControl.AsyncButtonItemIDs() {
		questions := b.host.TextControl.AsyncButtonQuestions(itemID)
		_, keys, ok := asyncQuestionButtons(questions)
		if !ok {
			continue
		}
		for _, question := range questions {
			for index := range question.Options {
				if query.Data != asyncQuestionCallbackID(question, index) {
					continue
				}
				b.mu.Lock()
				record := b.state.Messages["item:"+itemID]
				bound := !record.Skip && record.Keyboard != "" && record.Keyboard == keyboardDigest(keys) && len(record.IDs) == 1 && record.IDs[0] == message.MessageID
				b.mu.Unlock()
				if !bound {
					b.answer(ctx, c, query.ID, b.text("This question was handled or changed.", "问题已处理或已变化。"))
					return true
				}
				b.answer(ctx, c, query.ID, b.text("Received; checking the answer.", "已收到，正在提交回答。"))
				b.recoveryWait.Add(1)
				go func() {
					defer b.recoveryWait.Done()
					work, stop := context.WithTimeout(ctx, 16*time.Second)
					defer stop()
					b.host.TextControl.Handle(work, textchannel.Inbound{Channel: "telegram", Conversation: fmt.Sprint(chat), ID: "callback:" + query.ID, Text: fmt.Sprintf("/answer %s %d", question.ShortID, index+1)}, b.host.Snapshot())
					if body, currentKeys, ok := asyncQuestionButtons(b.host.TextControl.AsyncButtonQuestions(itemID)); ok {
						b.sendText(work, c, "item:"+itemID, chat, body, currentKeys)
					}
					b.mirror(work, c, b.host.Snapshot())
				}()
				return true
			}
		}
	}
	b.answer(ctx, c, query.ID, b.text("This question was handled or changed.", "问题已处理或已变化。"))
	return true
}

func (b *Bridge) callback(ctx context.Context, c client, q *tg.CallbackQuery) bool {
	b.mu.Lock()
	owner, chat := b.state.UserID, b.state.ChatID
	b.mu.Unlock()
	m, ok := q.Message.(*tg.Message)
	if !ok || q.From.ID != owner || m.Chat.ID != chat {
		return true
	}
	if isRecoveryButton(q.Data) {
		return b.recoveryCallback(ctx, c, q, m)
	}
	snapshot := b.host.Snapshot()
	if !ready(snapshot) {
		b.answer(ctx, c, q.ID, b.text("The Runtime is not ready. Check the original request on your Mac.", "Runtime 尚未就绪，请在 Mac 核对原请求。"))
		return true
	}
	if b.asyncQuestionCallback(ctx, c, q, m, chat) {
		return true
	}
	for _, a := range snapshot.Approvals {
		if a.Status != "pending" || len(a.Questions) > 0 || a.URL != "" {
			continue
		}
		currentKeys, valid := b.approvalKeyboard(a)
		if !valid {
			continue
		}
		for index, choice := range a.Choices {
			if q.Data == callbackID(a, choice.ID) {
				if b.host.TextControl != nil {
					if feedback, claimed := b.host.TextControl.Claimed(a.ID); claimed {
						b.answer(ctx, c, q.ID, feedback)
						return true
					}
				}
				key := "approval-decision:" + a.ID
				b.mu.Lock()
				record := b.state.Messages["approval:"+a.ID]
				fromButton := !record.Skip && !record.Closed && record.Keyboard == keyboardDigest(currentKeys) && len(record.IDs) > 0 && record.IDs[len(record.IDs)-1] > 0 && record.IDs[len(record.IDs)-1] == m.MessageID
				_, used := b.state.Inputs[key]
				if fromButton && !used {
					b.state.Inputs[key] = "dispatching"
					if b.saveLocked() != nil {
						delete(b.state.Inputs, key)
						b.mu.Unlock()
						b.answer(ctx, c, q.ID, b.text("Storage is unavailable; the decision was not sent. Retry shortly.", "本地存储暂时不可用，审批尚未发送；请稍后重试。"))
						return false
					}
				}
				b.mu.Unlock()
				if used || !fromButton {
					b.answer(ctx, c, q.ID, b.text("This request has changed. Check your Mac.", "此请求已变化，请在 Mac 查看。"))
					return true
				}
				b.answer(ctx, c, q.ID, b.text("Received; checking the Runtime result.", "已收到，正在核对 Runtime 审批结果。"))
				b.recoveryWait.Add(1)
				go func() {
					defer b.recoveryWait.Done()
					work, stop := context.WithTimeout(ctx, 16*time.Second)
					defer stop()
					var err error
					if b.host.TextControl != nil {
						_, short, cardErr := b.host.TextControl.Card(a)
						if cardErr != nil || short == "" {
							err = errors.New("approval catalog unavailable")
						} else {
							feedback := b.host.TextControl.Handle(work, textchannel.Inbound{Channel: "telegram", Conversation: fmt.Sprint(chat), ID: "callback:" + q.ID, Text: fmt.Sprintf("/approve %s %d", short, index+1)}, b.host.Snapshot())
							if feedback != "决定已提交，等待 Runtime 确认。" {
								err = errors.New("approval not confirmed")
							}
						}
					} else {
						err = b.host.Decide(work, api.Decision{ID: a.ID, Choice: choice.ID})
					}
					outcome := "handled"
					if err != nil {
						outcome = "unknown"
					}
					b.mu.Lock()
					b.state.Inputs[key] = outcome
					_ = b.saveLocked()
					b.mu.Unlock()
					feedback, finish := context.WithTimeout(ctx, 3*time.Second)
					defer finish()
					b.mirror(feedback, c, b.host.Snapshot())
				}()
				return true

			}
		}
	}
	b.answer(ctx, c, q.ID, b.text("This request has changed. Check your Mac.", "此请求已变化，请在 Mac 查看。"))
	return true
}
func (b *Bridge) mirror(ctx context.Context, c client, s api.Snapshot) {
	b.mu.Lock()
	chat := b.state.ChatID
	b.mu.Unlock()
	if chat == 0 {
		return
	}
	// Prepending older history in the desktop must not publish it as new output.
	b.mu.Lock()
	for index, item := range s.Items {
		if _, known := b.state.Messages[itemKey(item)]; known {
			for _, old := range s.Items[:index] {
				if _, seen := b.state.Messages[itemKey(old)]; !seen {
					b.state.Messages[itemKey(old)] = delivery{Skip: true}
				}
			}
			break
		}
	}
	b.mu.Unlock()
	for _, i := range s.Items {
		if i.Kind == "hostNotice" || i.Kind == "activation" {
			continue // Never mirror internal trigger text or attached bytes.
		}
		if i.Kind == "user" && (i.Status == "sending" || i.Status == "rejected" || i.Status == "unknown") {
			continue // A local presentation bubble is not a delivered native input.
		}
		if i.Text == api.SilentReminder {
			continue
		}
		if i.Kind == "user" {
			if b.host.TextControl != nil {
				if origin, ok := b.host.TextControl.OriginOf(i.RequestID); ok && origin.Channel != "" {
					if origin.Channel == "telegram" && origin.Conversation == fmt.Sprint(chat) {
						continue
					}
					b.sendUserText(ctx, c, itemKey(i), chat, i.Text, b.text("User", "用户"))
				} else {
					b.sendMacUserText(ctx, c, itemKey(i), chat, i.Text)
				}
			} else {
				b.sendMacUserText(ctx, c, itemKey(i), chat, i.Text)
			}
		} else if i.Kind == "assistant" {
			rendered := false
			if b.host.TextControl != nil {
				if text, keys, ok := asyncQuestionButtons(b.host.TextControl.AsyncButtonQuestions(i.ID)); ok {
					b.sendText(ctx, c, itemKey(i), chat, text, keys)
					rendered = true
				}
			}
			if !rendered {
				b.sendAssistantText(ctx, c, itemKey(i), chat, i.Text)
			}
		}
		for _, a := range i.Artifacts {
			b.mu.Lock()
			old, seen := b.state.Messages[itemKey(i)]
			b.mu.Unlock()
			if seen && old.Skip {
				continue
			}
			path, e := b.host.Artifact(a.ID)
			if e != nil {
				b.setIssue("file_unavailable")
				continue
			}
			b.sendFile(ctx, c, "artifact:"+a.ID, path)
		}
		if i.Screen != nil {
			for _, img := range i.Screen.Images {
				key := "screen:" + img.ID
				b.mu.Lock()
				_, seen := b.state.Messages[key]
				skip := b.state.Messages[itemKey(i)].Skip
				b.mu.Unlock()
				if seen || skip {
					continue
				}
				data, e := b.host.ScreenImage(img.ID)
				if e != nil {
					continue
				}
				dir := filepath.Join(b.root, "outgoing", digest(key))
				if os.MkdirAll(dir, 0700) != nil {
					continue
				}
				path := filepath.Join(dir, "screen.png")
				if os.WriteFile(path, data, 0600) == nil {
					b.sendFile(ctx, c, key, path)
				}
				os.RemoveAll(dir)
			}
		}
	}
	if key := phaseNoticeKey(s); key != "" {
		notice := ""
		switch s.Phase {
		case "failed":
			notice = b.text("Bot could not complete this request. Check Caelis Bot on your Mac.", "Bot 未能完成此请求，请在 Mac 的 Caelis Bot 中查看原因。")
		case "interrupted":
			notice = b.text("Work stopped.", "工作已停止。")
		case "unknown":
			notice = b.text("The result is uncertain. Check the original request on your Mac before resending.", "结果暂不确定。请在 Mac 查看原请求后再决定是否重发。")
		}
		if notice != "" {
			b.sendText(ctx, c, key, chat, notice, nil)
		}
	}

	unavailable := false
	visible := make(map[string]bool, len(s.Approvals))
	for _, a := range s.Approvals {
		visible[a.ID] = true
		if a.Status == "resolved" {
			b.closeApproval(ctx, c, chat, a.ID, approvalMessageText(a), b.approvalResultText(a.Resolution))
			continue
		}
		b.mu.Lock()
		closed := b.state.Messages["approval:"+a.ID].Closed
		decision := b.state.Inputs["approval-decision:"+a.ID]
		b.mu.Unlock()
		if b.host.TextControl != nil {
			if _, claimed := b.host.TextControl.Claimed(a.ID); claimed && decision == "" {
				decision = "submitted"
			}
		}
		if closed {
			continue // A terminal fence cannot be reopened by stale recovery state.
		}
		original := approvalMessageText(a)
		text := original
		if b.host.TextControl != nil {
			if card, _, err := b.host.TextControl.Card(a); err == nil && card != "" {
				text = card
				original = card
			}
		}
		if text == "" {
			text = b.text("Decision needed.", "需要决定。")
		}
		var keys *tg.InlineKeyboardMarkup
		if a.Status == "pending" && decision != "" {
			if decision == "unknown" {
				text += "\n" + b.text("The decision result is uncertain. The original request is retained; check /status.", "审批结果暂未确认，原请求已保留；请查看 /status。")
			} else {
				text += "\n" + b.text("Submitted; waiting for the Runtime result.", "已提交，等待 Runtime 确认结果。")
			}
		} else if a.Status == "pending" && len(a.Questions) == 0 && a.URL == "" && len(a.Choices) > 0 {
			var valid bool
			keys, valid = b.approvalKeyboard(a)
			if valid && b.host.TextControl != nil {
				if _, short, err := b.host.TextControl.Card(a); err == nil && short != "" {
					text = textchannel.ButtonCard(a, short)
					original = text
				} else {
					valid = false
				}
			}
			if !valid {
				keys = nil
				unavailable = true
				b.setIssue("approval_unavailable")
				text += "\n" + b.text("Approval options are unavailable here. Complete this request in Caelis Bot on your Mac.", "此处无法显示审批选项，请在 Mac 的 Caelis Bot 中完成此请求。")
			}
		} else if a.Status == "sent" || a.Status == "sending" {
			text += "\n" + b.text("Submitted; waiting for the Runtime result.", "已提交，等待 Runtime 确认结果。")
		} else if a.Status == "unknown" {
			text += "\n" + b.text("The decision result is uncertain. Check the original request on your Mac; do not submit it again.", "决定结果暂不确定。请在 Mac 核对原请求，不要再次提交。")
		} else if a.Status == "pending" && len(a.Questions) > 0 && b.host.TextControl != nil {
			// The shared text card above owns the answer commands.
		} else if a.Status == "pending" && len(a.Choices) == 0 {
			text += "\n" + b.text("The Runtime did not provide an actionable option here. Check the original request in the Runtime.", "Runtime 未提供此处可操作的选项，请在 Runtime 中核对原请求。")
		} else {
			text += "\n" + b.text("Complete this request in Caelis Bot on your Mac.", "请在 Mac 的 Caelis Bot 中完成此请求。")
		}
		ids := b.sendApprovalText(ctx, c, "approval:"+a.ID, chat, original, text, keys)
		if b.host.TextControl != nil {
			for _, id := range ids {
				_ = b.host.TextControl.BindCard("telegram", fmt.Sprint(chat), fmt.Sprint(id), a)
			}
		}
	}
	// Native resolution may remove a prompt from the snapshot entirely. A
	// disconnected snapshot is not an authoritative absence.
	if ready(s) {
		b.mu.Lock()
		var vanished []string
		for key := range b.state.Messages {
			if strings.HasPrefix(key, "approval:") && !visible[strings.TrimPrefix(key, "approval:")] {
				vanished = append(vanished, strings.TrimPrefix(key, "approval:"))
			}
		}
		b.mu.Unlock()
		for _, id := range vanished {
			b.closeApproval(ctx, c, chat, id, "", b.text("Handled", "已处理"))
		}
	}
	b.mu.Lock()
	if !unavailable && b.issue == "approval_unavailable" {
		b.issue = ""
	}
	b.mu.Unlock()
}

func (b *Bridge) approvalResultText(r *api.ApprovalResolution) string {
	if r == nil {
		return b.text("Handled", "已处理")
	}
	switch r.Outcome {
	case "allowed":
		scope := map[string]string{"once": b.text("once", "一次"), "session": b.text("this session", "本会话"), "always": b.text("always", "始终"), "conversation": b.text("this conversation", "本次对话"), "turn": b.text("this turn", "本轮"), "rule": b.text("rule", "规则")}[r.Scope]
		if scope != "" {
			return b.text("✅ Allowed (", "✅ 已允许（") + scope + b.text(")", "）")
		}
		return b.text("✅ Allowed", "✅ 已允许")
	case "declined":
		return b.text("❌ Declined", "❌ 已拒绝")
	case "cancelled":
		return b.text("🚫 Cancelled", "🚫 已取消")
	default:
		return b.text("Handled", "已处理")
	}
}

func approvalBodyParts(original string) []string {
	// The private delivery ledger is a recovery aid, not a second transcript.
	// Very large native payloads keep their original Telegram text and only
	// lose the keyboard at terminal state.
	if original == "" || utf16Length(original) > 16000 {
		return nil
	}
	return splitTextLimit(original, 3800)
}

func (b *Bridge) sendApprovalText(ctx context.Context, c client, key string, chat int64, original, text string, keys *tg.InlineKeyboardMarkup) []int {
	if original == "" {
		original = b.text("Decision needed.", "需要决定。")
	}
	parts := approvalBodyParts(original)
	b.mu.Lock()
	record := b.state.Messages[key]
	if !record.Closed {
		if !slices.Equal(record.ApprovalBody, parts) {
			record.ApprovalBody = append([]string(nil), parts...)
			b.state.Messages[key] = record
			if b.saveLocked() != nil {
				b.mu.Unlock()
				return nil
			}
		}
	}
	b.mu.Unlock()
	if len(parts) == 0 {
		b.sendText(ctx, c, key, chat, text, keys)
		return nil
	}
	if suffix, ok := strings.CutPrefix(text, original); ok && utf16Length(parts[len(parts)-1])+utf16Length(suffix) <= 4000 {
		parts[len(parts)-1] += suffix
	} else {
		parts = splitTextLimit(text, 3800)
	}
	messages := make([]outgoingText, len(parts))
	for i, part := range parts {
		messages[i] = plainText(part)
	}
	b.sendRenderedText(ctx, c, key, chat, messages, nil, keys)
	b.mu.Lock()
	defer b.mu.Unlock()
	record = b.state.Messages[key]
	if len(record.IDs) < len(messages) || len(record.Hashes) < len(messages) {
		return nil
	}
	var ids []int
	for i, message := range messages {
		if record.IDs[i] <= 0 || record.Hashes[i] != outgoingDigest(message) {
			return nil
		}
		ids = append(ids, record.IDs[i])
	}
	return ids
}

func (b *Bridge) closeApproval(ctx context.Context, c client, chat int64, id, original, status string) {
	key := "approval:" + id
	b.mu.Lock()
	record, exists := b.state.Messages[key]
	if !exists || record.Skip || len(record.IDs) == 0 {
		b.mu.Unlock()
		return
	}
	if record.Closed {
		changed := false
		// A native resolution can race the local respond return. Upgrade a
		// neutral terminal card only when the same approval later has a decision.
		if (record.Terminal == "Handled" || record.Terminal == "已处理") && status != "Handled" && status != "已处理" {
			record.Terminal = status
			changed = true
		} else if record.Terminal != "" {
			status = record.Terminal
		}
		if len(record.ApprovalBody) == 0 && original != "" {
			record.ApprovalBody = approvalBodyParts(original)
			changed = true
		}
		if changed {
			b.state.Messages[key] = record
			if b.saveLocked() != nil {
				b.mu.Unlock()
				return
			}
		}
	} else {
		record.Closed = true
		record.Terminal = status
		if len(record.ApprovalBody) == 0 && original != "" {
			record.ApprovalBody = approvalBodyParts(original)
		}
		b.state.Messages[key] = record
		if b.saveLocked() != nil {
			b.mu.Unlock()
			return
		}
	}
	b.mu.Unlock()
	for _, messageID := range record.IDs {
		if messageID <= 0 {
			return // An unconfirmed part must not become a new terminal message.
		}
	}
	if len(record.ApprovalBody) != len(record.IDs) {
		b.clearApprovalMarkup(ctx, c, key, chat, record)
		return
	}
	// Original card parts stay on their original message IDs. Only the final
	// part gains a short result; no terminal notification is sent separately.
	messages := make([]outgoingText, len(record.ApprovalBody))
	for i, part := range record.ApprovalBody {
		messages[i] = plainText(part)
	}
	last := len(messages) - 1
	if utf16Length(messages[last].Text)+utf16Length(status)+1 <= 4096 {
		messages[last] = plainText(messages[last].Text + "\n" + status)
	}
	b.sendRenderedText(ctx, c, key, chat, messages, nil, nil)
}

// Legacy cards without a stored body can still lose their old keyboard safely
// without replacing the unknown original text or creating a new message.
func (b *Bridge) clearApprovalMarkup(ctx context.Context, c client, key string, chat int64, record delivery) {
	if b.backingOff() {
		return
	}
	if record.Keyboard == "" || len(record.IDs) == 0 {
		return
	}
	part := len(record.IDs) - 1
	if record.IDs[part] <= 0 {
		return
	}
	if len(record.Rejected) > part && record.Rejected[part] == "terminal-markup" {
		return
	}
	if err := c.EditMarkup(withDeliveryTrace(ctx, b.host.Diagnostics, key, part), chat, record.IDs[part], nil); err != nil {
		b.mu.Lock()
		b.issue = issueOf(err)
		var transport *transportError
		if errors.As(err, &transport) && definiteBotRejection(transport.code) {
			current := b.state.Messages[key]
			for len(current.Rejected) <= part {
				current.Rejected = append(current.Rejected, "")
			}
			current.Rejected[part] = "terminal-markup"
			b.state.Messages[key] = current
			_ = b.saveLocked()
		} else {
			b.issue = "delivery_uncertain"
			b.retryUntil = time.Now().Add(2 * time.Second)
		}
		b.mu.Unlock()
		return
	}
	b.mu.Lock()
	current := b.state.Messages[key]
	current.Keyboard = ""
	b.state.Messages[key] = current
	_ = b.saveLocked()
	b.mu.Unlock()
}
func approvalMessageText(a api.Approval) string {
	parts := make([]string, 0, 5)
	seen := map[string]bool{}
	for _, value := range []string{a.Title, a.Action, a.Target, a.Description, a.Details} {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			parts = append(parts, value)
			seen[value] = true
		}
	}
	return strings.Join(parts, "\n")
}
func splitText(text string) []string { return splitTextLimit(text, 4000) }

func splitTextLimit(text string, limit int) []string {
	var parts []string
	start, size := 0, 0
	for pos, r := range text {
		width := utf16.RuneLen(r)
		if size+width > limit {
			parts = append(parts, text[start:pos])
			start = pos
			size = 0
		}
		size += width
	}
	if start < len(text) {
		parts = append(parts, text[start:])
	}
	return parts
}

func utf16Length(text string) int { return len(utf16.Encode([]rune(text))) }

// macUserText labels the source without claiming to send as the Telegram user.
// Bot replies remain unformatted plain text.
func macUserText(title, body string) []outgoingText {
	header := title + "\n"
	chunks := splitTextLimit(body, 4000-utf16Length(header))
	if len(chunks) == 0 {
		chunks = []string{""} // Preserve an attachment-only Mac user item.
	}
	result := make([]outgoingText, 0, len(chunks))
	for _, chunk := range chunks {
		visible := header + chunk
		if chunk == "" {
			visible = title
		}
		message := outgoingText{
			Text:     visible,
			Entities: []tg.MessageEntity{{Type: tg.EntityTypeBold, Offset: 0, Length: utf16Length(title)}},
		}
		if chunk != "" {
			message.Entities = append(message.Entities, tg.MessageEntity{
				Type: tg.EntityTypeBlockquote, Offset: utf16Length(header), Length: utf16Length(chunk),
			})
		}
		result = append(result, message)
	}
	return result
}

func (b *Bridge) sendMacUserText(ctx context.Context, c client, key string, chat int64, body string) {
	title := b.text("You · from Mac", "你 · 来自 Mac")
	old := splitText(b.text("From Mac: ", "Mac：") + body)
	legacyParts := make([]outgoingText, len(old))
	for i, part := range old {
		legacyParts[i] = plainText(part)
	}
	b.sendRenderedText(ctx, c, key, chat, macUserText(title, body), legacyParts, nil)
}
func (b *Bridge) sendUserText(ctx context.Context, c client, key string, chat int64, body, title string) {
	b.sendRenderedText(ctx, c, key, chat, macUserText(title, body), nil, nil)
}

func (b *Bridge) sendAssistantText(ctx context.Context, c client, key string, chat int64, body string) {
	messages := assistantOutboundMessages(body)
	b.mu.Lock()
	record := b.state.Messages[key]
	b.mu.Unlock()
	// Reuse every confirmed part when a streamed reply contracts. In particular,
	// this removes the heading from each part sent by the previous formatter
	// without leaving a stale trailing Telegram message at the split boundary.
	if len(record.IDs) > len(messages) {
		confirmed := true
		for _, id := range record.IDs {
			if id <= 0 {
				confirmed = false
				break
			}
		}
		if confirmed {
			// A contracted stream must keep its confirmed IDs. For this rare
			// repartition, use plain text instead of breaking Markdown syntax.
			parts := splitTextAtLeast(splitText(body), len(record.IDs))
			messages = make([]outgoingText, len(parts))
			for i, part := range parts {
				messages[i] = plainText(part)
			}
		}
	}
	b.sendRenderedText(ctx, c, key, chat, messages, nil, nil)
}

func splitTextAtLeast(parts []string, minimum int) []string {
	for len(parts) < minimum {
		longest := -1
		for i, part := range parts {
			if utf8.RuneCountInString(part) > 1 && (longest < 0 || utf16Length(part) > utf16Length(parts[longest])) {
				longest = i
			}
		}
		if longest < 0 {
			break
		}
		part := parts[longest]
		half := utf16Length(part) / 2
		split, units := 0, 0
		for pos, r := range part {
			if units >= half && pos > 0 {
				split = pos
				break
			}
			units += utf16.RuneLen(r)
		}
		if split == 0 {
			break
		}
		parts = append(parts[:longest+1], append([]string{part[split:]}, parts[longest+1:]...)...)
		parts[longest] = part[:split]
	}
	return parts
}

func outgoingDigest(message outgoingText) string {
	if len(message.Entities) == 0 {
		return digest(message.Text) // Existing plain-text delivery digests remain valid.
	}
	encoded, _ := json.Marshal(message)
	return digest(string(encoded))
}

func (b *Bridge) sendText(ctx context.Context, c client, key string, chat int64, text string, keys *tg.InlineKeyboardMarkup) {
	parts := splitText(text)
	messages := make([]outgoingText, len(parts))
	for i, part := range parts {
		messages[i] = plainText(part)
	}
	b.sendRenderedText(ctx, c, key, chat, messages, nil, keys)
}

func (b *Bridge) sendRenderedText(ctx context.Context, c client, key string, chat int64, messages, legacy []outgoingText, keys *tg.InlineKeyboardMarkup) {
	b.sendRenderedTextPriority(ctx, c, key, chat, messages, legacy, keys, false)
}
func (b *Bridge) sendCriticalText(ctx context.Context, c client, key string, chat int64, text string, keys *tg.InlineKeyboardMarkup) {
	parts := splitText(text)
	messages := make([]outgoingText, len(parts))
	for i, part := range parts {
		messages[i] = plainText(part)
	}
	b.sendRenderedTextPriority(ctx, c, key, chat, messages, nil, keys, true)
}
func (b *Bridge) sendRenderedTextPriority(ctx context.Context, c client, key string, chat int64, messages, legacy []outgoingText, keys *tg.InlineKeyboardMarkup, priority bool) {
	if !priority && b.backingOff() {
		return
	}
	// An unchanged pre-role delivery must stay in place on upgrade/reconnect.
	// A changed streaming item can still edit its original Telegram message ID.
	b.mu.Lock()
	record := b.state.Messages[key]
	oldUnchanged := len(legacy) > 0 && len(record.IDs) == len(legacy) && len(record.Hashes) == len(legacy)
	for part, message := range legacy {
		if !oldUnchanged || record.IDs[part] <= 0 || record.Hashes[part] != outgoingDigest(message) {
			oldUnchanged = false
			break
		}
	}
	b.mu.Unlock()
	if oldUnchanged {
		return
	}
	for part, value := range messages {
		if ctx.Err() != nil {
			return
		}
		hash := outgoingDigest(value)
		var partKeys *tg.InlineKeyboardMarkup
		keyboard := ""
		if part == len(messages)-1 && keys != nil {
			partKeys = keys
			keyboard = keyboardDigest(keys)
		}
		b.mu.Lock()
		record := b.state.Messages[key]
		if record.Skip {
			b.mu.Unlock()
			return
		}
		for len(record.IDs) <= part {
			record.IDs = append(record.IDs, 0)
		}
		for len(record.Hashes) <= part {
			record.Hashes = append(record.Hashes, "")
		}
		for len(record.Rejected) <= part {
			record.Rejected = append(record.Rejected, "")
		}
		id := record.IDs[part]
		attempt := hash + ":" + keyboard
		unchanged := record.Hashes[part] == hash && (part != len(messages)-1 || record.Keyboard == keyboard)
		if id == -1 || (id == -2 || id > 0) && unchanged || record.Rejected[part] == attempt {
			b.mu.Unlock()
			continue
		}
		if id == -2 { // Revised content/keyboard may retry a proven pre-send rejection.
			id = 0
		}
		if id == 0 {
			record.IDs[part] = -1
		}
		b.state.Messages[key] = record
		e := b.saveLocked()
		b.mu.Unlock()
		if e != nil {
			return
		}
		attemptCtx := withDeliveryTrace(ctx, b.host.Diagnostics, key, part)
		if id == 0 {
			id, e = c.Send(attemptCtx, chat, value, partKeys)
		} else if record.Hashes[part] == hash {
			e = c.EditMarkup(attemptCtx, chat, id, partKeys)
		} else {
			e = c.Edit(attemptCtx, chat, id, value, partKeys)
		}
		b.mu.Lock()
		record = b.state.Messages[key]
		if e == nil {
			record.IDs[part], record.Hashes[part] = id, hash
			record.Rejected[part] = ""
			if part == len(messages)-1 {
				record.Keyboard = keyboard
			}
			if b.issue == "network" || b.issue == "rate_limited" || b.issue == "telegram_error" {
				b.issue = ""
			}
		} else {
			b.issue = issueOf(e)
			var transport *transportError
			classified := errors.As(e, &transport)
			if classified && transport.code == 429 {
				b.retryUntil = time.Now().Add(time.Duration(max(transport.retry, 1)) * time.Second)
				if record.IDs[part] < 0 {
					record.IDs[part] = 0
				}
			} else if classified && definiteBotRejection(transport.code) {
				if id == 0 {
					record.IDs[part] = -2
					record.Hashes[part] = hash
					if part == len(messages)-1 {
						record.Keyboard = keyboard
					}
				} else {
					record.Rejected[part] = attempt
				}
			} else {
				b.issue = "delivery_uncertain"
				if id > 0 {
					// Updating the same original message is idempotent display work.
					// Retry it locally; never turn it into a new Send or native decision.
					b.retryUntil = time.Now().Add(2 * time.Second)
				}
			}
		}
		b.state.Messages[key] = record
		_ = b.saveLocked()
		b.mu.Unlock()
		if e != nil {
			return
		}
	}
}

func definiteBotRejection(code int) bool {
	switch code {
	case 400, 401, 403, 404, 409:
		return true
	}
	return false // Timeouts and unrecognized responses have an unknown outcome.
}

func (b *Bridge) sendFile(ctx context.Context, c client, key, path string) {
	if ctx.Err() != nil || b.backingOff() {
		return
	}
	b.mu.Lock()
	chat := b.state.ChatID
	_, seen := b.state.Messages[key]
	if chat == 0 || seen {
		b.mu.Unlock()
		return
	}
	b.state.Messages[key] = delivery{IDs: []int{-1}}
	e := b.saveLocked()
	b.mu.Unlock()
	if e != nil {
		return
	}
	e = c.Document(ctx, chat, path)
	b.mu.Lock()
	if e != nil {
		b.issue = "file_delivery_uncertain"
		var transport *transportError
		if errors.As(e, &transport) && transport.code == 429 {
			delete(b.state.Messages, key)
			b.issue = "rate_limited"
			b.retryUntil = time.Now().Add(time.Duration(max(transport.retry, 1)) * time.Second)
		}
	} else {
		b.state.Messages[key] = delivery{Skip: true}
	}
	_ = b.saveLocked()
	b.mu.Unlock()
}

func (b *Bridge) backingOff() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().Before(b.retryUntil)
}
func (b *Bridge) flushFiles(ctx context.Context, c client) {
	b.mu.Lock()
	pending := make(map[string]string, len(b.state.PendingFiles))
	for k, v := range b.state.PendingFiles {
		pending[k] = v
	}
	b.mu.Unlock()
	for key, relative := range pending {
		if ctx.Err() != nil {
			return
		}
		if !filepath.IsLocal(relative) {
			b.setIssue("storage")
			continue
		}
		path := filepath.Join(b.root, relative)
		b.sendFile(ctx, c, key, path)
		b.mu.Lock()
		_, attempted := b.state.Messages[key]
		if attempted {
			delete(b.state.PendingFiles, key)
			_ = b.saveLocked()
		}
		b.mu.Unlock()
		if attempted {
			os.Remove(path)
			os.Remove(filepath.Dir(path))
			os.Remove(filepath.Dir(filepath.Dir(path)))
		}
	}
}
