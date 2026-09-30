package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

type nativeProductClient interface {
	Connect(context.Context) (productrpc.Identity, error)
	State(context.Context) (productrpc.State, error)
	Watch(context.Context, productrpc.Cursor) (productrpc.State, error)
	Command(context.Context, productrpc.Command) (productrpc.Result, error)
	Receipt(context.Context, string) (productrpc.Result, error)
	Upload(context.Context, string, []byte) (productrpc.Resource, error)
	Download(context.Context, string) (productrpc.Resource, []byte, error)
	Close()
}

type productClientFactory func(backend.ProductPairing) (nativeProductClient, io.Closer, error)

type productPending struct {
	Kind         string                        `json:"kind"`
	Digest       string                        `json:"digest"`
	SourceDigest string                        `json:"sourceDigest,omitempty"`
	Runtime      *backend.RemoteRuntimeRequest `json:"runtime,omitempty"`
	Scope        *productmanagement.Scope      `json:"scope,omitempty"`
}
type productReceiptDocument struct {
	Version int                       `json:"version"`
	Pairing string                    `json:"pairing"`
	Pending map[string]productPending `json:"pending"`
}

// This is an APP-side product facade, not a resident Runtime. No model driver,
// Bot tools, Notebook, memory store or scheduler is constructed here.
type productEngine struct {
	op                sync.Mutex
	mu                sync.Mutex
	pairing           backend.ProductPairing
	factory           productClientFactory
	client            nativeProductClient
	transport         io.Closer
	state             productrpc.State
	identity          productrpc.Identity
	connection, issue string
	unresolved        int
	revision          uint64
	changed           chan struct{}
	life              context.Context
	cancel            context.CancelFunc
	watchCancel       context.CancelFunc
	connectCancel     context.CancelFunc
	attempt           uint64
	closed            bool
	root              string
	receipts          productReceiptDocument
}

func newProductEngine(root string, pairing backend.ProductPairing, factory productClientFactory) (*productEngine, error) {
	ctx, cancel := context.WithCancel(context.Background())
	b, _ := json.Marshal(pairing)
	sum := sha256.Sum256(b)
	e := &productEngine{root: root, pairing: pairing, factory: factory, connection: "offline", revision: 1, changed: make(chan struct{}), life: ctx, cancel: cancel, receipts: productReceiptDocument{Version: 1, Pairing: hex.EncodeToString(sum[:]), Pending: map[string]productPending{}}}
	file := filepath.Join(root, "product-client-receipts.json")
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		return e, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 64<<10 {
		cancel()
		return nil, errors.New("private product receipt metadata is unavailable; original file was preserved")
	}
	b, err = os.ReadFile(file)
	var saved productReceiptDocument
	if err != nil || json.Unmarshal(b, &saved) != nil || saved.Version != 1 || len(saved.Pending) > 128 || saved.Pending == nil {
		cancel()
		return nil, errors.New("private product receipt metadata is invalid; original file was preserved")
	}
	if saved.Pairing == e.receipts.Pairing {
		for id, pending := range saved.Pending {
			if !productIdentifier.MatchString(id) || len(pending.Digest) != 64 || pending.Kind == "" {
				cancel()
				return nil, errors.New("private product receipt metadata is invalid")
			}
		}
		e.receipts = saved
		e.unresolved = len(saved.Pending)
	} else if len(saved.Pending) > 0 {
		cancel()
		return nil, errors.New("reconcile the previous Bot's uncertain receipts before replacing its pairing")
	}
	return e, nil
}

func (e *productEngine) connectionState() (string, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.unresolved > 0 {
		return e.connection, "outcome_unknown"
	}
	return e.connection, e.issue
}

func (e *productEngine) notifyLocked() {
	e.revision++
	close(e.changed)
	e.changed = make(chan struct{})
}

func (e *productEngine) offlineLocked(issue string) {
	e.connection, e.issue = "offline", issue
	e.notifyLocked()
}

