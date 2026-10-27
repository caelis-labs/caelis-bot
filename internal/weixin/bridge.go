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
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/secretstore"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
	qrcode "github.com/skip2/go-qrcode"
)

type Host struct {
	Diagnostics *diagnosticlog.Logger
	Snapshot    func() api.Snapshot
	WorkStates  func() []api.WorkState // existing Worker authority; never infer activity from chat chips
	Now         func() time.Time       // injectable scheduler clock
	Recovery    func() api.RecoveryState
	Interrupt   func(context.Context) error
	Artifact    func(string) (string, error)
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
	ID           string `json:"id"`
	Text         string `json:"text"`
	ContextToken string `json:"contextToken"`
	Secret       bool   `json:"secret,omitempty"` // body is held only in memory
	PromptID     string `json:"promptId,omitempty"`
}
type outbound struct {
	State        string `json:"state"`
	ArtifactID   string `json:"artifactId,omitempty"` // owned Host handle; never a path
	FileTurnKey  string `json:"fileTurnKey,omitempty"`
	FileName     string `json:"fileName,omitempty"`
	FileInputID  string `json:"fileInputId,omitempty"` // preparation belongs to this send window
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
	ProgressUsed int    `json:"progressUsed,omitempty"`
	Exhausted    bool   `json:"exhausted,omitempty"`
	Replied      bool   `json:"replied,omitempty"`
	Recover      bool   `json:"recover,omitempty"`
}
type pendingProgress struct {
	InputID string `json:"inputId,omitempty"`
	ID      string `json:"id"`
	TurnKey string `json:"turnKey"`
	Text    string `json:"text"`
}
type document struct {
	Delivery         deliverySchedule    `json:"delivery,omitempty"`
	Version          int                 `json:"version"`
	Enabled          bool                `json:"enabled"`
	BotID            string              `json:"botId"`
	OwnerID          string              `json:"ownerId"`
	BaseURL          string              `json:"baseURL"`
	Cursor           string              `json:"cursor"`
	HasInput         bool                `json:"hasInput"`
	AcceptedInputID  string              `json:"acceptedInputId,omitempty"` // actual last accepted submit, independent of the newest receive window
	ContextToken     string              `json:"contextToken,omitempty"`
	Window           sendWindow          `json:"window,omitempty"`
	PendingFinal     string              `json:"pendingFinal,omitempty"`
	Progress         []pendingProgress   `json:"progress,omitempty"`
	ProgressBatch    []string            `json:"progressBatch,omitempty"`
	FinalProgressFor string              `json:"finalProgressFor,omitempty"`
	FinalProgressIDs []string            `json:"finalProgressIds,omitempty"`
	ObservedTurn     string              `json:"observedTurn,omitempty"`
	Missed           int                 `json:"missed,omitempty"`
	TurnPhases       map[string]string   `json:"turnPhases,omitempty"`
	TurnOrder        []string            `json:"turnOrder,omitempty"`
	PauseUntil       int64               `json:"pauseUntil,omitempty"`
	Inbox            []inbound           `json:"inbox,omitempty"`
	Inputs           map[string]string   `json:"inputs"`
	Outputs          map[string]outbound `json:"outputs"`
	FileIssue        string              `json:"fileIssue,omitempty"`
	MirrorV2         bool                `json:"mirrorV2,omitempty"`
	LocalIMSeeded    bool                `json:"localImSeeded,omitempty"`
}
type Bridge struct {
	savedDigest, savedPath        string
	deliveryMu                    sync.Mutex
	preparedFiles                 map[string]fileItem
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
	updating                      bool
	fileWait                      sync.WaitGroup
	loadErr                       bool
	secretInbox                   map[string]string
	fileWork                      map[string]bool
}

