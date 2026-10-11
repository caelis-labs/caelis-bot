package backend

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/chatlog"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"github.com/caelis-labs/caelis-bot/internal/messageimage"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
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
	draftSend                   *draftSend
	draftReconcileMu            sync.Mutex
	outbox                      []outgoingMessage
	botStatus                   func() string
	beforeInterrupt             func()
	mu                          sync.Mutex
	presentationRevision        uint64 // Local visible state independent of the engine and chat log.
	draft                       api.Draft
	draftFile                   string
	draftLoadError              error
	draftNotice                 string
	dismissed, presentationFile string
	initializer                 api.BotInitializer
	engine                      api.Engine
	botPlugins                  func(context.Context) (plugins.Snapshot, error)
	submitUser                  func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)
	files                       func([]string) ([]api.InputFile, error)
	consumeFiles                func([]string) error
	submissionObserver          func(api.Submission, []api.InputFile, api.Receipt)
	commandHandler              func(context.Context, api.Submission) (api.Receipt, bool, error)
	controlNotices              func() ([]api.Item, uint64)
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

func NewService(engine api.Engine, files func([]string) ([]api.InputFile, error), consume func([]string) error, openURL, reveal func(string) error) *Service {
	return &Service{engine: engine, files: files, consumeFiles: consume, openURL: openURL, reveal: reveal, draft: api.Draft{ReferenceIDs: []string{}}}
}

