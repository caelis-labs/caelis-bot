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
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

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
	IDs      []int    `json:"ids,omitempty"`
	Hashes   []string `json:"hashes,omitempty"`
	Keyboard string   `json:"keyboard,omitempty"`
	Skip     bool     `json:"skip,omitempty"`
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
			_, _ = c.Send(ctx, m.Chat.ID, plainText(b.text("Confirm this account in Caelis Bot on your Mac to finish connecting.", "请在 Mac 的 Caelis Bot 中确认这是你的账号，即可完成连接。")), nil)
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
		_, _ = c.Send(ctx, chat, plainText(b.text("Connected. Send a message or file. /stop stops work; /status checks it.", "已连接。直接发送消息或文件即可。/stop 停止工作，/status 查看状态。")), nil)
	case "/stop":
		if b.host.Interrupt(ctx) != nil {
			_, _ = c.Send(ctx, chat, plainText(b.text("Could not stop work. Check Caelis Bot on your Mac.", "未能停止，请在 Mac 的 Caelis Bot 中查看。")), nil)
		}
	case "/status":
		s := b.host.Snapshot()
		text := b.text("Caelis Bot is online.", "Caelis Bot 在线。")
		if s.CanInterrupt {
			text = b.text("Caelis Bot is working.", "Caelis Bot 正在工作。")
		}
		_, _ = c.Send(ctx, chat, plainText(text), nil)
	default:
		files, stickerNote, e := b.download(ctx, c, request, m)
		if e != nil {
			outcome = "rejected"
			_, _ = c.Send(ctx, chat, plainText(b.downloadNotice(e)), nil)
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
				_, _ = c.Send(ctx, chat, plainText(b.text("Send text, a photo or a file.", "请发送文字、图片或文件。")), nil)
			} else {
				receipt, submitErr := b.host.Submit(ctx, api.Submission{ID: request, Text: text}, files)
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
					_, _ = c.Send(ctx, chat, plainText(notice), nil)
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
				key := "approval-decision:" + a.ID
				b.mu.Lock()
				record := b.state.Messages["approval:"+a.ID]
				fromButton := len(record.IDs) > 0 && record.IDs[len(record.IDs)-1] > 0 && record.IDs[len(record.IDs)-1] == m.MessageID
				_, used := b.state.Inputs[key]
				if fromButton && !used {
					b.state.Inputs[key] = "dispatching"
					if b.saveLocked() != nil {
						b.mu.Unlock()
						return false
					}
				}
				b.mu.Unlock()
				if used || !fromButton {
					_ = c.Answer(ctx, q.ID, b.text("This request has changed. Check your Mac.", "此请求已变化，请在 Mac 查看。"))
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
			b.sendMacUserText(ctx, c, itemKey(i), chat, i.Text)
		} else if i.Kind == "assistant" {
			b.sendAssistantText(ctx, c, itemKey(i), chat, i.Text)
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

	for _, a := range s.Approvals {
		if a.Status == "resolved" {
			continue
		}
		text := approvalMessageText(a)
		if text == "" {
			text = b.text("Decision needed.", "需要决定。")
		}
		var keys *tg.InlineKeyboardMarkup
		if len(a.Questions) == 0 && a.URL == "" && len(a.Choices) > 0 {
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

func (b *Bridge) sendAssistantText(ctx context.Context, c client, key string, chat int64, body string) {
	parts := splitText(body)
	b.mu.Lock()
	record := b.state.Messages[key]
	b.mu.Unlock()
	// Reuse every confirmed part when a streamed reply contracts. In particular,
	// this removes the heading from each part sent by the previous formatter
	// without leaving a stale trailing Telegram message at the split boundary.
	if len(record.IDs) > len(parts) {
		confirmed := true
		for _, id := range record.IDs {
			if id <= 0 {
				confirmed = false
				break
			}
		}
		if confirmed {
			parts = splitTextAtLeast(parts, len(record.IDs))
		}
	}
	messages := make([]outgoingText, len(parts))
	for i, part := range parts {
		messages[i] = plainText(part)
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
	if b.backingOff() {
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
			encoded, _ := json.Marshal(keys)
			keyboard = digest(string(encoded))
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
		id := record.IDs[part]
		if id < 0 || record.Hashes[part] == hash && (part != len(messages)-1 || record.Keyboard == keyboard) {
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
			id, e = c.Send(ctx, chat, value, partKeys)
		} else if record.Hashes[part] == hash {
			e = c.EditMarkup(ctx, chat, id, partKeys)
		} else {
			e = c.Edit(ctx, chat, id, value, partKeys)
		}
		b.mu.Lock()
		record = b.state.Messages[key]
		if e == nil {
			record.IDs[part], record.Hashes[part] = id, hash
			if part == len(messages)-1 {
				record.Keyboard = keyboard
			}
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
