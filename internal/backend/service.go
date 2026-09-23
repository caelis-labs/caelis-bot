package backend

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Service is the Wails boundary. Engine owns execution; desktop owns surfaces and
// selection. Neither panel visibility nor renderer lifetime closes this service.
type Service struct {
	providers                   []api.ProviderInfo
	probeRuntime                func(context.Context, api.RuntimeSettings) error
	manageRuntime               func(context.Context, string, api.RuntimeSettings) (api.RuntimeStatus, error)
	switchGuard                 func() error
	executionFile               string
	executionSettings           api.ExecutionSettings
	configurationMu             sync.Mutex
	runtimeFile                 string
	runtimeSettings             api.RuntimeSettings
	pendingDraft                *api.Submission
	botStatus                   func() string
	mu                          sync.Mutex
	draft                       api.Draft
	draftFile                   string
	draftLoadError              error
	draftNotice                 string
	dismissed, presentationFile string
	engine                      api.Engine
	files                       func([]string) ([]api.InputFile, error)
	consumeFiles                func([]string)
	openURL                     func(string) error
	reveal                      func(string) error
}

func NewService(engine api.Engine, files func([]string) ([]api.InputFile, error), consume func([]string), openURL, reveal func(string) error) *Service {
	return &Service{engine: engine, files: files, consumeFiles: consume, openURL: openURL, reveal: reveal, draft: api.Draft{ReferenceIDs: []string{}}}
}
func (s *Service) Snapshot() api.Snapshot {
	v := s.engine.Snapshot()
	return s.decorate(v)
}
func (s *Service) decorate(v api.Snapshot) api.Snapshot {
	s.mu.Lock()
	pending := s.pendingDraft
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
		s.clearDraft(*pending)
	}
	if status != nil {
		v.BotStatus = status()
		if v.Message == "" {
			v.Message = v.BotStatus
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
		if len(text) > 1000 {
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
	return snapshot
}
func (s *Service) ChatSnapshot(revision uint64, botStatus string) api.ChatUpdate {
	s.mu.Lock()
	status := s.botStatus
	s.mu.Unlock()
	currentStatus := ""
	if status != nil {
		currentStatus = status()
	}
	if source, ok := s.engine.(api.RevisionSource); ok && revision != 0 && source.Revision() == revision && currentStatus == botStatus {
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
func (s *Service) LoadEarlier(ctx context.Context) error {
	if source, ok := s.engine.(api.HistorySource); ok {
		return source.LoadEarlier(ctx)
	}
	return errors.New("当前接入暂不支持读取更早消息")
}
func (s *Service) Connect(ctx context.Context) error { return s.engine.Connect(ctx) }
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
	files, err := s.files(input.FileIDs)
	if err != nil {
		return api.Receipt{ID: input.ID, Outcome: "rejected", Message: err.Error()}, nil
	}
	s.mu.Lock()
	s.pendingDraft = &input
	s.mu.Unlock()
	receipt, err := s.engine.Submit(ctx, input, files)
	if receipt.Outcome == "accepted" {
		s.consumeFiles(input.FileIDs)
		s.clearDraft(input)
	}
	return receipt, err
}
func (s *Service) Interrupt(ctx context.Context) error              { return s.engine.Interrupt(ctx) }
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
