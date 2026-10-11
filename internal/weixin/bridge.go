package weixin

import (
	"context"
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

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/secretstore"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
	qrcode "github.com/skip2/go-qrcode"
)

type Host struct {
	Snapshot    func() api.Snapshot
	Recovery    func() api.RecoveryState
	Submit      func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)
	TextControl *textchannel.Store
	RecordInput func(textchannel.Inbound, bool)
}
type Status struct {
	Enabled   bool   `json:"enabled"`
	Paired    bool   `json:"paired"`
	Phase     string `json:"phase"`
	Owner     string `json:"owner"`
	Bot       string `json:"bot"`
	Issue     string `json:"issue"`
	QRImage   string `json:"qrImage"`
	ExpiresAt int64  `json:"expiresAt"`
}
type inbound struct {
	ID           string             `json:"id"`
	Text         string             `json:"text"`
	ContextToken string             `json:"contextToken"`
	Secret       bool               `json:"secret,omitempty"` // body is held only in memory
	PromptID     string             `json:"promptId,omitempty"`
	ReferenceID  string             `json:"referenceId,omitempty"`
	Quoted       *api.QuotedMessage `json:"quoted,omitempty"`
}
type outbound struct {
	State        string `json:"state"`
	ClientID     string `json:"clientId,omitempty"`
	ContextToken string `json:"contextToken,omitempty"`
	Reason       string `json:"reason,omitempty"`
	ErrorClass   string `json:"errorClass,omitempty"`
	Ret          *int   `json:"ret,omitempty"`
	ErrCode      *int   `json:"errcode,omitempty"`
	Bytes        int    `json:"bytes,omitempty"`
	Units        int    `json:"units,omitempty"`
	InputAgeSec  int64  `json:"inputAgeSec,omitempty"`
	WindowUsed   int    `json:"windowUsed,omitempty"`
	Digest       string `json:"digest,omitempty"`
	MessageID    string `json:"messageId,omitempty"`
	Attempts     int    `json:"attempts,omitempty"`
	NextRetryAt  int64  `json:"nextRetryAt,omitempty"`
}
type sendWindow struct {
	OwnerID      string `json:"ownerId,omitempty"`
	InputID      string `json:"inputId,omitempty"`
	ContextToken string `json:"contextToken,omitempty"`
	InputAt      int64  `json:"inputAt,omitempty"`
	Used         int    `json:"used,omitempty"`
	Exhausted    bool   `json:"exhausted,omitempty"`
}
type document struct {
	Version        int                 `json:"version"`
	Enabled        bool                `json:"enabled"`
	BotID          string              `json:"botId"`
	OwnerID        string              `json:"ownerId"`
	BaseURL        string              `json:"baseURL"`
	Cursor         string              `json:"cursor"`
	HasInput       bool                `json:"hasInput"`
	ContextToken   string              `json:"contextToken,omitempty"`
	Window         sendWindow          `json:"window,omitempty"`
	PendingFinal   string              `json:"pendingFinal,omitempty"`
	Missed         int                 `json:"missed,omitempty"`
	TurnPhases     map[string]string   `json:"turnPhases,omitempty"`
	TurnOrder      []string            `json:"turnOrder,omitempty"`
	PauseUntil     int64               `json:"pauseUntil,omitempty"`
	InputContexts  map[string]string   `json:"inputContexts,omitempty"`
	Inbox          []inbound           `json:"inbox,omitempty"`
	Inputs         map[string]string   `json:"inputs"`
	Outputs        map[string]outbound `json:"outputs"`
	MirrorV2       bool                `json:"mirrorV2,omitempty"`
	LocalIMSeeded  bool                `json:"localImSeeded,omitempty"`
	SecretMessages map[string]bool     `json:"secretMessages,omitempty"`
}
type Bridge struct {
	configMu                      sync.Mutex
	mu                            sync.Mutex
	path, account                 string
	host                          Host
	state                         document
	secrets                       secretstore.Store
	client                        *protocol
	issue, phase, qrImage, qrCode string
	verifyCh                      chan string
	pairGeneration                uint64
	expires                       time.Time
	candidate                     *qrResult
	pairCancel                    context.CancelFunc
	cancel                        context.CancelFunc
	done                          chan struct{}
	closed                        bool
	loadErr                       bool
	secretInbox                   map[string]string
}

