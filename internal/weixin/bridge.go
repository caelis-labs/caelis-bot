package weixin

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

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/secretstore"
	qrcode "github.com/skip2/go-qrcode"
)

type Host struct {
	Snapshot func() api.Snapshot
	Recovery func() api.RecoveryState
	Submit   func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)
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
}
type outbound struct {
	State        string `json:"state"`
	ClientID     string `json:"clientId,omitempty"`
	ContextToken string `json:"contextToken,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Digest       string `json:"digest,omitempty"`
	Attempts     int    `json:"attempts,omitempty"`
	NextRetryAt  int64  `json:"nextRetryAt,omitempty"`
}
type document struct {
	Version       int                 `json:"version"`
	Enabled       bool                `json:"enabled"`
	BotID         string              `json:"botId"`
	OwnerID       string              `json:"ownerId"`
	BaseURL       string              `json:"baseURL"`
	Cursor        string              `json:"cursor"`
	HasInput      bool                `json:"hasInput"`
	ContextToken  string              `json:"contextToken,omitempty"`
	PauseUntil    int64               `json:"pauseUntil,omitempty"`
	InputContexts map[string]string   `json:"inputContexts,omitempty"`
	Inbox         []inbound           `json:"inbox,omitempty"`
	Inputs        map[string]string   `json:"inputs"`
	Outputs       map[string]outbound `json:"outputs"`
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
}

func emptyDocument() document {
	return document{Version: 1, Inputs: map[string]string{}, Outputs: map[string]outbound{}, InputContexts: map[string]string{}}
}
func Open(root string, host Host) (*Bridge, error) {
	hash := sha256.Sum256([]byte(root))
	account := hex.EncodeToString(hash[:])
	b := &Bridge{path: filepath.Join(root, "weixin.json"), account: account, host: host, state: emptyDocument(), secrets: &secretstore.FileStore{Root: filepath.Join(root, "Credentials"), Namespace: "weixin", Legacy: secretstore.Functions{LoadFunc: loadSecret, DeleteFunc: deleteSecret}}}
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
		if item.Kind == "assistant" && item.ID != "" {
			state.Outputs["item:"+item.ID] = outbound{State: "skip"}
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
			b.state.Inbox = append(b.state.Inbox, inbound{ID: key, Text: body, ContextToken: msg.ContextToken})
		}
		if result.Cursor != "" {
			b.state.Cursor = result.Cursor
		}
		if b.state.Cursor != oldCursor || len(b.state.Inbox) != len(oldInbox) || oldPause != 0 {
			if b.saveLocked() != nil {
				b.state.Cursor = oldCursor
				b.state.Inbox = oldInbox
				b.state.PauseUntil = oldPause
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
		b.dispatch(ctx)
		b.output(ctx, p)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (b *Bridge) dispatch(ctx context.Context) {
	if b.host.Snapshot().Connection != "ready" && b.host.Snapshot().Connection != "connected" {
		return
	}
	recovery := b.host.Recovery()
	if recovery.Automatic || recovery.InProgress || recovery.Manual {
		return
	}
	b.mu.Lock()
	if len(b.state.Inbox) == 0 {
		b.mu.Unlock()
		return
	}
	in := b.state.Inbox[0]
	if state := b.state.Inputs[in.ID]; state != "" && state != "queued" {
		b.state.Inbox = b.state.Inbox[1:]
		_ = b.saveLocked()
		b.mu.Unlock()
		return
	}
	b.state.Inputs[in.ID] = "dispatching"
	if !b.state.HasInput {
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
	b.mu.Unlock()
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
		b.state.ContextToken = in.ContextToken
		b.state.InputContexts[in.ID] = in.ContextToken
	}
	b.state.Inbox = b.state.Inbox[1:]
	if outcome == "unknown" {
		b.issue = "input_uncertain"
	}
	_ = b.saveLocked()
	b.mu.Unlock()
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
	if snapshot.Connection != "ready" && snapshot.Connection != "connected" {
		return
	}
	for _, item := range snapshot.Items {
		if item.Kind != "assistant" || item.ID == "" || item.Text == "" || item.Text == api.SilentReminder || item.Status != "completed" {
			continue
		}
		parts := chunks(item.Text)
		for index, part := range parts {
			key := fmt.Sprintf("%s:%d", outputKey(item), index)
			digest := textDigest(part)
			b.mu.Lock()
			if _, seen := b.state.Outputs[outputKey(item)]; seen {
				b.mu.Unlock()
				break
			}
			if !b.state.HasInput && b.state.ContextToken == "" {
				b.mu.Unlock()
				return
			}
			intent, seen := b.state.Outputs[key]
			if seen {
				if intent.State == "accepted" && intent.Digest == digest {
					b.mu.Unlock()
					continue
				}
				// Pre-policy unknowns have no digest/attempt counter and cannot
				// safely be replayed. Changed text cannot reuse a saved client ID.
				if intent.State != "unknown" || intent.Digest != digest || intent.ClientID == "" || intent.Attempts < 1 || intent.Attempts >= 3 {
					b.mu.Unlock()
					break
				}
				if intent.NextRetryAt > time.Now().UnixMilli() {
					b.mu.Unlock()
					return
				}
				intent.Attempts++
			} else {
				var seed [16]byte
				if _, err := rand.Read(seed[:]); err != nil {
					b.mu.Unlock()
					return
				}
				contextToken := b.state.ContextToken
				for _, source := range snapshot.Items {
					if source.Kind == "user" && source.TurnKey != "" && source.TurnKey == item.TurnKey && strings.HasPrefix(source.RequestID, "weixin:") {
						if token, ok := b.state.InputContexts[source.RequestID]; ok {
							contextToken = token
						}
					}
				}
				intent = outbound{State: "unknown", ClientID: "caelis-weixin-" + hex.EncodeToString(seed[:]), ContextToken: contextToken, Digest: digest, Attempts: 1}
			}
			previous := b.state.Outputs[key]
			intent.NextRetryAt = 0
			b.state.Outputs[key] = intent
			if b.saveLocked() != nil {
				if seen {
					b.state.Outputs[key] = previous
				} else {
					delete(b.state.Outputs, key)
				}
				b.mu.Unlock()
				return
			}
			owner := b.state.OwnerID
			b.mu.Unlock()
			work, stop := context.WithTimeout(ctx, 15*time.Second)
			result, err := p.send(work, owner, intent.ContextToken, intent.ClientID, part)
			stop()
			b.mu.Lock()
			state := "accepted"
			reason := "ret_zero"
			if err != nil {
				state = "unknown"
				reason = "transport_or_response"
				if errors.Is(err, context.DeadlineExceeded) {
					reason = "timeout"
				} else if strings.HasPrefix(err.Error(), "http_") {
					reason = "http_error"
				}
				if intent.Attempts < 3 {
					intent.NextRetryAt = time.Now().Add(time.Duration(2<<(2*(intent.Attempts-1))) * time.Second).UnixMilli()
					b.issue = "delivery_retrying"
				} else {
					b.issue = "delivery_uncertain"
				}
			} else if (result.Ret != nil && *result.Ret != 0) || result.ErrCode != 0 {
				state = "rejected"
				reason = "business_rejected"
				b.issue = "send_rejected"
			} else if result.Ret == nil {
				reason = "http_success_no_ret"
			}
			if state == "accepted" && b.issue == "delivery_retrying" {
				b.issue = ""
			}
			intent.State, intent.Reason = state, reason
			b.state.Outputs[key] = intent
			if b.saveLocked() != nil {
				b.mu.Unlock()
				return
			}
			b.mu.Unlock()
			if state != "accepted" {
				return
			}
			if index+1 < len(parts) {
				sleep(ctx, time.Second)
			}
		}
	}
}