func (e *productEngine) Connect(parent context.Context) error {
	e.op.Lock()
	defer e.op.Unlock()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(e.life, cancel)
	defer stop()
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return errors.New("APP product connection is closed")
	}
	oldClient, oldTransport, oldCancel := e.client, e.transport, e.watchCancel
	e.client, e.transport, e.watchCancel = nil, nil, nil
	e.attempt++
	attempt := e.attempt
	e.connectCancel = cancel
	e.connection, e.issue = "connecting", ""
	e.notifyLocked()
	e.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if oldClient != nil {
		oldClient.Close()
	}
	if oldTransport != nil {
		_ = oldTransport.Close()
	}
	var client nativeProductClient
	var transport io.Closer
	var err error
	if e.factory == nil {
		err = errors.New("native product transport unavailable")
	} else {
		client, transport, err = e.factory(e.pairing)
	}
	if err == nil && client == nil {
		err = errors.New("native product transport unavailable")
	}
	var identity productrpc.Identity
	var state productrpc.State
	if err == nil {
		identity, err = client.Connect(ctx)
	}
	if err == nil && (identity.Version != productrpc.ProtocolVersion || identity.NodeID != e.pairing.NodeID || identity.BotID != e.pairing.BotID || identity.Generation == "") {
		err = errors.New("product identity mismatch")
	}
	if err == nil {
		state, err = client.State(ctx)
	}
	if err == nil && (state.Scope != identity.Scope || state.Cursor.Generation != identity.Generation) {
		err = errors.New("product state generation changed")
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if client != nil {
			client.Close()
		}
		if transport != nil {
			_ = transport.Close()
		}
		e.mu.Lock()
		if attempt == e.attempt {
			e.offlineLocked("connection_unavailable")
			e.connectCancel = nil
		}
		e.mu.Unlock()
		return errors.New("remote Bot connection unavailable; check its prepared service and existing SSH access")
	}
	e.mu.Lock()
	if e.closed || ctx.Err() != nil || attempt != e.attempt {
		e.mu.Unlock()
		client.Close()
		if transport != nil {
			_ = transport.Close()
		}
		return errors.New("APP product connection ended")
	}
	e.connectCancel = nil
	e.client, e.transport, e.identity, e.state = client, transport, identity, state
	e.connection, e.issue = "ready", ""
	e.notifyLocked()
	watchCtx, watchCancel := context.WithCancel(e.life)
	e.watchCancel = watchCancel
	e.mu.Unlock()
	// Only original receipts are queried after a reconnect; no command is replayed.
	e.reconcile(ctx, client)
	go e.watch(watchCtx, client, state.Cursor)
	return nil
}

func (e *productEngine) watch(ctx context.Context, client nativeProductClient, cursor productrpc.Cursor) {
	for {
		state, err := client.Watch(ctx, cursor)
		if ctx.Err() != nil {
			return
		}
		e.mu.Lock()
		if e.client != client {
			e.mu.Unlock()
			return
		}
		if err != nil || state.Scope != e.identity.Scope || state.Cursor.Generation != e.identity.Generation {
			e.offlineLocked("reconnect_required")
			e.mu.Unlock()
			return
		}
		if state.Cursor != cursor {
			e.state = state
			e.notifyLocked()
		}
		cursor = state.Cursor
		e.mu.Unlock()
	}
}

func (e *productEngine) Snapshot() api.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, _ := json.Marshal(e.state.Snapshot)
	var snapshot api.Snapshot
	_ = json.Unmarshal(b, &snapshot)
	snapshot.Revision = e.revision
	if e.connection != "ready" {
		snapshot.Connection = e.connection
		snapshot.ConnectionIssue = "remote_product:" + e.issue
		snapshot.CanSend, snapshot.CanSteer, snapshot.CanInterrupt, snapshot.LoginPending = false, false, false, false
		snapshot.Approvals = nil
	}
	if e.unresolved > 0 {
		snapshot.CanSend, snapshot.CanSteer, snapshot.CanInterrupt = false, false, false
		snapshot.Approvals = nil
		snapshot.Phase = "unknown"
		snapshot.ConnectionIssue = "remote_product:outcome_unknown"
	}
	if !e.identity.Capabilities.Interrupt {
		snapshot.CanInterrupt = false
	}
	return snapshot
}

func (e *productEngine) WaitSnapshot(ctx context.Context, revision uint64) (api.Snapshot, error) {
	for {
		e.mu.Lock()
		changed, current, closed := e.changed, e.revision, e.closed
		e.mu.Unlock()
		if closed {
			return api.Snapshot{}, context.Canceled
		}
		if revision != current {
			return e.Snapshot(), nil
		}
		select {
		case <-ctx.Done():
			return api.Snapshot{}, ctx.Err()
		case <-changed:
		}
	}
}

