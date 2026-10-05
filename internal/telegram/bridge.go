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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	tg "github.com/mymmrac/telego"
)

type Host struct {
	Snapshot    func() api.Snapshot
	Submit      func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)
	Interrupt   func(context.Context) error
	Decide      func(context.Context, api.Decision) error
	Artifact    func(string) (string, error)
	ScreenImage func(string) ([]byte, error)
	Chinese     func() bool
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
	IDs    []int    `json:"ids,omitempty"`
	Hashes []string `json:"hashes,omitempty"`
	Skip   bool     `json:"skip,omitempty"`
}
type document struct {
	Version      int                 `json:"version"`
	Enabled      bool                `json:"enabled"`
	BotID        int64               `json:"botId"`
	Bot          string              `json:"bot"`
	ChatID       int64               `json:"chatId"`
	UserID       int64               `json:"userId"`
	Owner        string              `json:"owner"`
	Offset       int                 `json:"offset"`
	Inputs       map[string]string   `json:"inputs"` // Original stable request and native receipt outcome only.
	PendingFiles map[string]string   `json:"pendingFiles,omitempty"`
	Messages     map[string]delivery `json:"messages"` // Telegram IDs and digests, never transcript text.
}
type Bridge struct {
	configMu            sync.Mutex
	mu                  sync.Mutex
	path, root, account string
	host                Host
	state               document
	issue               string
	candidate           *tg.Message
	nonce               string
	expires             time.Time
	cancel              context.CancelFunc
	done                chan struct{}
	newClient           func(string) (client, error)
	saveSecret          func(string, string) error
	loadSecret          func(string) (string, error)
	deleteSecret        func(string) error
	retryUntil          time.Time
	closed              bool
}