func emptyDocument() document {
	return document{Version: 1, Inputs: map[string]string{}, Outputs: map[string]outbound{}, TurnPhases: map[string]string{}, MirrorV2: true, LocalIMSeeded: true}
}
func Open(root string, host Host) (*Bridge, error) {
	hash := sha256.Sum256([]byte(root))
	account := hex.EncodeToString(hash[:])
	b := &Bridge{path: filepath.Join(root, "weixin.json"), account: account, host: host, state: emptyDocument(), secretInbox: map[string]string{}, fileWork: map[string]bool{}, secrets: &secretstore.FileStore{Root: filepath.Join(root, "Credentials"), Namespace: "weixin", Legacy: secretstore.Functions{LoadFunc: loadSecret, DeleteFunc: deleteSecret}}}
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

	if b.state.TurnPhases == nil {
		b.state.TurnPhases = map[string]string{}
	}

	if b.state.PauseUntil > time.Now().UnixMilli() {
		b.issue = "session_cooldown"
	} else if b.state.FileIssue != "" {
		b.issue = b.state.FileIssue
	}
	return b, nil
}
func (b *Bridge) saveLocked() error {
	if b.loadErr {
		return errors.New("storage")
	}
	// Idle scheduler polls must not fsync the same document repeatedly. Only
	// confirmed writes populate this cache; every changed intent/window still
	// crosses the existing durable write boundary before HTTP.
	encoded, err := json.Marshal(b.state)
	if err != nil {
		return err
	}
	digest := textDigest(string(encoded))
	if digest == b.savedDigest && b.path == b.savedPath {
		return nil
	}
	if err := localstate.WriteConfirmed(b.path, b.state); err != nil {
		b.savedDigest, b.savedPath = "", ""
		b.issue = "storage"
		return errors.New("storage")
	}
	b.savedDigest, b.savedPath = digest, b.path
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
	if b.closed || b.updating || !b.state.Enabled || b.state.BotID == "" || b.cancel != nil || b.issue == "storage" {
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
		b.fileWait.Wait()
		work, stop := context.WithTimeout(context.Background(), 4*time.Second)
		_ = client.notify(work, false)
		stop()
	}
	b.mu.Lock()
	if !b.updating {
		b.secretInbox = map[string]string{} // a later resume asks the owner to re-enter secret answers
	}
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
		oldPaused, oldStart := b.state.Delivery.PausedWindow, b.state.Delivery.StartAt
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
			controlIn := textchannel.Inbound{Channel: "weixin", Conversation: msg.ContextToken, ID: key, Text: body}
			promptID := ""
			if b.host.TextControl != nil {
				promptID = b.host.TextControl.SecretPrompt(controlIn)
			}
			secret := promptID != ""
			stored := body
			if secret {
				b.secretInbox[key] = body
				stored = "" // never put the marked value in the persistent inbox
			}
			b.state.Inbox = append(b.state.Inbox, inbound{ID: key, Text: stored, ContextToken: msg.ContextToken, Secret: secret, PromptID: promptID})
			// Even an input without a usable context fences the prior window.
			// It cannot authorize another send using the previous token.
			if b.remainingLocked(true) == 0 {
				b.state.Delivery.PausedWindow = b.state.Window.InputID
			}
			if b.state.Delivery.StartAt == 0 {
				b.state.Delivery.StartAt = b.state.Window.InputAt
				if b.state.Delivery.StartAt == 0 {
					b.state.Delivery.StartAt = b.now().UnixMilli()
				}
			}
			b.state.ContextToken = msg.ContextToken
			b.state.Window = sendWindow{OwnerID: b.state.OwnerID, InputID: key, ContextToken: msg.ContextToken, InputAt: b.now().UnixMilli()}
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
				b.state.Delivery.PausedWindow, b.state.Delivery.StartAt = oldPaused, oldStart
				b.mu.Unlock()
				sleep(ctx, 5*time.Second)
				continue
			}
		}
		if b.issue == "network" || b.issue == "remote_error" || b.issue == "backlog" || b.issue == "session_cooldown" {
			b.issue = ""
		}
		received := append([]inbound(nil), b.state.Inbox[len(oldInbox):]...)
		owner := b.state.OwnerID
		b.mu.Unlock()
		// Publish authenticated ingress before recovery/dispatch can delay it.
		// Request-ID deduplication keeps the later dispatch echo in its place.
		if b.host.RecordInput != nil {
			for _, in := range received {
				b.host.RecordInput(textchannel.Inbound{Channel: "weixin", Conversation: owner + "\x00" + in.ContextToken, ID: in.ID, Text: in.Text}, in.Secret)
			}
		}
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
	index := 0
	in := b.state.Inbox[index]
	secretText := b.secretInbox[in.ID]
	controlIn := textchannel.Inbound{Channel: "weixin", Conversation: in.ContextToken, ID: in.ID, Text: in.Text}
	control := in.Secret || textchannel.IsCommand(in.Text)
	if !control {
		snapshot := b.host.Snapshot()
		recovery := b.host.Recovery()
		if snapshot.Connection != "ready" && snapshot.Connection != "connected" || recovery.Automatic || recovery.InProgress || recovery.Manual {
			// Keep ordinary input in order, but let an explicit control
			// command reach its original pending request during recovery.
			index = -1
			for i := 1; i < len(b.state.Inbox); i++ {
				candidate := b.state.Inbox[i]
				if candidate.Secret || textchannel.IsCommand(candidate.Text) {
					index = i
					break
				}
			}
			if index < 0 {
				b.mu.Unlock()
				return
			}
			in = b.state.Inbox[index]
			secretText = b.secretInbox[in.ID]
			controlIn = textchannel.Inbound{Channel: "weixin", Conversation: in.ContextToken, ID: in.ID, Text: in.Text}
			control = true
		}
	}
	if state := b.state.Inputs[in.ID]; state != "" && state != "queued" {
		if in.Secret && b.host.TextControl != nil {
			b.host.TextControl.RecoverSecretInput(textchannel.Inbound{Channel: "weixin", Conversation: in.ContextToken, ID: in.ID}, in.PromptID)
		}
		delete(b.secretInbox, in.ID)
		b.state.Inbox = append(b.state.Inbox[:index], b.state.Inbox[index+1:]...)
		_ = b.saveLocked()
		b.mu.Unlock()
		if in.Secret && len(transports) > 0 && transports[0] != nil {
			b.mirrorNotices(ctx, transports[0])
		}
		return
	}
	b.state.Inputs[in.ID] = "dispatching"

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
		if localCommand(in.Text) {
			b.mu.Lock()
			priorInbox := append([]inbound(nil), b.state.Inbox...)
			priorInput := b.state.Inputs[in.ID]
			delete(b.secretInbox, in.ID)
			b.state.Inputs[in.ID] = "handled"
			b.state.Inbox = append(b.state.Inbox[:index], b.state.Inbox[index+1:]...)
			err := b.saveLocked()
			if err != nil {
				b.state.Inbox = priorInbox
				b.state.Inputs[in.ID] = priorInput
			}
			b.mu.Unlock()
			if err == nil && len(transports) > 0 && transports[0] != nil {
				b.handleLocalCommand(ctx, transports[0], in)
			}
			return
		}
		if b.host.TextControl != nil {
			command := in.Text
			if in.Secret {
				command = secretText
			}
			if in.Secret && command == "" {
				b.host.TextControl.RecoverSecretInput(textchannel.Inbound{Channel: "weixin", Conversation: in.ContextToken, ID: in.ID}, in.PromptID)
			} else {
				controlIn.Text = command
				_ = b.host.TextControl.Handle(ctx, controlIn, b.host.Snapshot())
			}
			if len(transports) > 0 && transports[0] != nil {
				b.mirrorNotices(ctx, transports[0])
			}
		}
		b.mu.Lock()
		delete(b.secretInbox, in.ID)
		b.state.Inputs[in.ID] = "handled"
		b.state.Inbox = append(b.state.Inbox[:index], b.state.Inbox[index+1:]...)
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
			b.state.Inbox = append(b.state.Inbox[:index], b.state.Inbox[index+1:]...)
			_ = b.saveLocked()
			b.mu.Unlock()
			if len(transports) > 0 && transports[0] != nil {
				b.sendControl(ctx, transports[0], in, "来源记录不可用，消息未提交。")
			}
			return
		}
	}
	work, stop := context.WithTimeout(ctx, 30*time.Second)
	receipt, err := b.host.Submit(work, api.Submission{ID: in.ID, Text: in.Text, IngressFence: recovery.Fence}, nil)
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
		b.state.AcceptedInputID = in.ID
		if b.state.Window.InputID == in.ID {
			b.state.ContextToken = in.ContextToken
		}
	}
	b.state.Inbox = append(b.state.Inbox[:index], b.state.Inbox[index+1:]...)
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
	b.localFeedback(ctx, p, in, body)
}
func (b *Bridge) sendTextOnce(ctx context.Context, p *protocol, key, contextToken, body string) {
	b.sendTextOncePolicy(ctx, p, key, contextToken, body, true)
}
func (b *Bridge) sendTextOncePolicy(ctx context.Context, p *protocol, key, contextToken, body string, critical bool) {
	_ = contextToken // the newest confirmed owner ingress owns the send window
	parts := chunks(body)
	if len(parts) == 1 {
		_, _ = b.sendOne(ctx, p, key, parts[0], critical)
		return
	}
	if len(parts) == 0 || len(parts) > windowSendLimit {
		return
	}
	b.mu.Lock()
	remaining := b.remainingLocked(critical)
	for index := range parts {
		if old, ok := b.state.Outputs[fmt.Sprintf("%s:part:%d", key, index)]; ok && old.State == "accepted" {
			remaining++
		}
	}
	b.mu.Unlock()
	if remaining < len(parts) {
		return // never strand a card before its choices and command
	}
	for index, part := range parts {
		_, state := b.sendOne(ctx, p, fmt.Sprintf("%s:part:%d", key, index), part, critical)
		if state != "accepted" {
			break
		}
	}
}
func outputKey(item api.Item) string { return "item:" + item.ID }
func textDigest(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}
func markdownApprovalCard(card string) string {
	lines := strings.Split(card, "\n")
	if len(lines) > 0 {
		lines[0] = "**" + lines[0] + "**"
	}
	for i, line := range lines {
		if strings.HasPrefix(line, "[Q") && strings.Contains(line, "] ") || strings.HasPrefix(line, "提示：") {
			lines[i] = "**" + line + "**"
		}
	}
	return strings.Join(lines, "\n")
}
func markdownQuestionCard(card string) string {
	lines := strings.Split(card, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "[Q") && strings.Contains(line, "] ") || strings.HasPrefix(line, "提示：") {
			lines[i] = "**" + line + "**"
		}
	}
	return strings.Join(lines, "\n")
}
func (b *Bridge) mirrorNotices(ctx context.Context, p *protocol) {
	b.observeNotices()
	b.output(ctx, p)
}