// NativePlugins is queried only while the composer menu is open. An adapter
// without a scoped native catalog contributes no native plugin rows.
func (s *Service) NativePlugins() ([]api.NativePlugin, error) {
	s.admission.RLock()
	engine := s.engine
	s.admission.RUnlock()
	provider, ok := engine.(api.NativePluginSource)
	if !ok {
		return []api.NativePlugin{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return provider.NativePlugins(ctx)
}
func (s *Service) SetBotPluginSource(source func(context.Context) (plugins.Snapshot, error)) {
	s.mu.Lock()
	s.botPlugins = source
	s.mu.Unlock()
}

// A composer plugin reference is a visible per-turn request, not an execution
// grant. Resolve it against the host catalog at admission while retaining the
// original draft/receipt identity. Ordinary Skill references reach the adapter.
func (s *Service) resolvePluginReferences(ctx context.Context, input api.Submission) (api.Submission, error) {
	var botIDs, nativeIDs []string
	seen := map[string]bool{}
	remaining := make([]string, 0, len(input.ReferenceIDs))
	for _, id := range input.ReferenceIDs {
		if seen[id] {
			return input, errors.New("插件引用重复，请重新选择")
		}
		seen[id] = true
		switch {
		case strings.HasPrefix(id, "bot-plugin:"):
			botIDs = append(botIDs, strings.TrimPrefix(id, "bot-plugin:"))
		case strings.HasPrefix(id, "codex-plugin:"):
			nativeIDs = append(nativeIDs, strings.TrimPrefix(id, "codex-plugin:"))
		default:
			remaining = append(remaining, id)
		}
	}
	if len(botIDs)+len(nativeIDs) == 0 {
		return input, nil
	}
	if len(botIDs)+len(nativeIDs) > 8 {
		return input, errors.New("一次最多引用 8 个插件")
	}
	var names []string
	if len(botIDs) > 0 {
		s.mu.Lock()
		source := s.botPlugins
		s.mu.Unlock()
		if source == nil {
			return input, errors.New("插件目录暂不可用，请重新选择")
		}
		catalog, err := source(ctx)
		if err != nil || catalog.SyncState != "" {
			return input, errors.New("插件状态暂未就绪，请稍后再试")
		}
		for _, id := range botIDs {
			found := false
			for _, item := range catalog.Items {
				if item.ID == id && item.Installed && item.Enabled && slices.Contains([]string{"enabled", "ready", "update_available"}, item.Status) {
					names = append(names, "Bot: "+strconv.Quote(item.Title))
					found = true
					break
				}
			}
			if !found {
				return input, errors.New("引用的 Bot 插件已不可用，请重新选择")
			}
		}
	}
	if len(nativeIDs) > 0 {
		provider, ok := s.engine.(api.NativePluginSource)
		if !ok {
			return input, errors.New("当前运行时没有原生插件目录")
		}
		catalog, err := provider.NativePlugins(ctx)
		if err != nil {
			return input, errors.New("原生插件目录暂不可用，请稍后再试")
		}
		for _, id := range nativeIDs {
			found := false
			for _, item := range catalog {
				if item.ID == id && item.Source == "codex" {
					names = append(names, "Codex: "+strconv.Quote(item.Name))
					found = true
					break
				}
			}
			if !found {
				return input, errors.New("引用的原生插件已不可用，请重新选择")
			}
		}
	}
	input.ReferenceIDs = remaining
	hint := "\n\n引用插件：" + strings.Join(names, "；")
	if len(input.Text)+len(hint) > 256<<10 {
		return input, errors.New("消息和插件引用超过长度限制")
	}
	input.Text += hint
	return input, nil
}
func (s *Service) Snapshot() api.Snapshot {
	v := s.engine.Snapshot()
	// Capture local versions before projection. If an independent change lands
	// during projection, this result keeps the older revision and the next poll
	// must observe the newer state instead of accepting a stale projection.
	s.mu.Lock()
	localRevision := s.presentationRevision
	controlNotices := s.controlNotices
	s.mu.Unlock()
	v = s.decorate(v)
	if s.chat != nil {
		chatRevision := s.chat.Revision()
		items, earlier := s.chat.Snapshot()
		knownInput := make(map[string]bool, len(items))
		for _, item := range items {
			if item.Kind == "user" && item.RequestID != "" {
				knownInput[item.RequestID] = true
			}
		}
		for _, item := range v.Items {
			if item.Kind == "user" && strings.HasPrefix(item.ID, "outgoing:") && !knownInput[item.RequestID] {
				items = append(items, item) // only the transient sending bubble
			} else if item.Kind != "user" && item.Kind != "assistant" && item.Kind != "controlNotice" {
				items = append(items, item)
			}
		}
		v.Items, v.HasEarlier = items, earlier
		v.Revision += chatRevision
	}
	if controlNotices != nil && s.chat == nil {
		items, revision := controlNotices()
		v.Items = append(v.Items, items...)
		v.Revision += revision
	}
	v.Revision += localRevision
	return v
}
func (s *Service) decorate(v api.Snapshot) api.Snapshot {
	// Native automatic-review facts remain in adapter history and diagnostics.
	// They are not an independent user message or an actionable approval.
	v.Reviews = nil
	s.admission.RLock()
	setupRequired := s.setupRequired
	s.admission.RUnlock()
	if setupRequired && v.Connection != "ready" {
		v.ConnectionIssue = "setup_required"
	}
	v.Activity = currentActivity(v)
	s.mu.Lock()
	var pendingSend draftSend
	if s.draftSend != nil {
		pendingSend = *s.draftSend
	}
	s.mu.Unlock()
	if pendingSend.ID != "" && pendingSend.Outcome == "" {
		s.reconcileDraftReceipt(draftReceipt(v, pendingSend.ID))
	}
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
	localRevision := s.presentationRevision
	controlNotices := s.controlNotices
	needsReconcile := s.draftSend != nil && s.draftSend.Outcome == ""
	s.mu.Unlock()
	currentStatus := ""
	if status != nil {
		currentStatus = status()
	}
	if source, ok := s.engine.(api.RevisionSource); ok && !needsReconcile && revision != 0 && currentStatus == botStatus {
		combined := source.Revision() + localRevision
		if s.chat != nil {
			combined += s.chat.Revision()
		}
		if controlNotices != nil && s.chat == nil {
			_, noticeRevision := controlNotices()
			combined += noticeRevision
		}
		if combined == revision {
			return api.ChatUpdate{}
		}
	}
	v := s.Snapshot()
	// An unresolved original receipt can require a read even when no visible
	// state changed. That read must not cause a React update.
	if revision != 0 && v.Revision == revision && v.BotStatus == botStatus {
		return api.ChatUpdate{}
	}
	items := make([]api.Item, 0)
	for _, item := range v.Items {
		if item.Kind == "user" || item.Kind == "assistant" || item.Kind == "controlNotice" {
			item.Details = ""
			items = append(items, item)
		}
	}
	// The local IM keeps the display order independent of Runtime item order and
	// approval authority. Legacy receipts have no time, so keep them ahead of
	// newer chat instead of appending them after today's reply.
	slices.SortStableFunc(items, func(a, b api.Item) int {
		if a.SeenAt == 0 && b.SeenAt == 0 && a.Kind != b.Kind {
			if a.Kind == "controlNotice" {
				return -1
			}
			if b.Kind == "controlNotice" {
				return 1
			}
		}
		return cmp.Compare(a.SeenAt, b.SeenAt)
	})
	v.Items = items
	return api.ChatUpdate{Changed: true, Snapshot: v}
}
func (s *Service) SetControlNotices(source func() ([]api.Item, uint64)) {
	s.mu.Lock()
	s.controlNotices = source
	s.mu.Unlock()
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
	input.Quoted = api.BoundQuote(input.Quoted)
	s.mu.Lock()
	commandHandler := s.commandHandler
	draftRevision := s.draft.Revision
	s.mu.Unlock()
	// An explicit command in the new body stays a command. Quoted text is only
	// model context and is never parsed as a decision.
	if commandHandler != nil && len(input.FileIDs) == 0 && len(input.ReferenceIDs) == 0 {
		if receipt, handled, err := commandHandler(ctx, input); handled {
			if err == nil && receipt.Outcome == "accepted" {
				s.clearDraftAtRevision(input, draftRevision)
			}
			return receipt, err
		}
	}
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
	s.mu.Lock()
	blocked := s.draftSend != nil && (len(input.FileIDs) > 0 || s.draftSend.ID == input.ID)
	s.mu.Unlock()
	if blocked {
		return api.Receipt{ID: input.ID, Outcome: "rejected", Message: "先核对或清理上一条附件消息，不能再次发送原附件"}, nil
	}

	files, err := s.files(input.FileIDs)
	if err != nil {
		return api.Receipt{ID: input.ID, Outcome: "rejected", Message: err.Error()}, nil
	}
	projected, err := s.resolvePluginReferences(ctx, input)
	if err != nil {
		return api.Receipt{ID: input.ID, Outcome: "rejected", Message: err.Error()}, nil
	}
	if err := s.retainMessageMedia(projected, files); err != nil {
		return api.Receipt{ID: input.ID, Outcome: "rejected", Message: "图片预览存储暂不可用，消息未发送"}, nil
	}
	if len(input.FileIDs) > 0 {
		if err := s.reserveDraftSend(input); err != nil {
			s.messageMedia.Resolve(input.ID)
			return api.Receipt{ID: input.ID, Outcome: "rejected", Message: err.Error()}, nil
		}
	}
	s.mu.Lock()
	if len(input.FileIDs) == 0 {
		s.pendingDraft = &input
		s.pendingDraftRevision = s.draft.Revision
	}
	s.mu.Unlock()
	s.stageOutgoing(projected, files)
	receipt, err := s.submit(ctx, projected, files)
	if errors.Is(err, api.ErrRecoveryPending) {
		s.discardOutgoing(input.ID)
		if len(input.FileIDs) > 0 {
			s.reconcileDraftReceipt(api.Receipt{ID: input.ID, Outcome: "rejected"})
		} else {
			s.mu.Lock()
			if s.pendingDraft != nil && s.pendingDraft.ID == input.ID {
				s.pendingDraft = nil
			}
			s.mu.Unlock()
		}
		return receipt, err
	}
	s.finishOutgoing(input.ID, receipt)
	s.recordInput(input, files, receipt)
	if len(input.FileIDs) > 0 {
		s.reconcileDraftReceipt(receipt)
		return receipt, err
	}
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

func (s *Service) SetCommandHandler(handler func(context.Context, api.Submission) (api.Receipt, bool, error)) {
	s.mu.Lock()
	s.commandHandler = handler
	s.mu.Unlock()
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
