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
	// A read can reconcile the original native receipt and retry local cleanup.
	// It never dispatches another submission.
	s.mu.Lock()
	var pending draftSend
	if s.draftSend != nil {
		pending = *s.draftSend
	}
	s.mu.Unlock()
	if pending.ID != "" {
		receipt := api.Receipt{ID: pending.ID, Outcome: pending.Outcome}
		if receipt.Outcome == "" {
			receipt = draftReceipt(s.engine.Snapshot(), pending.ID)
			if receipt.Outcome == "" {
				receipt = api.Receipt{ID: pending.ID, Outcome: s.messageMedia.ReceiptStatus(pending.ID)}
			}
		}
		s.reconcileDraftReceipt(receipt)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.draft
	d.Notice = s.draftNotice
	if s.draftSend != nil {
		if s.draftSend.Outcome == "accepted" {
			d.CleanupPending = true
			d.ConsumedFileIDs = slices.Clone(s.draftSend.FileIDs)
			if d.Notice == "" {
				d.Notice = "消息已发送，但本机草稿或附件清理尚未保存；请重试清理。"
			}
		} else if s.draftSend.Outcome == "rejected" {
			d.RejectedCleanupPending = true
			if d.Notice == "" {
				d.Notice = "发送已拒绝，但本机状态未能保存；请重试读取。"
			}
		} else {
			d.PendingSend = true
			if d.Notice == "" {
				d.Notice = "上一条附件消息的原发送结果待核对；请勿用新请求重发。"
			}
		}
	}
	d.ReferenceIDs = slices.Clone(d.ReferenceIDs)
	return d
}
func draftReceipt(v api.Snapshot, id string) api.Receipt {
	for _, item := range v.Items {
		if item.Kind == "user" && item.RequestID == id {
			return api.Receipt{ID: id, Outcome: "accepted"}
		}
	}
	if v.LastReceipt.ID == id {
		return v.LastReceipt
	}
	return api.Receipt{}
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
	if err := s.persistDraft(d, s.draftSend); err != nil {
		return s.draft, err
	}
	if s.draftSend == nil {
		s.draftNotice = ""
	}
	s.draft = d
	return d, nil
}
func (s *Service) clearDraft(input api.Submission) {
	s.mu.Lock()
	revision := s.draft.Revision
	s.mu.Unlock()
	s.clearDraftAtRevision(input, revision)
}
func (s *Service) clearDraftAtRevision(input api.Submission, revision uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft.Revision == revision && s.draft.Text == input.Text && slices.Equal(s.draft.ReferenceIDs, input.ReferenceIDs) {
		d := api.Draft{Revision: s.draft.Revision + 1, ReferenceIDs: []string{}}
		if s.draftLoadError != nil || s.persistDraft(d, s.draftSend) != nil {
			s.draftNotice = "消息已发送，但草稿清理未能保存；请勿重复发送。"
			return
		}
		s.draft, s.draftNotice = d, ""
	}
}

type draftDocument struct {
	Version int        `json:"version"`
	Draft   api.Draft  `json:"draft"`
	Send    *draftSend `json:"send,omitempty"`
}

// Stored before native dispatch. Until the same request's terminal receipt is
// reconciled, these file IDs cannot be attached to a replacement send.
type draftSend struct {
	ID           string   `json:"id"`
	FileIDs      []string `json:"fileIds"`
	Text         string   `json:"text"`
	ReferenceIDs []string `json:"referenceIds"`
	Revision     uint64   `json:"revision"`
	Outcome      string   `json:"outcome,omitempty"`
}

func (s *Service) persistDraft(d api.Draft, send *draftSend) error {
	d.Notice, d.CleanupPending, d.RejectedCleanupPending, d.PendingSend, d.ConsumedFileIDs = "", false, false, false, nil
	if err := localstate.Write(s.draftFile, draftDocument{Version: 2, Draft: d, Send: send}); err != nil {
		return errors.New("草稿暂未保存，请保留当前内容后重试")
	}
	return nil
}
func (s *Service) reserveDraftSend(input api.Submission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.ID == "" {
		return errors.New("附件消息缺少原请求标识，消息未发送")
	}
	if s.draftLoadError != nil {
		return s.draftLoadError
	}
	if s.draftFile == "" {
		return errors.New("本机草稿存储尚未准备好，消息未发送")
	}
	if s.draftSend != nil {
		return errors.New("上一条附件消息仍待核对或清理")
	}
	send := &draftSend{ID: input.ID, FileIDs: slices.Clone(input.FileIDs), Text: input.Text, ReferenceIDs: slices.Clone(input.ReferenceIDs), Revision: s.draft.Revision}
	if err := s.persistDraft(s.draft, send); err != nil {
		return err
	}
	s.draftSend = send
	return nil
}

