package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Presentation state is shared by all renderers. It cannot approve or cancel work.
// Drafts survive surface switches and normal restarts; restoration never sends.
func (s *Service) Draft() api.Draft {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.draft
	d.Notice = s.draftNotice
	d.ReferenceIDs = slices.Clone(d.ReferenceIDs)
	return d
}
func (s *Service) SaveDraft(d api.Draft) (api.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draftLoadError != nil {
		return s.draft, s.draftLoadError
	}
	if d.Revision != s.draft.Revision {
		return s.draft, errors.New("草稿已在另一处更新，请重新打开输入框")
	}
	if len(d.Text) > 256*1024 || len(d.ReferenceIDs) > 64 {
		return s.draft, errors.New("内容过长")
	}
	d.Revision++
	d.Notice = "" // Only the host can issue persistence notices.
	d.ReferenceIDs = slices.Clone(d.ReferenceIDs)
	if err := s.persistDraft(d); err != nil {
		return s.draft, err
	}
	s.draftNotice = ""
	s.draft = d
	return d, nil
}
func (s *Service) clearDraft(input api.Submission) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft.Text == input.Text && slices.Equal(s.draft.ReferenceIDs, input.ReferenceIDs) {
		d := api.Draft{Revision: s.draft.Revision + 1, ReferenceIDs: []string{}}
		if s.draftLoadError != nil || s.persistDraft(d) != nil {
			s.draftNotice = "消息已发送，但草稿清理未能保存；请勿重复发送。"
			return
		}
		s.draft, s.draftNotice = d, ""
	}
}

type draftDocument struct {
	Version int       `json:"version"`
	Draft   api.Draft `json:"draft"`
}

func (s *Service) persistDraft(d api.Draft) error {
	if err := localstate.Write(s.draftFile, draftDocument{Version: 1, Draft: d}); err != nil {
		return errors.New("草稿暂未保存，请保留当前内容后重试")
	}
	return nil
}
func (s *Service) ConfigureDraft(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.draftFile = path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var stored draftDocument
	if err != nil || len(b) > 1024*1024 || json.Unmarshal(b, &stored) != nil || stored.Version != 1 || len(stored.Draft.Text) > 256*1024 || len(stored.Draft.ReferenceIDs) > 64 {
		s.draftLoadError = errors.New("本机草稿无法读取，原文件已保留；请检查 draft.json 后重新启动")
		s.draftNotice = s.draftLoadError.Error()
		return s.draftLoadError
	}
	s.draft = stored.Draft
	s.draft.Notice = ""
	return nil
}
func previewKey(v api.Snapshot) string {
	// Stable across restarts and revision-only updates. A different outcome cannot
	// inherit an acknowledgement made against an older result.
	var lastUser, lastAssistant string
	for _, i := range v.Items {
		if i.Kind == "user" {
			lastUser = i.ID
			lastAssistant = ""
		}
		if i.Kind == "assistant" {
			lastAssistant = i.ID + "\x00" + i.Text
		}
	}
	if lastUser == "" && lastAssistant == "" {
		return ""
	}
	h := sha256.Sum256([]byte(lastUser + "\x00" + lastAssistant))
	return hex.EncodeToString(h[:])
}
func (s *Service) presentation(v api.Snapshot) api.Snapshot {
	v.PreviewKey = previewKey(v)
	s.mu.Lock()
	v.PreviewDismissed = v.PreviewKey != "" && v.PreviewKey == s.dismissed
	s.mu.Unlock()
	return v
}

// ConfigurePresentation loads only an acknowledgement, never conversation bytes.
func (s *Service) ConfigurePresentation(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presentationFile = path
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	return json.Unmarshal(b, &s.dismissed)
}
func (s *Service) DismissPreview(key string) error {
	v := s.engine.Snapshot()
	if v.CanInterrupt || v.Phase == "unknown" || v.Phase == "sending" {
		return errors.New("当前工作尚未结束")
	}
	for _, a := range v.Approvals {
		if a.Status != "resolved" {
			return errors.New("还有待处理的决定")
		}
	}
	if key == "" || key != previewKey(v) {
		return errors.New("消息已更新，请再试一次")
	}
	if consumer, ok := s.engine.(api.PresentationAcknowledger); ok {
		if e := consumer.AcknowledgePresentation(context.Background(), v); e != nil {
			return e
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.presentationFile != "" {
		if err := os.MkdirAll(filepath.Dir(s.presentationFile), 0700); err != nil {
			return err
		}
		b, _ := json.Marshal(key)
		f, e := os.CreateTemp(filepath.Dir(s.presentationFile), ".preview-*")
		if e != nil {
			return e
		}
		defer os.Remove(f.Name())
		if _, e = f.Write(b); e == nil {
			e = f.Sync()
		}
		c := f.Close()
		if e == nil {
			e = c
		}
		if e == nil {
			e = os.Rename(f.Name(), s.presentationFile)
		}
		if e != nil {
			return e
		}
	}
	s.dismissed = key
	return nil
}
