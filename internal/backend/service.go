package backend

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/chatlog"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"github.com/caelis-labs/caelis-bot/internal/messageimage"
	"github.com/caelis-labs/caelis-bot/internal/screeninput"
)

// Service is the Wails boundary. Engine owns execution; desktop owns surfaces and
// selection. Neither panel visibility nor renderer lifetime closes this service.
type Service struct {
	chat                        *chatlog.Log
	localWorkers                api.LocalWorkerController
	machines                    api.MachineController
	admission                   sync.RWMutex
	recoveryMu                  sync.Mutex
	recoveryFlight              *recoveryFlight
	recoveryGeneration          uint64
	restarting                  bool
	setupRequired               bool
	setup                       api.SetupController
	providers                   []api.ProviderInfo
	probeRuntime                func(context.Context, api.RuntimeSettings) error
	manageRuntime               func(context.Context, string, api.RuntimeSettings) (api.RuntimeStatus, error)
	switchGuard                 func() error
	executionFile               string
	executionSettings           api.ExecutionSettings
	workExecutionFile           string
	workExecutionSettings       api.WorkExecutionSettings
	configurationMu             sync.Mutex
	runtimeFile                 string
	runtimeSettings             api.RuntimeSettings
	runtimeLoadError            error
	pendingDraft                *api.Submission
	pendingDraftRevision        uint64
	outbox                      []outgoingMessage
	botStatus                   func() string
	beforeInterrupt             func()
	mu                          sync.Mutex
	draft                       api.Draft
	draftFile                   string
	draftLoadError              error
	draftNotice                 string
	dismissed, presentationFile string
	initializer                 api.BotInitializer
	engine                      api.Engine
	submitUser                  func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)
	files                       func([]string) ([]api.InputFile, error)
	consumeFiles                func([]string)
	submissionObserver          func(api.Submission, []api.InputFile, api.Receipt)
	openURL                     func(string) error
	reveal                      func(string) error
	screenMedia                 *screeninput.Media
	screenMediaError            error
	messageMedia                *messageimage.Store
	messageMediaError           error
}

type recoveryFlight struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

func NewService(engine api.Engine, files func([]string) ([]api.InputFile, error), consume func([]string), openURL, reveal func(string) error) *Service {
	return &Service{engine: engine, files: files, consumeFiles: consume, openURL: openURL, reveal: reveal, draft: api.Draft{ReferenceIDs: []string{}}}
}
func (s *Service) Snapshot() api.Snapshot {
	v := s.engine.Snapshot()
	v = s.decorate(v)
	if s.chat != nil {
		s.chat.Observe(v.Items)
		items, earlier := s.chat.Snapshot()
		for _, item := range v.Items {
			if item.Kind != "user" && item.Kind != "assistant" {
				items = append(items, item)
			}
		}
		v.Items, v.HasEarlier = items, earlier
		v.Revision += s.chat.Revision()
	}
	return v
}
func (s *Service) decorate(v api.Snapshot) api.Snapshot {
	v.Activity = currentActivity(v)
	s.mu.Lock()
	v = s.presentOutgoing(v)
	pending := s.pendingDraft
	pendingRevision := s.pendingDraftRevision
	status := s.botStatus
	if pending != nil && v.LastReceipt.ID == pending.ID && v.LastReceipt.Outcome == "accepted" {
		s.pendingDraft = nil
	} else {
		pending = nil
	}
	s.mu.Unlock()
	if pending != nil {
		if s.consumeFiles != nil {
			s.consumeFiles(pending.FileIDs)
		}
		s.clearDraftAtRevision(*pending, pendingRevision)
	}
	if status != nil {
		v.BotStatus = status()
		if v.Message == "" {
			v.Message = v.BotStatus
		}
	}
	// Copy presentation items so native history and model context stay intact.
	v.Items = append([]api.Item(nil), v.Items...)
	for i := range v.Items {
		v.Items[i] = screeninput.Present(v.Items[i])
		if v.Items[i].Screen != nil {
			v.Items[i].Screen.Images = s.screenMedia.Images(v.Items[i].RequestID)
		} else if v.Items[i].Kind == "user" {
			v.Items[i].Media = s.messageMedia.Presentation(v.Items[i].RequestID)
		}
	}
	return s.presentation(v)
}
func (s *Service) SetBotStatus(f func() string) { s.mu.Lock(); s.botStatus = f; s.mu.Unlock() }