func (e *productEngine) Revision() uint64 { e.mu.Lock(); defer e.mu.Unlock(); return e.revision }
func (e *productEngine) ProviderInfo() api.ProviderInfo {
	return api.ProviderInfo{ID: "remote-product", Name: e.pairing.Label, ConnectionKind: "remote-product"}
}

func (e *productEngine) TaskSummaries() []api.TaskSummary {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.state.TaskSummaries)
}

func (e *productEngine) refresh(ctx context.Context, client nativeProductClient) error {
	state, err := client.State(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client != client || state.Scope != e.identity.Scope || state.Cursor.Generation != e.identity.Generation {
		return errors.New("remote product generation changed")
	}
	e.state = state
	e.notifyLocked()
	return nil
}

func (e *productEngine) writeReceipts() error {
	e.mu.Lock()
	e.unresolved = len(e.receipts.Pending)
	e.notifyLocked()
	e.mu.Unlock()
	file := filepath.Join(e.root, "product-client-receipts.json")
	if err := localstate.Write(file, e.receipts); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(file))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (e *productEngine) reconcile(ctx context.Context, client nativeProductClient) {
	for id, pending := range e.receipts.Pending {
		result, err := client.Receipt(ctx, id)
		if err != nil || result.ID != id {
			continue
		}
		if result.Outcome == "accepted" || result.Outcome == "rejected" {
			delete(e.receipts.Pending, id)
			_ = e.writeReceipts()
		}
		if pending.Kind == "submit" {
			e.mu.Lock()
			e.state.Snapshot.LastReceipt = api.Receipt{ID: id, Outcome: result.Outcome, Message: result.Code}
			e.notifyLocked()
			e.mu.Unlock()
		}
	}
}

func (e *productEngine) command(ctx context.Context, command productrpc.Command) (productrpc.Result, error) {
	return e.commandWithDigest(ctx, command, "")
}

func (e *productEngine) commandWithDigest(ctx context.Context, command productrpc.Command, sourceDigest string) (productrpc.Result, error) {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	client, ready := e.client, e.connection == "ready"
	e.mu.Unlock()
	if client == nil || !ready {
		return productrpc.Result{ID: command.ID, Outcome: "rejected", Code: "offline"}, errors.New("reconnect the remote Bot before continuing")
	}
	if command.ID == "" {
		command.ID = rand.Text()
	}
	if !productIdentifier.MatchString(command.ID) {
		return productrpc.Result{}, errors.New("product command identity is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	stop := context.AfterFunc(e.life, cancel)
	defer stop()
	if command.Kind != "save-draft" {
		b, _ := json.Marshal(command)
		sum := sha256.Sum256(b)
		digest := hex.EncodeToString(sum[:])
		if pending, exists := e.receipts.Pending[command.ID]; exists {
			if pending.Digest != digest {
				return productrpc.Result{}, errors.New("original product command identity is already bound")
			}
			return client.Receipt(ctx, command.ID)
		}
		if len(e.receipts.Pending) > 0 {
			return productrpc.Result{ID: command.ID, Outcome: "rejected", Code: "original-outcome-unknown"}, errors.New("reconcile the original remote operation before issuing another")
		}
		if len(e.receipts.Pending) >= 128 {
			return productrpc.Result{}, errors.New("reconcile uncertain product commands before continuing")
		}
		e.receipts.Pending[command.ID] = productPending{Kind: command.Kind, Digest: digest, SourceDigest: sourceDigest}
		if err := e.writeReceipts(); err != nil {
			return productrpc.Result{ID: command.ID, Outcome: "rejected"}, errors.New("original product command receipt could not be preserved")
		}
	}
	result, err := client.Command(ctx, command)
	if err != nil || result.Outcome != "accepted" && result.Outcome != "rejected" {
		e.mu.Lock()
		e.offlineLocked("outcome_unknown")
		e.mu.Unlock()
		return productrpc.Result{ID: command.ID, Outcome: "unknown", Code: "response-unobserved"}, errors.New("remote operation outcome is unknown; reconnect to check its original receipt")
	}
	delete(e.receipts.Pending, command.ID)
	_ = e.writeReceipts()
	_ = e.refresh(ctx, client)
	if result.Outcome != "accepted" {
		return result, errors.New("remote Bot rejected this operation; refresh its current state")
	}
	return result, nil
}

func (e *productEngine) Submit(ctx context.Context, input api.Submission, files []api.InputFile) (api.Receipt, error) {
	if !productIdentifier.MatchString(input.ID) {
		return api.Receipt{ID: input.ID, Outcome: "rejected"}, errors.New("product submission identity is required")
	}
	e.mu.Lock()
	client, ready, caps := e.client, e.connection == "ready", e.identity.Capabilities
	e.mu.Unlock()
	if !ready || client == nil {
		return api.Receipt{ID: input.ID, Outcome: "rejected"}, errors.New("remote Bot is offline")
	}
	if len(files) > 8 || len(files) > 0 && !caps.Files {
		return api.Receipt{ID: input.ID, Outcome: "rejected"}, errors.New("remote attachment capability is unavailable")
	}
	b, _ := json.Marshal(input)
	sourceHash := sha256.Sum256(b)
	sourceDigest := hex.EncodeToString(sourceHash[:])
	e.op.Lock()
	pending, exists := e.receipts.Pending[input.ID]
	if exists {
		if pending.Kind != "submit" || pending.SourceDigest != sourceDigest {
			e.op.Unlock()
			return api.Receipt{ID: input.ID, Outcome: "rejected"}, errors.New("original submission identity is already bound")
		}
		result, err := client.Receipt(ctx, input.ID)
		e.op.Unlock()
		return api.Receipt{ID: input.ID, Outcome: result.Outcome, Message: result.Code}, err
	}
	if len(e.receipts.Pending) > 0 {
		e.op.Unlock()
		return api.Receipt{ID: input.ID, Outcome: "rejected"}, errors.New("original remote operation remains unknown; no replacement message was sent")
	}
	e.op.Unlock()
	input.FileIDs = nil
	for _, file := range files {
		f, err := os.Open(file.Path)
		if err != nil {
			return api.Receipt{ID: input.ID, Outcome: "rejected"}, errors.New("selected attachment is unavailable")
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > productrpc.MaxResourceBytes {
			f.Close()
			return api.Receipt{ID: input.ID, Outcome: "rejected"}, errors.New("selected attachment exceeds the remote resource capability")
		}
		b, err := io.ReadAll(io.LimitReader(f, productrpc.MaxResourceBytes+1))
		f.Close()
		if err != nil || len(b) > productrpc.MaxResourceBytes {
			return api.Receipt{ID: input.ID, Outcome: "rejected"}, errors.New("selected attachment could not be read")
		}
		resource, err := client.Upload(ctx, file.Name, b)
		if err != nil {
			return api.Receipt{ID: input.ID, Outcome: "rejected"}, errors.New("remote attachment delivery was not confirmed; no message was sent")
		}
		input.FileIDs = append(input.FileIDs, resource.ID)
	}
	result, err := e.commandWithDigest(ctx, productrpc.Command{ID: input.ID, Kind: "submit", Submission: &input}, sourceDigest)
	receipt := api.Receipt{ID: input.ID, Outcome: result.Outcome, Message: result.Code}
	if result.Submission != nil {
		receipt = *result.Submission
		receipt.ID = input.ID
	}
	return receipt, err
}

func (e *productEngine) Interrupt(ctx context.Context) error {
	e.mu.Lock()
	turn, allowed := e.state.Snapshot.CurrentTurn, e.connection == "ready" && e.identity.Capabilities.Interrupt && e.state.Snapshot.CanInterrupt
	e.mu.Unlock()
	if !allowed || turn == "" {
		return errors.New("remote exact interruption is unavailable")
	}
	_, err := e.command(ctx, productrpc.Command{Kind: "interrupt", Turn: turn})
	return err
}

func (e *productEngine) Decide(ctx context.Context, decision api.Decision) error {
	e.mu.Lock()
	target := e.state.ApprovalTargets[decision.ID]
	exists := slices.ContainsFunc(e.state.Snapshot.Approvals, func(a api.Approval) bool {
		return a.ID == decision.ID && slices.ContainsFunc(a.Choices, func(c api.Choice) bool { return c.ID == decision.Choice })
	})
	e.mu.Unlock()
	if target == "" || !exists {
		return errors.New("remote approval changed; refresh its current choices")
	}
	_, err := e.command(ctx, productrpc.Command{Kind: "decide", Decision: &productrpc.ApprovalDecision{Target: target, Decision: decision}})
	return err
}

func (e *productEngine) LoadEarlier(ctx context.Context) error {
	_, err := e.command(ctx, productrpc.Command{Kind: "load-earlier"})
	return err
}

func (e *productEngine) Initialization() api.BotInitialization {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.connection != "ready" && e.state.Initialization.Status == "" {
		return api.BotInitialization{Status: "loading", Message: "Remote Bot connection is offline"}
	}
	return e.state.Initialization
}

func (e *productEngine) Initialize(ctx context.Context, introduction api.BotIntroduction) (api.BotInitialization, error) {
	_, err := e.command(ctx, productrpc.Command{Kind: "initialize", Introduction: &introduction})
	return e.Initialization(), err
}
func (e *productEngine) RetryInitialization(ctx context.Context) (api.BotInitialization, error) {
	_, err := e.command(ctx, productrpc.Command{Kind: "retry-introduction"})
	return e.Initialization(), err
}
func (e *productEngine) Draft() api.Draft {
	e.mu.Lock()
	defer e.mu.Unlock()
	draft := e.state.Draft
	draft.ReferenceIDs = slices.Clone(draft.ReferenceIDs)
	return draft
}
func (e *productEngine) SaveDraft(draft api.Draft) (api.Draft, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := e.command(ctx, productrpc.Command{Kind: "save-draft", Draft: &draft})
	return e.Draft(), err
}

func (e *productEngine) Artifact(id string) (string, error) {
	e.mu.Lock()
	client, scope, ready := e.client, e.identity.Scope, e.connection == "ready"
	var name string
	for _, item := range e.state.Snapshot.Items {
		for _, artifact := range item.Artifacts {
			if artifact.ID == id {
				name = artifact.Name
			}
		}
	}
	e.mu.Unlock()
	if client == nil || !ready || name == "" {
		return "", errors.New("current remote artifact is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	meta, b, err := client.Download(ctx, id)
	if err != nil || meta.ID != id || meta.Name != name || name != filepath.Base(name) || name == "." || name == ".." || strings.ContainsAny(name, "\x00/\\\r\n") {
		return "", errors.New("remote task artifact could not be verified")
	}
	if err = e.refresh(ctx, client); err != nil {
		return "", errors.New("remote task artifact generation changed")
	}
	e.mu.Lock()
	same := e.client == client && e.identity.Scope == scope && e.connection == "ready"
	var current bool
	for _, item := range e.state.Snapshot.Items {
		for _, a := range item.Artifacts {
			if a.ID == id && a.Name == name {
				current = true
			}
		}
	}
	e.mu.Unlock()
	if !same || !current {
		return "", errors.New("remote task artifact ownership changed")
	}
	sum := sha256.Sum256(b)
	if meta.Size < 0 || meta.Size > productrpc.MaxResourceBytes || meta.Size != int64(len(b)) || meta.SHA256 != hex.EncodeToString(sum[:]) {
		return "", errors.New("remote artifact resource integrity failed")
	}
	directory := filepath.Join(e.root, "ProductClientResources", "Artifacts")
	for _, candidate := range []string{filepath.Dir(directory), directory} {
		info, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			if err = os.Mkdir(candidate, 0700); err != nil {
				return "", errors.New("private product download destination is unavailable")
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("product download destination must be private")
		}
	}

	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("product download destination must be private")
	}
	owned, err := os.MkdirTemp(directory, "artifact-")
	if err != nil {
		return "", err
	}
	destination := filepath.Join(owned, name)
	if err := os.WriteFile(destination, b, 0600); err != nil {
		os.RemoveAll(owned)
		return "", errors.New("product download could not be saved")
	}
	return destination, nil
}

func (e *productEngine) detach(ctx context.Context) error {
	e.mu.Lock()
	client, transport, cancel, connectCancel := e.client, e.transport, e.watchCancel, e.connectCancel
	e.client, e.transport, e.watchCancel, e.connectCancel = nil, nil, nil, nil
	e.attempt++
	e.offlineLocked("detached")
	e.mu.Unlock()
	if connectCancel != nil {
		connectCancel()
	}
	if cancel != nil {
		cancel()
	}
	if client != nil {
		client.Close()
	}
	if transport != nil {
		return transport.Close()
	}
	return nil
}

func (e *productEngine) Close(ctx context.Context) error {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	e.cancel()
	return e.detach(ctx)
}

var _ api.Engine = (*productEngine)(nil)
var _ api.BotInitializer = (*productEngine)(nil)
var _ backend.ProductDraftPort = (*productEngine)(nil)
