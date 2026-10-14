package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// RuntimeBinding is prepared without changing the resident service. A corrupt
// target draft or configuration cannot displace the current conversation.
type RuntimeBinding struct {
	Engine                                                            api.Engine
	Settings                                                          api.RuntimeSettings
	RuntimeFile, ExecutionFile, WorkFile, DraftFile, PresentationFile string
	Execution                                                         api.ExecutionSettings
	Work                                                              api.WorkExecutionSettings
	Draft                                                             api.Draft
	DraftSend                                                         *draftSend
	Dismissed                                                         string
}

func PrepareRuntimeBinding(root string, settings api.RuntimeSettings, defaults api.ExecutionSettings, engine api.Engine) (RuntimeBinding, error) {
	dir := root
	if settings.Runtime != "codex" {
		dir = filepath.Join(root, "providers", settings.Runtime)
	}
	b := RuntimeBinding{Engine: engine, Settings: settings, RuntimeFile: filepath.Join(root, "runtime.json"), ExecutionFile: filepath.Join(dir, "execution.json"), WorkFile: filepath.Join(dir, "work-execution.json"), DraftFile: filepath.Join(dir, "draft.json"), PresentationFile: filepath.Join(dir, "preview.json"), Draft: api.Draft{ReferenceIDs: []string{}}}
	var err error
	if b.Execution, err = LoadExecutionSettings(b.ExecutionFile, defaults); err != nil {
		return b, err
	}
	if b.Work, err = LoadWorkExecutionSettings(b.WorkFile); err != nil {
		return b, err
	}
	if data, e := os.ReadFile(b.DraftFile); e == nil {
		var stored draftDocument
		if len(data) > 1024*1024 || json.Unmarshal(data, &stored) != nil || (stored.Version != 1 && stored.Version != 2) || len(stored.Draft.Text) > 256*1024 || len(stored.Draft.ReferenceIDs) > 64 || (stored.Send != nil && (stored.Send.ID == "" || len(stored.Send.FileIDs) == 0 || len(stored.Send.FileIDs) > 8 || !slices.Contains([]string{"", "accepted", "rejected"}, stored.Send.Outcome))) {
			return b, errors.New("目标连接的草稿无法读取，原文件已保留")
		}
		b.Draft, b.DraftSend = stored.Draft, stored.Send
	} else if !errors.Is(e, os.ErrNotExist) {
		return b, e
	}
	if data, e := os.ReadFile(b.PresentationFile); e == nil {
		if json.Unmarshal(data, &b.Dismissed) != nil {
			return b, errors.New("目标连接的预览状态无法读取，原文件已保留")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return b, e
	}
	return b, nil
}

func (s *Service) CurrentRuntimeBinding() RuntimeBinding {
	s.configurationMu.Lock()
	s.mu.Lock()
	b := RuntimeBinding{Engine: s.currentEngine(), Settings: s.runtimeSettings, RuntimeFile: s.runtimeFile, ExecutionFile: s.executionFile, WorkFile: s.workExecutionFile, DraftFile: s.draftFile, PresentationFile: s.presentationFile, Execution: s.executionSettings, Work: s.workExecutionSettings, Draft: s.draft, DraftSend: s.draftSend, Dismissed: s.dismissed}
	s.mu.Unlock()
	s.configurationMu.Unlock()
	return b
}

// BeginRuntimeSwitch fences new submissions and recovery before the old owner
// is paused. The caller must finish or cancel this transaction.
func (s *Service) BeginRuntimeSwitch(guard func() error) error {
	s.admission.Lock()
	defer s.admission.Unlock()
	if s.restarting {
		return errors.New("请先完成当前连接操作")
	}
	if err := guard(); err != nil {
		return err
	}
	s.mu.Lock()
	unsafeDraft := s.draftSend != nil || s.pendingDraft != nil
	for _, item := range s.outbox {
		if item.item.Status == "sending" || item.item.Status == "unknown" {
			unsafeDraft = true
		}
	}
	s.mu.Unlock()
	if unsafeDraft {
		return errors.New("上一条消息的结果还未确认，请先核对后切换")
	}
	s.recoveryMu.Lock()
	recovering := s.recoveryFlight != nil
	s.recoveryMu.Unlock()
	if recovering {
		return errors.New("当前连接正在恢复，请稍后切换")
	}
	s.restarting = true
	return nil
}

func (s *Service) EndRuntimeSwitch() {
	s.admission.Lock()
	s.restarting = false
	s.admission.Unlock()
}

// ConnectPreparedRuntime is used only while admission is fenced. It performs
// the normal adapter handshake without admitting a user turn before commit.
func (s *Service) ConnectPreparedRuntime(ctx context.Context) error {
	s.admission.RLock()
	fenced := s.restarting || s.setupRequired
	s.admission.RUnlock()
	if !fenced {
		return errors.New("连接切换未准备好")
	}
	return s.currentEngine().Connect(ctx)
}

// BindRuntime changes only this Bot client's adapter and provider-scoped
// presentation. It never starts, stops, or reconfigures the shared Host.
func (s *Service) BindRuntime(b RuntimeBinding) {
	s.configurationMu.Lock()
	s.mu.Lock()
	s.runtimeFile, s.runtimeSettings = b.RuntimeFile, b.Settings
	s.runtimeLoadError = nil
	s.executionFile, s.executionSettings = b.ExecutionFile, b.Execution
	s.workExecutionFile, s.workExecutionSettings = b.WorkFile, b.Work
	s.draftFile, s.draft, s.draftSend = b.DraftFile, b.Draft, b.DraftSend
	s.draftLoadError, s.draftNotice, s.pendingDraft = nil, "", nil
	s.presentationFile, s.dismissed = b.PresentationFile, b.Dismissed
	s.outbox = nil
	s.botStatus, s.submitUser = nil, nil
	s.presentationRevision++
	s.mu.Unlock()
	s.configurationMu.Unlock()
	s.setEngine(b.Engine)
	s.recoveryMu.Lock()
	s.recoveryGeneration++
	s.recoveryMu.Unlock()
}