func Open(root string, host Host) (*Bridge, error) {
	b := &Bridge{path: filepath.Join(root, "telegram.json"), root: filepath.Join(root, "Telegram"), account: digest(root), host: host, newClient: newClient, saveSecret: saveSecret, loadSecret: loadSecret, deleteSecret: deleteSecret}
	b.state = document{Version: 1, Inputs: map[string]string{}, Messages: map[string]delivery{}}
	data, e := os.ReadFile(b.path)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, errors.New("telegram_state_unavailable")
	}
	if e == nil && (len(data) > 8<<20 || json.Unmarshal(data, &b.state) != nil || b.state.Version != 1 || b.state.Inputs == nil || b.state.Messages == nil) {
		return nil, errors.New("telegram_state_unavailable")
	}
	if b.state.PendingFiles == nil {
		b.state.PendingFiles = map[string]string{}
	}
	return b, nil
}
func digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func (b *Bridge) saveLocked() error {
	if e := localstate.Write(b.path, b.state); e != nil {
		b.issue = "storage"
		return errors.New("storage")
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
	enabled := b.state.Enabled
	b.mu.Unlock()
	if enabled {
		b.resume()
	}
}
func (b *Bridge) resume() {
	token, e := b.loadSecret(b.account)
	if e != nil {
		b.setIssue("keychain")
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
		token, e = b.loadSecret(b.account)
		if e != nil {
			b.setIssue("keychain")
			return b.Status(), errors.New("keychain")
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
	if e = b.saveSecret(b.account, token); e != nil {
		b.setIssue("keychain")
		return b.Status(), errors.New("keychain")
	}
	b.mu.Lock()
	if b.state.BotID != me.ID {
		b.state = document{Version: 1, Inputs: map[string]string{}, Messages: map[string]delivery{}}
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
	if e := b.deleteSecret(b.account); e != nil {
		return b.Status(), errors.New("keychain")
	}
	b.mu.Lock()
	b.state = document{Version: 1, Inputs: map[string]string{}, Messages: map[string]delivery{}}
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
	// Recovery must establish the private history boundary before any remote
	// submission can create a reply. Telegram retains updates until we poll them.
	if !b.waitBaseline(ctx) {
		return
	}
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
	defer func() {
		cancel()
		<-pollDone
		<-outputDone
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
			for _, u := range r.updates {
				if ctx.Err() != nil {
					return
				}
				if !b.input(ctx, c, u) {
					return
				}
				offset = u.UpdateID + 1 // Telegram can randomize the sequence after a quiet week.
				b.mu.Lock()
				b.state.Offset = offset
				e := b.saveLocked()
				b.mu.Unlock()
				if e != nil {
					return
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

func (b *Bridge) waitBaseline(ctx context.Context) bool {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		s := b.host.Snapshot()
		if ready(s) {
			b.mu.Lock()
			b.baselineLocked(s)
			err := b.saveLocked()
			b.mu.Unlock()
			return err == nil
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
	return false
}

// One output worker bounds concurrency and reads the newest snapshot on each
// tick, coalescing stream edits without accumulating work. Slow sends/uploads
// cannot block incoming stop, status or approval actions. Both workers join on
// cancellation before the lifecycle owner replaces their configuration.
func (b *Bridge) output(ctx context.Context, c client) {
	_ = c.Commands(ctx)
	ticker := time.NewTicker(1100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil || b.backingOff() {
				continue
			}
			b.mirror(ctx, c, b.host.Snapshot())
			if ctx.Err() == nil {
				b.flushFiles(ctx, c)
			}
		}
	}
}
func (b *Bridge) input(ctx context.Context, c client, u tg.Update) bool {
	if q := u.CallbackQuery; q != nil {
		return b.callback(ctx, c, q)
	}
	m := u.Message
	if m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != "private" {
		return true
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
			_, _ = c.Send(ctx, m.Chat.ID, b.text("Confirm this account in Caelis Bot on your Mac to finish connecting.", "请在 Mac 的 Caelis Bot 中确认这是你的账号，即可完成连接。"), nil)
		}
		return true
	}
	if m.Chat.ID != chat || m.From.ID != owner {
		return true
	}
	request := fmt.Sprintf("telegram:%d:%d", bot, m.MessageID)
	b.mu.Lock()
	_, exists := b.state.Inputs[request]
	if !exists {
		b.state.Inputs[request] = "dispatching"
		// Output may observe the native user item while Submit is still running.
		// Record its Telegram origin before dispatch so it cannot echo back as Mac input.
		b.state.Messages["input:"+request] = delivery{Skip: true}
		if b.saveLocked() != nil {
			b.mu.Unlock()
			return false
		}
	}
	b.mu.Unlock()
	if exists {
		return true
	}
	outcome := "handled"
	switch strings.Split(m.Text, " ")[0] {
	case "/start":
		_, _ = c.Send(ctx, chat, b.text("Connected. Send a message or file. /stop stops work; /status checks it.", "已连接。直接发送消息或文件即可。/stop 停止工作，/status 查看状态。"), nil)
	case "/stop":
		if b.host.Interrupt(ctx) != nil {
			_, _ = c.Send(ctx, chat, b.text("Could not stop work. Check Caelis Bot on your Mac.", "未能停止，请在 Mac 的 Caelis Bot 中查看。"), nil)
		}
	case "/status":
		s := b.host.Snapshot()
		text := b.text("Caelis Bot is online.", "Caelis Bot 在线。")
		if s.CanInterrupt {
			text = b.text("Caelis Bot is working.", "Caelis Bot 正在工作。")
		}
		_, _ = c.Send(ctx, chat, text, nil)
	default:
		files, e := b.download(ctx, c, request, m)
		if e != nil {
			outcome = "rejected"
			_, _ = c.Send(ctx, chat, b.text("The attachment could not be received. Send one file of up to 8 MB, or send text.", "附件未能接收。请发送单个不超过 8 MB 的文件，或直接发送文字。"), nil)
		} else {
			text := m.Text
			if text == "" {
				text = m.Caption
			}
			if strings.TrimSpace(text) == "" && len(files) == 0 {
				outcome = "rejected"
				_, _ = c.Send(ctx, chat, b.text("Send text, a photo or a file.", "请发送文字、图片或文件。"), nil)
			} else {
				receipt, _ := b.host.Submit(ctx, api.Submission{ID: request, Text: text}, files)
				outcome = receipt.Outcome
				if outcome == "" {
					outcome = "unknown"
				}
				if outcome != "accepted" {
					notice := b.text("The message was not accepted. Check Caelis Bot on your Mac before resending.", "消息尚未被接收，请先在 Mac 的 Caelis Bot 中查看后再决定是否重发。")
					if outcome == "unknown" {
						notice = b.text("Delivery is uncertain. Check the original message on your Mac; it will not be sent again automatically.", "发送结果暂不确定。请在 Mac 查看原消息，系统不会自动重复发送。")
					}
					_, _ = c.Send(ctx, chat, notice, nil)
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
	return e == nil
}
func (b *Bridge) download(ctx context.Context, c client, request string, m *tg.Message) ([]api.InputFile, error) {
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
		return nil, nil
	}
	if size > maxInputBytes {
		return nil, errors.New("file_too_large")
	}
	dir := filepath.Join(b.root, "incoming", digest(request))
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(dir, safeName(name))
	if e := c.Download(ctx, id, path); e != nil {
		return nil, e
	}
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > maxInputBytes {
		return nil, errors.New("file_too_large")
	}
	return []api.InputFile{{Name: filepath.Base(path), Path: path}}, nil
}
func callbackID(id, choice string) string { return "a:" + digest(id + "\x00" + choice)[:40] }
func (b *Bridge) callback(ctx context.Context, c client, q *tg.CallbackQuery) bool {
	b.mu.Lock()
	owner, chat := b.state.UserID, b.state.ChatID
	b.mu.Unlock()
	m, ok := q.Message.(*tg.Message)
	if !ok || q.From.ID != owner || m.Chat.ID != chat {
		return true
	}
	for _, a := range b.host.Snapshot().Approvals {
		if a.Status == "resolved" || len(a.Questions) > 0 || a.URL != "" {
			continue
		}
		for _, choice := range a.Choices {
			if q.Data == callbackID(a.ID, choice.ID) {
				key := "callback:" + q.ID
				b.mu.Lock()
				_, used := b.state.Inputs[key]
				if !used {
					b.state.Inputs[key] = "dispatching"
					if b.saveLocked() != nil {
						b.mu.Unlock()
						return false
					}
				}
				b.mu.Unlock()
				if used {
					return true
				}
				e := b.host.Decide(ctx, api.Decision{ID: a.ID, Choice: choice.ID})
				notice := b.text("Done.", "已处理。")
				outcome := "handled"
				if e != nil {
					notice = b.text("Check this request on your Mac.", "请在 Mac 查看此请求。")
					outcome = "unknown"
				}
				_ = c.Answer(ctx, q.ID, notice)
				b.mu.Lock()
				b.state.Inputs[key] = outcome
				e = b.saveLocked()
				b.mu.Unlock()
				return e == nil
			}
		}
	}
	_ = c.Answer(ctx, q.ID, b.text("This request has changed. Check your Mac.", "此请求已变化，请在 Mac 查看。"))
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
		if i.Text == api.SilentReminder {
			continue
		}
		text := i.Text
		if i.Kind == "user" {
			text = b.text("From Mac: ", "Mac：") + text
		}
		if i.Kind == "user" || i.Kind == "assistant" {
			b.sendText(ctx, c, itemKey(i), chat, text, nil)
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
	if s.LastReceipt.ID != "" {
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
			b.sendText(ctx, c, "status:"+s.LastReceipt.ID+":"+s.Phase, chat, notice, nil)
		}
	}

	for _, a := range s.Approvals {
		if a.Status == "resolved" {
			continue
		}
		text := strings.Join([]string{a.Title, a.Action, a.Target, a.Description, a.Details}, "\n")
		var keys *tg.InlineKeyboardMarkup
		if len(a.Questions) == 0 && a.URL == "" {
			keys = &tg.InlineKeyboardMarkup{}
			for _, choice := range a.Choices {
				keys.InlineKeyboard = append(keys.InlineKeyboard, []tg.InlineKeyboardButton{{Text: choice.Label, CallbackData: callbackID(a.ID, choice.ID)}})
			}
		} else {
			text += "\n" + b.text("Complete this request in Caelis Bot on your Mac.", "请在 Mac 的 Caelis Bot 中完成此请求。")
		}
		b.sendText(ctx, c, "approval:"+a.ID, chat, text, keys)
	}
}
func splitText(text string) []string {
	var parts []string
	start, size := 0, 0
	for pos, r := range text {
		width := utf16.RuneLen(r)
		if size+width > 4000 {
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
func (b *Bridge) sendText(ctx context.Context, c client, key string, chat int64, text string, keys *tg.InlineKeyboardMarkup) {
	if b.backingOff() {
		return
	}
	for part, value := range splitText(text) {
		if ctx.Err() != nil {
			return
		}
		hash := digest(value)
		b.mu.Lock()
		record := b.state.Messages[key]
		if record.Skip {
			b.mu.Unlock()
			return
		}
		for len(record.IDs) <= part {
			record.IDs = append(record.IDs, 0)
			record.Hashes = append(record.Hashes, "")
		}
		id := record.IDs[part]
		if id < 0 || record.Hashes[part] == hash {
			b.mu.Unlock()
			continue
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
		if id == 0 {
			id, e = c.Send(ctx, chat, value, keys)
		} else {
			e = c.Edit(ctx, chat, id, value)
		}
		b.mu.Lock()
		record = b.state.Messages[key]
		if e == nil {
			record.IDs[part], record.Hashes[part] = id, hash
			if b.issue == "network" || b.issue == "rate_limited" {
				b.issue = ""
			}
		} else {
			b.issue = issueOf(e)
			var transport *transportError
			if errors.As(e, &transport) && transport.code == 429 {
				b.retryUntil = time.Now().Add(time.Duration(max(transport.retry, 1)) * time.Second)
				if record.IDs[part] < 0 {
					record.IDs[part] = 0
				}
			} else if id == 0 || record.IDs[part] < 0 {
				b.issue = "delivery_uncertain"
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
