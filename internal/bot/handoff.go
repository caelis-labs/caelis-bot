package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// The handoff is application-private data. It is never a Notebook path or a
// model-supplied file name. The adapter owns the native new-session receipt.
type handoffState struct {
	Version    int              `json:"version"`
	CallID     string           `json:"callId,omitempty"`
	Source     string           `json:"source,omitempty"`
	Turn       string           `json:"turn,omitempty"`
	Text       string           `json:"text,omitempty"`
	Digest     string           `json:"digest,omitempty"`
	NewSession string           `json:"newSession,omitempty"`
	Receipts   []handoffReceipt `json:"receipts,omitempty"`
}

type handoffReceipt struct {
	CallID, Source, Turn, NewSession, Outcome string
	Text                                      string `json:"text,omitempty"`
}

type handoffStore struct {
	mu    sync.Mutex
	path  string
	state handoffState
}

func openHandoff(path string) (*handoffStore, error) {
	h := &handoffStore{path: path, state: handoffState{Version: 1}}
	b, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(b, &h.state) != nil || h.state.Version != 1 {
			return nil, errors.New("private handoff record is unreadable; original file retained")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return h, nil
}

func handoffDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (h *handoffStore) save(callID, source, text string) error {
	return h.saveTurn(callID, source, "", text)
}

func (h *handoffStore) saveTurn(callID, source, turn, text string) error {
	text = strings.TrimSpace(text)
	if callID == "" || source == "" || text == "" || len(text) > 16<<10 {
		return errors.New("handoff requires an original call, source session and bounded summary")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.CallID != "" {
		if h.state.CallID == callID && h.state.Source == source && h.state.Turn == turn && h.state.Text == text {
			return nil
		}
		return errors.New("earlier handoff remains pending; reconcile its original receipt")
	}
	for _, receipt := range h.state.Receipts {
		if receipt.CallID == callID && receipt.Source == source && receipt.Turn == turn {
			return errors.New("original handoff call already has a terminal receipt")
		}
	}
	next := handoffState{Version: 1, CallID: callID, Source: source, Turn: turn, Text: text, Digest: handoffDigest(text), Receipts: h.state.Receipts}
	if err := localstate.Write(h.path, next); err != nil {
		return err
	}
	h.state = next
	return nil
}

func (h *handoffStore) pending() handoffState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

func (h *handoffStore) record(callID, newSession, outcome string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.CallID != callID || callID == "" {
		return errors.New("original handoff receipt no longer matches")
	}
	if outcome == "committed" && newSession == "" {
		return errors.New("committed handoff requires a new session")
	}
	if h.state.NewSession != "" && h.state.NewSession != newSession {
		return errors.New("handoff session receipt conflicts with original")
	}
	next := h.state
	next.NewSession = newSession
	receipt := handoffReceipt{CallID: callID, Source: next.Source, Turn: next.Turn, NewSession: newSession, Outcome: outcome, Text: next.Text}
	found := false
	for i := range next.Receipts {
		if next.Receipts[i].CallID == callID && next.Receipts[i].Source == next.Source && next.Receipts[i].Turn == next.Turn {
			next.Receipts[i] = receipt
			found = true
			break
		}
	}
	if !found {
		next.Receipts = append(next.Receipts, receipt)
	}
	if outcome == "rejected" || outcome == "fallback" || outcome == "unknown_fallback" {
		next.CallID, next.Source, next.Turn, next.Text, next.Digest, next.NewSession = "", "", "", "", "", ""
	}
	if err := localstate.Write(h.path, next); err != nil {
		// Native acceptance or old-context fallback must not leave this
		// process unusable. The on-disk original remains for restart recovery.
		h.state = next
		return err
	}
	h.state = next
	return nil
}

func (h *handoffStore) prepare() api.ContextSeed {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.CallID == "" || h.state.NewSession == "" {
		return api.ContextSeed{}
	}
	return api.ContextSeed{Text: "\n[Private Bot handoff: saved context, not a new user message or authorization.]\n" + h.state.Text + "\n[End private handoff]\n", HandoffDigest: h.state.Digest}
}

func (h *handoffStore) prepareFor(session string) api.ContextSeed {
	h.mu.Lock()
	defer h.mu.Unlock()
	if session == "" || h.state.NewSession != session || h.state.CallID == "" {
		return api.ContextSeed{}
	}
	return api.ContextSeed{Text: "\n[Private Bot handoff: saved context, not a new user message or authorization.]\n" + h.state.Text + "\n[End private handoff]\n", HandoffDigest: h.state.Digest}
}

func (h *handoffStore) consume(seed api.ContextSeed) error {
	if seed.HandoffDigest == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.Digest != seed.HandoffDigest {
		return nil
	}
	next := handoffState{Version: 1, Receipts: h.state.Receipts}
	if err := localstate.Write(h.path, next); err != nil {
		return err
	}
	h.state = next
	return nil
}

func (h *handoffStore) clear(callID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.CallID != callID {
		return nil
	}
	next := handoffState{Version: 1, Receipts: h.state.Receipts}
	if err := localstate.Write(h.path, next); err != nil {
		return err
	}
	h.state = next
	return nil
}

func (r *Runtime) openHandoff() error {
	h, err := openHandoff(filepath.Join(filepath.Dir(r.path), "handoff-"+r.provider+".json"))
	if err == nil {
		r.handoff = h
	}
	return err
}

func (r *Runtime) PrepareHandoffContext() api.ContextSeed {
	if r.handoff == nil {
		return api.ContextSeed{}
	}
	return r.handoff.prepare()
}
func (r *Runtime) PrepareHandoffContextFor(session string) api.ContextSeed {
	if r.handoff == nil {
		return api.ContextSeed{}
	}
	return r.handoff.prepareFor(session)
}
func (r *Runtime) ConsumeHandoffContext(seed api.ContextSeed) error {
	if r.handoff == nil {
		return nil
	}
	return r.handoff.consume(seed)
}