func emptyDocument() document {
	return document{Version: 1, Inputs: map[string]string{}, Outputs: map[string]outbound{}, InputContexts: map[string]string{}, SecretMessages: map[string]bool{}, TurnPhases: map[string]string{}, MirrorV2: true, LocalIMSeeded: true}
}
func Open(root string, host Host) (*Bridge, error) {
	hash := sha256.Sum256([]byte(root))
	account := hex.EncodeToString(hash[:])
	b := &Bridge{path: filepath.Join(root, "weixin.json"), account: account, host: host, state: emptyDocument(), secretInbox: map[string]string{}, secrets: &secretstore.FileStore{Root: filepath.Join(root, "Credentials"), Namespace: "weixin", Legacy: secretstore.Functions{LoadFunc: loadSecret, DeleteFunc: deleteSecret}}}
	f, err := os.Open(b.path)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		b.issue = "storage"
		b.loadErr = true
		return b, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 4<<20))
	if dec.Decode(&b.state) != nil || dec.Decode(new(any)) != io.EOF || b.state.Version != 1 || b.state.Inputs == nil || b.state.Outputs == nil {
		b.state = emptyDocument()
		b.issue = "storage"
		b.loadErr = true
		return b, errors.New("weixin_state_unavailable")
	}
	if b.state.BotID != "" {
		if _, err := safeBase(b.state.BaseURL); err != nil {
			b.issue = "storage"
			b.loadErr = true
			return b, err
		}
	}
	if b.state.InputContexts == nil {
		b.state.InputContexts = map[string]string{}
	}
	if b.state.SecretMessages == nil {
		b.state.SecretMessages = map[string]bool{}
	}
	if b.state.TurnPhases == nil {
		b.state.TurnPhases = map[string]string{}
	}
	if b.state.PauseUntil > time.Now().UnixMilli() {
		b.issue = "session_cooldown"
	}
	return b, nil
}
func (b *Bridge) saveLocked() error {
	if b.loadErr {
		return errors.New("storage")
	}
	if err := localstate.WriteConfirmed(b.path, b.state); err != nil {
		b.issue = "storage"
		return errors.New("storage")
	}
	if b.issue == "storage" {
		b.issue = ""
	}
	return nil
}
func mask(id string) string {
	if len(id) <= 8 {
		return "••••"
	}
	return id[:4] + "…" + id[len(id)-4:]
}
func (b *Bridge) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	phase := b.phase
	if b.state.BotID != "" {
		if b.state.Enabled {
			phase = "connected"
		} else {
			phase = "paused"
		}
	}
	if phase == "" {
		phase = "unconfigured"
	}
	if b.issue == "auth_expired" || b.issue == "session_cooldown" {
		phase = "attention"
	}
	if b.qrCode != "" && time.Now().After(b.expires) && b.candidate == nil {
		phase = "expired"
	}
	owner := b.state.OwnerID
	if b.candidate != nil {
		owner = b.candidate.UserID
	}
	return Status{Enabled: b.state.Enabled, Paired: b.state.BotID != "", Phase: phase, Owner: mask(owner), Bot: mask(b.state.BotID), Issue: b.issue, QRImage: b.qrImage, ExpiresAt: b.expires.UnixMilli()}
}
func (b *Bridge) StartPairing(ctx context.Context) (Status, error) {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	if b.closed || b.loadErr || b.state.BotID != "" {
		b.mu.Unlock()
		return b.Status(), errors.New("unavailable")
	}
	if b.pairCancel != nil {
		b.pairCancel()
	}
	b.pairGeneration++
	b.pairCancel = nil
	b.verifyCh = make(chan string, 1)
	b.candidate = nil
	b.qrCode = ""
	b.qrImage = ""
	b.phase = "pairing"
	b.issue = ""
	b.mu.Unlock()
	p, _ := newProtocol(defaultBase, "", nil)
	work, stop := context.WithTimeout(ctx, 15*time.Second)
	defer stop()
	qr, err := p.qr(work)
	if err != nil {
		b.setIssue("network")
		return b.Status(), errors.New("network")
	}
	png, err := qrcode.Encode(qr.Content, qrcode.Medium, 280)
	if err != nil {
		b.setIssue("pairing_failed")
		return b.Status(), errors.New("pairing_failed")
	}
	pairCtx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.qrCode = qr.Code
	b.qrImage = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	b.expires = time.Now().Add(5 * time.Minute)
	b.pairCancel = cancel
	verifyCh := b.verifyCh
	generation := b.pairGeneration
	b.mu.Unlock()
	go b.pollPair(pairCtx, p, qr.Code, verifyCh, generation)
	return b.Status(), nil
}
func (b *Bridge) pollPair(ctx context.Context, p *protocol, code string, verifyCh <-chan string, generation uint64) {
	verify := ""
	for ctx.Err() == nil {
		b.mu.Lock()
		if b.pairGeneration != generation {
			b.mu.Unlock()
			return
		}
		expires := b.expires
		b.mu.Unlock()
		if time.Now().After(expires) {
			b.mu.Lock()
			if b.pairGeneration == generation {
				b.phase = "expired"
				b.qrImage = ""
			}
			b.mu.Unlock()
			return
		}
		work, stop := context.WithTimeout(ctx, 38*time.Second)
		result, err := p.qrStatus(work, code, verify)
		verify = ""
		stop()
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			continue
		}
		if err != nil {
			b.mu.Lock()
			if b.pairGeneration == generation {
				b.issue = "network"
			}
			b.mu.Unlock()
			sleep(ctx, 2*time.Second)
			continue
		}
		b.mu.Lock()
		if b.pairGeneration != generation {
			b.mu.Unlock()
			return
		}
		b.issue = ""
		switch result.Status {
		case "wait":
		case "scaned":
			b.phase = "scanned"
		case "need_verifycode":
			b.phase = "verify"
			b.mu.Unlock()
			select {
			case verify = <-verifyCh:
			case <-ctx.Done():
				return
			case <-time.After(time.Until(expires)):
				return
			}
			continue
		case "verify_code_blocked":
			b.phase = "blocked"
			b.mu.Unlock()
			return
		case "expired":
			b.phase = "expired"
			b.qrImage = ""
			b.mu.Unlock()
			return
		case "binded_redirect":
			b.phase = "bound_elsewhere"
			b.mu.Unlock()
			return
		case "scaned_but_redirect":
			base, err := safeBase("https://" + result.RedirectHost)
			if err != nil {
				b.issue = "untrusted_api_host"
				b.mu.Unlock()
				return
			}
			p.base = base
			b.phase = "scanned"
		case "confirmed":
			if result.BaseURL == "" {
				result.BaseURL = p.base
			}
			base, err := safeBase(result.BaseURL)
			if err != nil || result.Token == "" || result.BotID == "" || result.UserID == "" {
				b.issue = "pairing_failed"
				b.mu.Unlock()
				return
			}
			result.BaseURL = base
			b.candidate = &result
			b.phase = "confirm"
			b.qrImage = ""
			b.mu.Unlock()
			return
		default:
			b.issue = "pairing_failed"
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
	}
}
func (b *Bridge) setIssue(issue string) { b.mu.Lock(); b.issue = issue; b.mu.Unlock() }
func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
func (b *Bridge) Verify(code string) (Status, error) {
	code = strings.TrimSpace(code)
	if len(code) < 3 || len(code) > 32 {
		return b.Status(), errors.New("invalid_verify_code")
	}
	b.mu.Lock()
	if b.phase != "verify" || b.candidate != nil {
		b.mu.Unlock()
		return b.Status(), errors.New("verification_unavailable")
	}
	b.phase = "scanned"
	select {
	case b.verifyCh <- code:
	default:
	}
	b.mu.Unlock()
	return b.Status(), nil
}
func (b *Bridge) Confirm() (Status, error) {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	candidate := b.candidate
	if candidate == nil || b.closed {
		b.mu.Unlock()
		return b.Status(), errors.New("pairing_unavailable")
	}
	initial := b.host.Snapshot()
	state := emptyDocument()
	state.Enabled = true
	state.BotID = candidate.BotID
	state.OwnerID = candidate.UserID
	state.BaseURL = candidate.BaseURL
	for _, item := range initial.Items {
		if (item.Kind == "assistant" || item.Kind == "user") && item.ID != "" {
			state.Outputs["item:"+item.ID] = outbound{State: "skip"}
		}
	}
	for _, approval := range initial.Approvals {
		if approval.Status == "resolved" {
			state.Outputs["approval-result:"+approval.ID] = outbound{State: "skip"}
		}
	}
	if b.host.TextControl != nil {
		for _, notice := range b.host.TextControl.Notices() {
			state.Outputs["notice:"+notice.ID] = outbound{State: "skip"}
		}
	}
	if err := b.secrets.Save(b.account, candidate.Token); err != nil {
		b.issue = "credential"
		b.mu.Unlock()
		return b.Status(), errors.New("credential")
	}
	previous := b.state
	b.state = state
	if err := b.saveLocked(); err != nil {
		b.state = previous
		_ = b.secrets.Delete(b.account)
		b.mu.Unlock()
		return b.Status(), err
	}
	b.candidate = nil
	b.qrCode = ""
	b.qrImage = ""
	b.phase = "connected"
	b.issue = ""
	b.mu.Unlock()
	b.startLocked()
	return b.Status(), nil
}
func (b *Bridge) Start() { b.configMu.Lock(); defer b.configMu.Unlock(); b.startLocked() }
func (b *Bridge) startLocked() {
	b.mu.Lock()
	if b.closed || !b.state.Enabled || b.state.BotID == "" || b.cancel != nil || b.issue == "storage" {
		b.mu.Unlock()
		return
	}
	base := b.state.BaseURL
	b.mu.Unlock()
	token, err := b.secrets.Load(b.account)
	if err != nil {
		b.setIssue("credential")
		return
	}
	p, err := newProtocol(base, token, nil)
	if err != nil {
		b.setIssue("storage")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	b.mu.Lock()
	b.client = p
	b.cancel = cancel
	b.done = done
	b.mu.Unlock()
	go func() {
		b.mu.Lock()
		pauseUntil := b.state.PauseUntil
		b.mu.Unlock()
		if remaining := time.Until(time.UnixMilli(pauseUntil)); remaining > 0 {
			sleep(ctx, remaining)
		}
		if ctx.Err() != nil {
			close(done)
			return
		}
		work, stop := context.WithTimeout(ctx, 4*time.Second)
		_ = p.notify(work, true)
		stop()
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); b.receive(ctx, p) }()
		go func() { defer wg.Done(); b.process(ctx, p) }()
		go func() { defer wg.Done(); b.typing(ctx, p) }()
		wg.Wait()
		close(done)
	}()
}
func (b *Bridge) stopLocked() {
	b.mu.Lock()
	cancel, done, client := b.cancel, b.done, b.client
	b.cancel = nil
	b.done = nil
	b.client = nil
	b.pairGeneration++
	if b.pairCancel != nil {
		b.pairCancel()
		b.pairCancel = nil
	}
	b.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
		work, stop := context.WithTimeout(context.Background(), 4*time.Second)
		_ = client.notify(work, false)
		stop()
	}
	b.mu.Lock()
	b.secretInbox = map[string]string{} // a later resume asks the owner to re-enter secret answers
	b.mu.Unlock()
}
func (b *Bridge) Pause() (Status, error) {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.stopLocked()
	b.mu.Lock()
	b.state.Enabled = false
	err := b.saveLocked()
	b.mu.Unlock()
	return b.Status(), err
}
func (b *Bridge) Resume() (Status, error) {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.stopLocked()
	b.mu.Lock()
	if b.state.BotID == "" {
		b.mu.Unlock()
		return b.Status(), errors.New("unpaired")
	}
	previousEnabled := b.state.Enabled
	b.state.Enabled = true
	err := b.saveLocked()
	if err == nil {
		b.issue = ""
	} else {
		b.state.Enabled = previousEnabled
	}
	b.mu.Unlock()
	if err == nil {
		b.startLocked()
	}
	return b.Status(), err
}
func (b *Bridge) Forget() (Status, error) {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.stopLocked()
	if err := b.secrets.Delete(b.account); err != nil {
		b.setIssue("credential")
		return b.Status(), errors.New("credential")
	}
	b.mu.Lock()
	previous := b.state
	b.state = emptyDocument()
	err := b.saveLocked()
	if err != nil {
		b.state = previous
	}
	b.candidate = nil
	b.qrCode = ""
	b.qrImage = ""
	b.phase = ""
	b.mu.Unlock()
	if err != nil {
		return b.Status(), err
	}
	return b.Status(), nil
}
func (b *Bridge) Close() {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.stopLocked()
}