// PetSnapshot bounds the default surface payload. History remains available on
// explicit request; a new user message starts a new preview boundary.
func (s *Service) PetSnapshot() api.Snapshot {
	var snapshot api.Snapshot
	if recent, ok := s.engine.(api.RecentSource); ok {
		snapshot = s.decorate(recent.RecentSnapshot())
	} else {
		snapshot = s.Snapshot()
	}
	items := make([]api.Item, 0, 3)
	for _, item := range snapshot.Items {
		if snapshot.CurrentTurn != "" && item.TurnKey != snapshot.CurrentTurn {
			continue
		}
		if item.Kind == "user" {
			items = items[:0]
		}
		if item.Kind != "user" && item.Kind != "assistant" && item.Kind != "activity" {
			continue
		}
		for i, old := range items {
			if old.Kind == item.Kind {
				items = append(items[:i], items[i+1:]...)
				break
			}
		}
		item.Details = ""
		item.Artifacts = nil
		text := []rune(item.Text)
		// The latest assistant response remains complete for hover reading;
		// bound the number of messages, not the Markdown source mid-structure.
		if item.Kind != "assistant" && len(text) > 1000 {
			item.Text = string(text[:1000]) + "…"
		}
		items = append(items, item)
	}
	snapshot.Items = items
	snapshot.References = nil
	approvals := make([]api.Approval, 0)
	for _, a := range snapshot.Approvals {
		if a.Status != "resolved" {
			approvals = append(approvals, a)
		}
	}
	snapshot.Approvals = approvals
	// The bubble acknowledges only the bounded, visible preview. Computing its
	// key before the current-turn filter can fence an invisible older result.
	return s.presentation(snapshot)
}
func (s *Service) ChatSnapshot(revision uint64, botStatus string) api.ChatUpdate {
	s.mu.Lock()
	status := s.botStatus
	s.mu.Unlock()
	currentStatus := ""
	if status != nil {
		currentStatus = status()
	}
	s.mu.Lock()
	hasOutgoing := len(s.outbox) > 0
	s.mu.Unlock()
	if source, ok := s.engine.(api.RevisionSource); s.chat == nil && ok && !hasOutgoing && revision != 0 && source.Revision() == revision && currentStatus == botStatus {
		return api.ChatUpdate{}
	}
	v := s.Snapshot()
	items := make([]api.Item, 0)
	for _, item := range v.Items {
		if item.Kind == "user" || item.Kind == "assistant" {
			item.Details = ""
			items = append(items, item)
		}
	}
	v.Items = items
	return api.ChatUpdate{Changed: true, Snapshot: v}
}
func (s *Service) ConfigureChat(path string) { s.chat = chatlog.Open(path) }
func (s *Service) ObserveChat(v api.Snapshot) {
	if s.chat != nil {
		s.chat.Observe(v.Items)
	}
}

func (s *Service) LoadEarlier(ctx context.Context) error {
	if s.chat != nil {
		return s.chat.LoadEarlier(ctx)
	}
	if source, ok := s.engine.(api.HistorySource); ok {
		return source.LoadEarlier(ctx)
	}
	return errors.New("当前接入暂不支持读取更早消息")
}
func (s *Service) Connect(ctx context.Context) error {
	return s.connect(ctx, "")
}

// RecoveryState is a host-only view of the current Runtime owner. Its fence
// changes on native reconnect and on every manual admission attempt.
func (s *Service) RecoveryState() api.RecoveryState {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	return s.recoveryStateLocked()
}

func (s *Service) recoveryStateLocked() api.RecoveryState {
	state, _ := s.recoveryStateWithNativeLocked()
	return state
}