func (s *Service) reconcileDraftReceipt(receipt api.Receipt) {
	if receipt.ID == "" || receipt.Outcome == "" || receipt.Outcome == "unknown" {
		return
	}
	s.draftReconcileMu.Lock()
	defer s.draftReconcileMu.Unlock()
	s.mu.Lock()
	send := s.draftSend
	if send == nil || send.ID != receipt.ID {
		s.mu.Unlock()
		return
	}
	if receipt.Outcome == "accepted" {
		send.Outcome = "accepted"
		if s.draft.Revision == send.Revision && s.draft.Text == send.Text && slices.Equal(s.draft.ReferenceIDs, send.ReferenceIDs) {
			s.draft = api.Draft{Revision: s.draft.Revision + 1, ReferenceIDs: []string{}}
		}
		// Keep a durable accepted marker until both stores have been cleaned.
		if err := s.persistDraft(s.draft, send); err != nil {
			s.draftNotice = "消息已发送，但本机清理状态尚未保存；请重试清理。"
		}
	} else if receipt.Outcome != "rejected" {
		s.mu.Unlock()
		return
	} else {
		send.Outcome = "rejected"
	}
	s.mu.Unlock()
	if receipt.Outcome == "accepted" {
		err := errors.New("attachment consumer unavailable")
		if s.consumeFiles != nil {
			err = s.consumeFiles(send.FileIDs)
		}
		if err != nil {
			s.mu.Lock()
			s.draftNotice = "消息已发送，但附件选择清理未能保存；请重试清理。"
			s.mu.Unlock()
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draftSend != send {
		return
	}
	if err := s.persistDraft(s.draft, nil); err != nil {
		if receipt.Outcome == "accepted" {
			s.draftNotice = "消息已发送，但草稿清理未能保存；请重试清理。"
		} else {
			s.draftNotice = "发送已拒绝，但本机状态未能保存；请重试读取。"
		}
		return
	}
	s.draftSend, s.draftNotice = nil, ""
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
	if err != nil || len(b) > 1024*1024 || json.Unmarshal(b, &stored) != nil || (stored.Version != 1 && stored.Version != 2) || len(stored.Draft.Text) > 256*1024 || len(stored.Draft.ReferenceIDs) > 64 || (stored.Send != nil && (stored.Send.ID == "" || len(stored.Send.FileIDs) == 0 || len(stored.Send.FileIDs) > 8 || (stored.Send.Outcome != "" && stored.Send.Outcome != "accepted" && stored.Send.Outcome != "rejected"))) {
		s.draftLoadError = errors.New("本机草稿无法读取，原文件已保留；请检查 draft.json 后重新启动")
		s.draftNotice = s.draftLoadError.Error()
		return s.draftLoadError
	}
	s.draft = stored.Draft
	s.draft.Notice = ""
	s.draftSend = stored.Send
	if send := s.draftSend; send != nil && send.Outcome == "accepted" && s.draft.Revision == send.Revision && s.draft.Text == send.Text && slices.Equal(s.draft.ReferenceIDs, send.ReferenceIDs) {
		s.draft = api.Draft{Revision: s.draft.Revision + 1, ReferenceIDs: []string{}}
	}
	return nil
}
func previewKey(v api.Snapshot) string {
	// A preview acknowledges the last visible result. An outgoing user item
	// inserted before an already visible assistant result must not change that
	// result's identity. A newer user item or changed assistant result must.
	var result string
	for _, i := range v.Items {
		if i.Kind == "user" {
			result = "user\x00" + i.ID + "\x00" + i.RequestID + "\x00" + i.Text
		}
		if i.Kind == "assistant" {
			result = "assistant\x00" + i.ID + "\x00" + i.Text
		}
	}
	if result == "" {
		return ""
	}
	h := sha256.Sum256([]byte(result))
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
	// Validate the exact product projection supplied to the bubble. Local
	// outbox items and RecentSource are part of that projection, whereas the
	// engine's raw Snapshot is not what the user acknowledged.
	v := s.PetSnapshot()
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
		if e := consumer.AcknowledgePresentation(context.Background(), s.engine.Snapshot()); e != nil {
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