func textOf(msg message) string {
	var parts []string
	for _, item := range msg.Items {
		if item.Type == 1 && item.Text != nil && item.Text.Text != "" {
			parts = append(parts, item.Text.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
func referenceOf(msg message) string {
	var id string
	for _, item := range msg.Items {
		if item.Type != 1 || item.Text == nil || item.Text.Text == "" {
			continue
		}
		if item.Ref == nil || item.Ref.ServerID == "" {
			return ""
		}
		if id != "" && id != string(item.Ref.ServerID) {
			return ""
		}
		id = string(item.Ref.ServerID)
	}
	return id
}
func (b *Bridge) quotedMessageLocked(msg message) *api.QuotedMessage {
	var ref *refItem
	for _, item := range msg.Items {
		if item.Type == 1 && item.Text != nil && item.Text.Text != "" && item.Ref != nil {
			if ref != nil {
				return nil
			} // several fragments are not one quote
			ref = item.Ref
		}
	}
	if ref == nil {
		return nil
	}
	id := string(ref.ServerID)
	if id != "" && b.state.SecretMessages[id] {
		return nil
	}
	q := &api.QuotedMessage{HostID: id, Role: "unknown", Excerpt: true}
	if ref.Item != nil && ref.Item.Type == 1 && ref.Item.Text != nil {
		q.Text = ref.Item.Text.Text
	}
	if q.Text == "" {
		q.Text = ref.Title
	}
	if b.host.TextControl != nil && b.host.TextControl.IsSecretCommand(q.Text) {
		return nil
	}
	if id != "" {
		for key, out := range b.state.Outputs {
			if out.State == "accepted" && out.MessageID == id {
				q.Role, q.LocalID = "assistant", key
				break
			}
		}
		if q.LocalID == "" {
			key := "weixin:" + b.state.BotID + ":" + id
			if _, exists := b.state.Inputs[key]; exists {
				q.Role, q.LocalID = "user", key
			}
		}
	}
	return api.BoundQuote(q)
}
func (b *Bridge) receive(ctx context.Context, p *protocol) {
	backoff := time.Second
	pollTimeout := 40 * time.Second
	for ctx.Err() == nil {
		b.mu.Lock()
		cursor := b.state.Cursor
		pauseUntil := b.state.PauseUntil
		b.mu.Unlock()
		if remaining := time.Until(time.UnixMilli(pauseUntil)); remaining > 0 {
			sleep(ctx, remaining)
			continue
		}
		work, stop := context.WithTimeout(ctx, pollTimeout)
		result, err := p.getUpdates(work, cursor)
		stop()
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			continue
		}
		if err != nil {
			b.setIssue("network")
			sleep(ctx, backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		if result.Ret != 0 || result.ErrCode != 0 {
			if result.Ret == -14 || result.ErrCode == -14 {
				b.mu.Lock()
				b.state.PauseUntil = time.Now().Add(time.Hour).UnixMilli()
				b.issue = "session_cooldown"
				_ = b.saveLocked()
				b.mu.Unlock()
				continue
			}
			b.setIssue("remote_error")
			sleep(ctx, backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		if result.TimeoutMS > 0 && result.TimeoutMS <= 120000 {
			pollTimeout = time.Duration(result.TimeoutMS)*time.Millisecond + 5*time.Second
		}
		b.mu.Lock()
		oldCursor, oldInbox, oldPause := b.state.Cursor, b.state.Inbox, b.state.PauseUntil
		oldWindow, oldContext := b.state.Window, b.state.ContextToken
		b.state.PauseUntil = 0
		if len(b.state.Inbox) > 100 {
			b.issue = "backlog"
			b.mu.Unlock()
			sleep(ctx, 5*time.Second)
			continue
		}
		for _, msg := range result.Messages {
			if msg.Type != 1 || msg.Group != "" || msg.From != b.state.OwnerID || (msg.To != "" && msg.To != b.state.BotID) || msg.MessageID == "" {
				continue
			}
			key := "weixin:" + b.state.BotID + ":" + string(msg.MessageID)
			if _, seen := b.state.Inputs[key]; seen {
				continue
			}
			duplicate := false
			for _, queued := range b.state.Inbox {
				if queued.ID == key {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			body := textOf(msg)
			if body == "" || len(body) > 32<<10 {
				continue
			}
			ref := referenceOf(msg)
			controlIn := textchannel.Inbound{Channel: "weixin", Conversation: msg.ContextToken, ID: key, Text: body}
			if ref != "" {
				controlIn.Reference = &textchannel.Reference{Conversation: b.state.OwnerID, MessageID: ref}
			}
			promptID := ""
			if b.host.TextControl != nil {
				promptID = b.host.TextControl.SecretPrompt(controlIn)
			}
			secret := promptID != ""
			stored := body
			if secret {
				b.secretInbox[key] = body
				b.state.SecretMessages[string(msg.MessageID)] = true
				stored = "" // never put the marked value in the persistent inbox
			}
			b.state.Inbox = append(b.state.Inbox, inbound{ID: key, Text: stored, ContextToken: msg.ContextToken, Secret: secret, PromptID: promptID, ReferenceID: ref, Quoted: b.quotedMessageLocked(msg)})
			if msg.ContextToken != "" {
				b.state.ContextToken = msg.ContextToken
				b.state.Window = sendWindow{OwnerID: b.state.OwnerID, InputID: key, ContextToken: msg.ContextToken, InputAt: time.Now().UnixMilli()}
			}
		}
		if result.Cursor != "" {
			b.state.Cursor = result.Cursor
		}
		if b.state.Cursor != oldCursor || len(b.state.Inbox) != len(oldInbox) || oldPause != 0 {
			if b.saveLocked() != nil {
				for _, pending := range b.state.Inbox[len(oldInbox):] {
					delete(b.secretInbox, pending.ID)
				}
				b.state.Cursor = oldCursor
				b.state.Inbox = oldInbox
				b.state.PauseUntil = oldPause
				b.state.Window, b.state.ContextToken = oldWindow, oldContext
				b.mu.Unlock()
				sleep(ctx, 5*time.Second)
				continue
			}
		}
		if b.issue == "network" || b.issue == "remote_error" || b.issue == "backlog" || b.issue == "session_cooldown" {
			b.issue = ""
		}
		b.mu.Unlock()
	}
}
func (b *Bridge) process(ctx context.Context, p *protocol) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		b.dispatch(ctx, p)
		b.output(ctx, p)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (b *Bridge) dispatch(ctx context.Context, transports ...*protocol) {
	b.mu.Lock()
	if len(b.state.Inbox) == 0 {
		b.mu.Unlock()
		return
	}
	in := b.state.Inbox[0]
	secretText := b.secretInbox[in.ID]
	controlIn := textchannel.Inbound{Channel: "weixin", Conversation: in.ContextToken, ID: in.ID, Text: in.Text}
	if in.ReferenceID != "" {
		controlIn.Reference = &textchannel.Reference{Conversation: b.state.OwnerID, MessageID: in.ReferenceID}
	}
	control := in.Secret || textchannel.IsCommand(in.Text) || b.host.TextControl != nil && b.host.TextControl.HasCardReference(controlIn)
	if !control {
		snapshot := b.host.Snapshot()
		recovery := b.host.Recovery()
		if snapshot.Connection != "ready" && snapshot.Connection != "connected" || recovery.Automatic || recovery.InProgress || recovery.Manual {
			b.mu.Unlock()
			return
		}
	}
	if state := b.state.Inputs[in.ID]; state != "" && state != "queued" {
		if in.Secret && b.host.TextControl != nil {
			b.host.TextControl.RecoverSecretInput(textchannel.Inbound{Channel: "weixin", Conversation: in.ContextToken, ID: in.ID}, in.PromptID)
		}
		delete(b.secretInbox, in.ID)
		b.state.Inbox = b.state.Inbox[1:]
		_ = b.saveLocked()
		b.mu.Unlock()
		if in.Secret && len(transports) > 0 && transports[0] != nil {
			b.mirrorNotices(ctx, transports[0])
		}
		return
	}
	b.state.Inputs[in.ID] = "dispatching"
	if !control && !b.state.HasInput {
		for _, item := range b.host.Snapshot().Items {
			if item.Kind == "assistant" && item.ID != "" {
				b.state.Outputs[outputKey(item)] = outbound{State: "skip"}
			}
		}
	}
	if b.saveLocked() != nil {
		b.state.Inputs[in.ID] = "queued"
		b.mu.Unlock()
		return
	}
	owner := b.state.OwnerID
	b.mu.Unlock()
	if b.host.RecordInput != nil {
		b.host.RecordInput(textchannel.Inbound{Channel: "weixin", Conversation: owner + "\x00" + in.ContextToken, ID: in.ID, Text: in.Text}, in.Secret)
	}
	if control {
		if b.host.TextControl != nil {
			command := in.Text
			if in.Secret {
				command = secretText
			}
			if in.Secret && command == "" {
				b.host.TextControl.RecoverSecretInput(textchannel.Inbound{Channel: "weixin", Conversation: in.ContextToken, ID: in.ID}, in.PromptID)
			} else {
				controlIn.Text = command
				if textchannel.IsCommand(command) {
					_ = b.host.TextControl.Handle(ctx, controlIn, b.host.Snapshot())
				} else {
					_, _ = b.host.TextControl.HandleReference(ctx, controlIn, b.host.Snapshot())
				}
			}
			if len(transports) > 0 && transports[0] != nil {
				b.mirrorNotices(ctx, transports[0])
			}
		}
		b.mu.Lock()
		delete(b.secretInbox, in.ID)
		b.state.Inputs[in.ID] = "handled"
		b.state.Inbox = b.state.Inbox[1:]
		_ = b.saveLocked()
		b.mu.Unlock()
		return
	}
	recovery := b.host.Recovery()
	if b.host.TextControl != nil {
		b.mu.Lock()
		conversation := b.state.OwnerID + "\x00" + in.ContextToken
		b.mu.Unlock()
		if err := b.host.TextControl.RecordOrigin(in.ID, textchannel.Origin{Channel: "weixin", Conversation: conversation}); err != nil {
			b.mu.Lock()
			b.state.Inputs[in.ID] = "rejected"
			b.state.Inbox = b.state.Inbox[1:]
			_ = b.saveLocked()
			b.mu.Unlock()
			if len(transports) > 0 && transports[0] != nil {
				b.sendControl(ctx, transports[0], in, "来源记录不可用，消息未提交。")
			}
			return
		}
	}
	work, stop := context.WithTimeout(ctx, 30*time.Second)
	receipt, err := b.host.Submit(work, api.Submission{ID: in.ID, Text: in.Text, Quoted: in.Quoted, IngressFence: recovery.Fence}, nil)
	stop()
	b.mu.Lock()
	if errors.Is(err, api.ErrRecoveryPending) {
		b.state.Inputs[in.ID] = "queued"
		_ = b.saveLocked()
		b.mu.Unlock()
		return
	}
	outcome := receipt.Outcome
	if outcome != "accepted" && outcome != "rejected" {
		outcome = "unknown"
	}
	if err != nil && outcome != "rejected" {
		outcome = "unknown"
	}
	b.state.Inputs[in.ID] = outcome
	if outcome == "accepted" {
		b.state.HasInput = true
		if b.state.Window.InputID == in.ID {
			b.state.ContextToken = in.ContextToken
		}
		b.state.InputContexts[in.ID] = in.ContextToken
	}
	b.state.Inbox = b.state.Inbox[1:]
	if outcome == "unknown" {
		b.issue = "input_uncertain"
	}
	_ = b.saveLocked()
	b.mu.Unlock()
}
func (b *Bridge) sendControl(ctx context.Context, p *protocol, in inbound, body string) {
	if b.host.TextControl != nil {
		b.mu.Lock()
		owner := b.state.OwnerID
		b.mu.Unlock()
		b.host.TextControl.Publish(textchannel.Inbound{Channel: "weixin", Conversation: owner + "\x00" + in.ContextToken, ID: in.ID}, body)
		b.mirrorNotices(ctx, p)
		return
	}
	b.sendTextOnce(ctx, p, "control:"+in.ID, in.ContextToken, body)
}
func (b *Bridge) sendTextOnce(ctx context.Context, p *protocol, key, contextToken, body string) string {
	_ = contextToken // the newest confirmed owner ingress owns the send window
	parts := chunks(body)
	if len(parts) == 1 {
		id, _ := b.sendOne(ctx, p, key, parts[0], true)
		return id
	}
	if len(parts) == 0 || len(parts) > windowSendLimit {
		return ""
	}
	b.mu.Lock()
	remaining := b.remainingLocked(true)
	for index := range parts {
		if old, ok := b.state.Outputs[fmt.Sprintf("%s:part:%d", key, index)]; ok && old.State == "accepted" {
			remaining++
		}
	}
	b.mu.Unlock()
	if remaining < len(parts) {
		return "" // never strand a card before its choices and command
	}
	for index, part := range parts {
		_, state := b.sendOne(ctx, p, fmt.Sprintf("%s:part:%d", key, index), part, true)
		if state != "accepted" {
			break
		}
	}
	return "" // a multi-message card has no single referenceable server ID
}
func outputKey(item api.Item) string { return "item:" + item.ID }
func textDigest(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}
func (b *Bridge) output(ctx context.Context, p *protocol) {
	b.mu.Lock()
	blocked := b.issue == "auth_expired" || b.issue == "storage" || b.state.PauseUntil > time.Now().UnixMilli()
	b.mu.Unlock()
	if blocked {
		return
	}
	snapshot := b.host.Snapshot()
	b.ObserveTurn(snapshot)
	if b.host.TextControl != nil {
		snapshot.Items = append([]api.Item(nil), snapshot.Items...)
		for index, item := range snapshot.Items {
			if len(item.AsyncQuestions) == 0 {
				continue
			}
			if card, err := b.host.TextControl.AsyncCard(item, snapshot.RuntimeOwner); err == nil {
				snapshot.Items[index] = card
			}
		}
	}
	b.mu.Lock()
	if !b.state.LocalIMSeeded {
		for _, item := range snapshot.Items {
			if (item.Kind == "user" || item.Kind == "assistant") && item.ID != "" && item.SeenAt == 0 {
				key := outputKey(item)
				if _, known := b.state.Outputs[key]; !known {
					b.state.Outputs[key] = outbound{State: "skip"}
				}
			}
		}
		if b.host.TextControl != nil {
			for _, notice := range b.host.TextControl.Notices() {
				if notice.SeenAt == 0 {
					key := "notice:" + notice.ID
					if _, known := b.state.Outputs[key]; !known {
						b.state.Outputs[key] = outbound{State: "skip"}
					}
				}
			}
		}
		b.state.LocalIMSeeded = true
		if b.saveLocked() != nil {
			b.mu.Unlock()
			return
		}
	}
	if !b.state.MirrorV2 {
		for _, item := range snapshot.Items {
			if (item.Kind == "assistant" || item.Kind == "user") && item.ID != "" {
				b.state.Outputs[outputKey(item)] = outbound{State: "skip"}
			}
		}
		for _, approval := range snapshot.Approvals {
			if approval.Status == "resolved" {
				b.state.Outputs["approval-result:"+approval.ID] = outbound{State: "skip"}
			}
		}
		if b.host.TextControl != nil {
			for _, notice := range b.host.TextControl.Notices() {
				b.state.Outputs["notice:"+notice.ID] = outbound{State: "skip"}
			}
		}
		b.state.MirrorV2 = true
		if b.saveLocked() != nil {
			b.mu.Unlock()
			return
		}
	}
	b.mu.Unlock()
	kinds := b.outputKinds(snapshot)
	ready := snapshot.Connection == "ready" || snapshot.Connection == "connected"
	if ready {
		b.mirrorControls(ctx, p, snapshot)
		// A question is actionable while its Turn waits; it cannot wait for the
		// final assistant response. Worker questions are coordinated by the Bot.
		for _, item := range snapshot.Items {
			if kinds[item.ID] == "question" {
				b.mirrorItem(ctx, p, item, "question")
			}
		}
	}
	b.mirrorNotices(ctx, p)
	if !ready {
		return
	}
	b.mirrorPhase(ctx, p, snapshot)
	latestFinal := ""
	visible := map[string]bool{}
	for _, item := range snapshot.Items {
		visible[item.ID] = true
		if kinds[item.ID] == "final" {
			latestFinal = item.ID
		}
	}
	b.mu.Lock()
	if old := b.state.PendingFinal; old != "" && (!visible[old] || latestFinal != "" && latestFinal != old) {
		b.state.Outputs["item:"+old] = outbound{State: "skip"}
		b.state.PendingFinal = ""
		b.state.Missed++
		_ = b.saveLocked()
	}
	b.mu.Unlock()
	for _, item := range snapshot.Items {
		if (item.Kind != "assistant" && item.Kind != "user") || item.ID == "" || item.Text == "" || item.Text == api.SilentReminder || item.Status != "completed" && item.Status != "accepted" && (item.Kind != "user" || item.Status != "received") {
			continue
		}
		if item.Kind == "assistant" {
			switch kinds[item.ID] {
			case "question":
				continue
			case "final", "task":
				b.mirrorItem(ctx, p, item, kinds[item.ID])
			default:
				// The Turn has ended and a later assistant item is its
				// final reply. Earlier completed items were commentary.
				b.mu.Lock()
				terminal := terminalTurn(b.state.TurnPhases[item.TurnKey])
				b.mu.Unlock()
				if item.TurnKey != "" && terminal {
					b.skipItem(outputKey(item), false)
				}
			}
			continue
		}
		b.mu.Lock()
		_, own := b.state.Inputs[item.RequestID]
		b.mu.Unlock()
		if own {
			continue
		}
		if b.host.TextControl != nil {
			route, _ := b.host.TextControl.OriginOf(item.RequestID)
			if route.Channel == "weixin" {
				continue
			}
		}
		b.mirrorItem(ctx, p, item, "user")
	}
	b.mirrorMissed(ctx, p)
}
func (b *Bridge) mirrorControls(ctx context.Context, p *protocol, snapshot api.Snapshot) {
	if b.host.TextControl != nil {
		for _, approval := range snapshot.Approvals {
			b.mu.Lock()
			contextToken, owner := b.state.ContextToken, b.state.OwnerID
			b.mu.Unlock()
			if approval.Status == "pending" {
				card, short, err := b.host.TextControl.Card(approval)
				if err == nil && card != "" {
					if messageID := b.sendTextOnce(ctx, p, "approval:"+approval.ID+":"+short, contextToken, card); messageID != "" {
						_ = b.host.TextControl.BindCard("weixin", owner, messageID, approval)
					}
				}
			} else if approval.Status == "resolved" {
				b.sendTextOnce(ctx, p, "approval-result:"+approval.ID, contextToken, approvalStatus(approval))
			}
		}
	}
}
func (b *Bridge) mirrorPhase(ctx context.Context, p *protocol, snapshot api.Snapshot) {
	if key, body := phaseNotice(snapshot); key != "" {
		b.mu.Lock()
		token := b.state.ContextToken
		b.mu.Unlock()
		b.sendTextOnce(ctx, p, key, token, body)
	}
}
func approvalStatus(a api.Approval) string {
	if a.Resolution == nil {
		return "原请求已处理。"
	}
	for _, choice := range a.Choices {
		if choice.ID == a.Resolution.ChoiceID && choice.Label != "" {
			return "原请求已处理：" + choice.Label
		}
	}
	switch a.Resolution.Outcome {
	case "allowed":
		return "原请求已处理：已允许。"
	case "declined":
		return "原请求已处理：已拒绝。"
	case "cancelled":
		return "原请求已处理：已取消。"
	default:
		return "原请求已处理。"
	}
}
func phaseNotice(s api.Snapshot) (string, string) {
	if s.LastReceipt.ID == "" || s.CurrentTurn == "" {
		return "", ""
	}
	body := map[string]string{"failed": "Bot 未能完成此请求，可在同一对话询问原因。", "interrupted": "工作已停止。", "unknown": "结果暂不确定，请先核对原请求，不要重复发送。"}[s.Phase]
	if body == "" {
		return "", ""
	}
	for _, item := range s.Items {
		if item.Kind == "user" && item.RequestID == s.LastReceipt.ID && item.TurnKey == s.CurrentTurn {
			return "status:" + s.CurrentTurn + ":" + s.Phase, body
		}
	}
	return "", ""
}
func (b *Bridge) mirrorNotices(ctx context.Context, p *protocol) {
	if b.host.TextControl == nil {
		return
	}
	for _, notice := range b.host.TextControl.Notices() {
		b.mu.Lock()
		token := b.state.ContextToken
		b.mu.Unlock()
		if notice.Origin.Channel == "weixin" {
			token = notice.Origin.Conversation
		}
		out := textchannel.Outbound{Channel: "weixin", Conversation: token, ID: "notice:" + notice.ID, Text: notice.Text}
		b.sendTextOnce(ctx, p, out.ID, out.Conversation, out.Text)
	}
}