func (s *Service) recoveryStateWithNativeLocked() (api.RecoveryState, string) {
	source, ok := s.engine.(api.RecoverySource)
	if !ok {
		return api.RecoveryState{}, ""
	}
	state := source.RecoveryState()
	if state.Fence == "" {
		return api.RecoveryState{}, ""
	}
	nativeFence := state.Fence
	state.Fence = fmt.Sprintf("%s:%d", state.Fence, s.recoveryGeneration)
	if s.recoveryFlight != nil {
		state.InProgress = true
		state.Manual = false
	}
	return state, nativeFence
}

// RecoverIfCurrent applies one manually selected original-owner recovery.
// Stale channel buttons cannot turn a newer Runtime generation into an action.
func (s *Service) RecoverIfCurrent(ctx context.Context, fence string) error {
	if fence == "" {
		return errors.New("recovery_target_changed")
	}
	return s.connect(ctx, fence)
}

func (s *Service) connect(ctx context.Context, fence string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := context.WithTimeout(ctx, 35*time.Second)
	defer stop()
	if err := lockwait.RLock(ctx, &s.admission); err != nil {
		return err
	}
	defer s.admission.RUnlock()
	if err := lockwait.Lock(ctx, &s.configurationMu); err != nil {
		return err
	}
	configErr := s.runtimeLoadError
	if configErr != nil {
		_, exists := os.Stat(s.runtimeFile)
		if restored, err := LoadRuntimeSettings(s.runtimeFile, s.runtimeSettings.Runtime); exists == nil && err == nil && restored == s.runtimeSettings {
			s.runtimeLoadError, configErr = nil, nil
		}
	}
	s.configurationMu.Unlock()
	if configErr != nil {
		return configErr
	}
	if s.restarting || s.setupRequired {
		return errors.New("请先完成运行时设置")
	}
	s.recoveryMu.Lock()
	state := s.recoveryStateLocked()
	if fence != "" && (!state.Manual || state.Fence != fence) {
		s.recoveryMu.Unlock()
		return errors.New("recovery_target_changed")
	}
	if state.Automatic {
		s.recoveryMu.Unlock()
		return errors.New("automatic_recovery_in_progress")
	}
	if flight := s.recoveryFlight; flight != nil {
		s.recoveryMu.Unlock()
		select {
		case <-flight.done:
			return flight.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	workCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	flight := &recoveryFlight{done: make(chan struct{}), cancel: cancel}
	s.recoveryFlight = flight
	s.recoveryGeneration++
	s.recoveryMu.Unlock()
	err := s.engine.Connect(workCtx)
	cancel()
	s.recoveryMu.Lock()
	flight.err = err
	s.recoveryFlight = nil
	close(flight.done)
	s.recoveryMu.Unlock()
	return err
}
func (s *Service) OpenConnectionHelp() error {
	return s.OpenMessageLink(s.ProviderInfo().HelpURL)
}
func (s *Service) ProviderInfo() api.ProviderInfo {
	if provider, ok := s.engine.(api.Provider); ok {
		return provider.ProviderInfo()
	}
	return api.ProviderInfo{}
}
func (s *Service) OpenMessageLink(value string) error {
	u, err := url.Parse(value)
	if err != nil || len(value) > 16*1024 || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || s.openURL == nil {
		return errors.New("此链接无法直接打开")
	}
	return s.openURL(u.String())
}
func (s *Service) Submit(ctx context.Context, input api.Submission) (api.Receipt, error) {
	s.admission.RLock()
	defer s.admission.RUnlock()
	if s.restarting || s.setupRequired {
		return api.Receipt{}, errors.New("请先完成运行时设置")
	}

	s.mu.Lock()
	initializer := s.initializer
	s.mu.Unlock()
	if initializer != nil && initializer.Initialization().Status != "accepted" {
		return api.Receipt{ID: input.ID, Outcome: "rejected", Message: "请先完成 Bot 初始化，并等待介绍发送完成"}, nil
	}

	files, err := s.files(input.FileIDs)
	if err != nil {
		return api.Receipt{ID: input.ID, Outcome: "rejected", Message: err.Error()}, nil
	}
	if err := s.retainMessageMedia(input, files); err != nil {
		return api.Receipt{ID: input.ID, Outcome: "rejected", Message: "图片预览存储暂不可用，消息未发送"}, nil
	}
	s.mu.Lock()
	s.pendingDraft = &input
	s.pendingDraftRevision = s.draft.Revision
	s.mu.Unlock()
	s.stageOutgoing(input, files)
	receipt, err := s.submit(ctx, input, files)
	s.finishOutgoing(input.ID, receipt)
	if receipt.Outcome == "accepted" {
		s.mu.Lock()
		pending := s.pendingDraft
		revision := s.pendingDraftRevision
		if pending != nil && pending.ID == input.ID {
			s.pendingDraft = nil
		} else {
			pending = nil
		}
		s.mu.Unlock()
		if pending != nil {
			if s.consumeFiles != nil {
				s.consumeFiles(pending.FileIDs)
			}
			s.clearDraftAtRevision(*pending, revision)
		}
	}
	return receipt, err
}
func (s *Service) SetUserSubmitter(f func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)) {
	s.mu.Lock()
	s.submitUser = f
	s.mu.Unlock()
}
func (s *Service) submit(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	s.mu.Lock()
	submit := s.submitUser
	observer := s.submissionObserver
	s.mu.Unlock()
	var receipt api.Receipt
	var err error
	if submit != nil {
		receipt, err = submit(ctx, in, files)
	} else {
		receipt, err = s.engine.Submit(ctx, in, files)
	}
	if observer != nil && receipt.Outcome == "accepted" {
		observer(in, files, receipt)
	}
	return receipt, err
}
func (s *Service) SetInterruptObserver(f func()) { s.mu.Lock(); s.beforeInterrupt = f; s.mu.Unlock() }
func (s *Service) Interrupt(ctx context.Context) error {
	s.mu.Lock()
	stop := s.beforeInterrupt
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	return s.engine.Interrupt(ctx)
}
func (s *Service) Decide(ctx context.Context, d api.Decision) error { return s.engine.Decide(ctx, d) }
func (s *Service) Login(ctx context.Context) error {
	auth, ok := s.engine.(api.Authenticator)
	if !ok {
		return errors.New("当前后端不支持在此登录")
	}
	u, err := auth.Login(ctx)
	if err != nil {
		return err
	}
	return s.openURL(u)
}
func (s *Service) CancelLogin(ctx context.Context) error {
	if auth, ok := s.engine.(api.Authenticator); ok {
		return auth.CancelLogin(ctx)
	}
	return errors.New("当前后端没有登录流程")
}
func (s *Service) RevealArtifact(id string) error {
	resolver, ok := s.engine.(api.ArtifactResolver)
	if !ok {
		return errors.New("当前后端不支持打开产物")
	}
	p, err := resolver.Artifact(id)
	if err != nil {
		return errors.New("文件已不可用")
	}
	return s.reveal(p)
}
func (s *Service) OpenApprovalURL(id string) error {
	navigator, ok := s.engine.(api.ApprovalNavigator)
	if !ok {
		return errors.New("当前后端不支持打开外部审批")
	}
	u, err := navigator.ApprovalURL(id)
	if err != nil {
		return err
	}
	return s.openURL(u)
}
func (s *Service) Shutdown() error {
	defer s.chat.Close()
	s.recoveryMu.Lock()
	if s.recoveryFlight != nil {
		s.recoveryFlight.cancel()
	}
	s.recoveryMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.engine.Close(ctx)
}

func (s *Service) ComposerSnapshot() api.Snapshot {
	if source, ok := s.engine.(api.ComposerSource); ok {
		return s.decorate(source.ComposerSnapshot())
	}
	return s.Snapshot()
}

// RuntimeDefaultModel is a read-only display hint, independent of Bot/Worker
// overrides. Unknown defaults stay null and never cause configuration writes.
func (s *Service) RuntimeDefaultModel() *api.WorkExecutionSettings {
	provider, ok := s.engine.(interface {
		RuntimeDefault(context.Context) (api.WorkExecutionSettings, error)
	})
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	v, err := provider.RuntimeDefault(ctx)
	if err != nil || v.Model == "" {
		return nil
	}
	return &v
}
