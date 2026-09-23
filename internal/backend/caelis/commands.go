package caelis

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// command persists exact bytes before dispatch. Unknown journals are read only;
// transport retries can never accidentally create a second native operation.
// Caller serializes mutations with step; stream projection only takes mu.
func (s *Session) command(ctx context.Context, op, path string, req any) (wire.CommandResult, error) {
	b, e := json.Marshal(req)
	if e != nil {
		return wire.CommandResult{}, e
	}
	hash := digest(b)
	s.mu.Lock()
	old, exists := s.state.Operations[op]
	if exists {
		s.mu.Unlock()
		oldHash := old.Digest
		if oldHash == "" {
			oldHash = digest(old.Body)
		}
		if old.Path != path || oldHash != hash {
			return wire.CommandResult{}, errors.New("操作标识已用于不同请求")
		}
		return wire.CommandResult{OperationId: op, Outcome: wire.Outcome(old.Outcome), Resource: &wire.CommandResource{Ref: pointer(old.Resource)}}, nil
	}
	s.state.Operations[op] = journal{Path: path, Body: b, Digest: hash, Outcome: "unknown"}
	e = s.saveLocked()
	s.bumpLocked()
	c := s.client
	s.mu.Unlock()
	if e != nil {
		return wire.CommandResult{}, e
	}
	if c == nil {
		return wire.CommandResult{}, errors.New("Caelis 尚未连接")
	}
	var out wire.CommandResult
	var headers struct {
		Revision string `json:"expected_revision"`
	}
	_ = json.Unmarshal(b, &headers)
	e = c.json(ctx, "POST", path, req, &out, op, headers.Revision)
	if e != nil {
		var remote *remoteError
		if errors.As(e, &remote) && (remote.Status == 400 || remote.Status == 401 || remote.Status == 403 || remote.Status == 404 || remote.Status == 409 || remote.Status == 413) {
			s.mu.Lock()
			j := s.state.Operations[op]
			j.Outcome = "rejected"
			j.Body = nil
			s.state.Operations[op] = j
			save := s.saveLocked()
			s.bumpLocked()
			s.mu.Unlock()
			if save != nil {
				return wire.CommandResult{OperationId: op, Outcome: "unknown"}, save
			}
			return wire.CommandResult{OperationId: op, Outcome: "rejected"}, e
		}
		return wire.CommandResult{OperationId: op, Outcome: "unknown"}, e
	}
	if out.OperationId != op || !slices.Contains([]wire.Outcome{"accepted", "committed", "rejected", "conflicted", "unknown"}, out.Outcome) {
		return wire.CommandResult{OperationId: op, Outcome: "unknown"}, errors.New("Caelis 操作回执不匹配")
	}
	s.mu.Lock()
	j := s.state.Operations[op]
	j.Outcome = string(out.Outcome)
	if j.Outcome != "unknown" {
		j.Body = nil
	}
	if out.Resource != nil {
		j.Resource = value(out.Resource.Ref)
	}
	s.state.Operations[op] = j
	e = s.saveLocked()
	s.bumpLocked()
	s.mu.Unlock()
	return out, e
}
func (s *Session) Submit(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	s.step.Lock()
	defer s.step.Unlock()
	receipt := api.Receipt{ID: in.ID, Outcome: "rejected"}
	if in.ID == "" || len(in.ID) > 128 || len(in.Text) > 256<<10 || len(in.ReferenceIDs) != 0 || len(files) > 4 {
		receipt.Message = "Caelis 不支持此消息或插件引用"
		return receipt, nil
	}
	s.mu.Lock()
	v := s.snapshotLocked()
	_, retry := s.state.Operations[in.ID]
	sid := s.state.Bot.SessionId
	s.mu.Unlock()
	if !retry && !v.CanSend {
		receipt.Message = "Caelis 当前不能发送新消息"
		return receipt, nil
	}
	req := wire.PromptRequest{OperationId: &in.ID, SessionId: &sid, Input: &in.Text}
	for _, f := range files {
		if !slices.Contains(s.info.Capabilities, "bot-image-input-v1") {
			receipt.Message = "当前 Caelis 不支持图片"
			return receipt, nil
		}
		typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(f.Name)))
		if !slices.Contains([]string{"image/png", "image/jpeg", "image/webp", "image/gif"}, typ) {
			receipt.Message = "Caelis 当前仅支持文本与图片附件"
			return receipt, nil
		}
		info, e := os.Lstat(f.Path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
			receipt.Message = "图片不可用或超过 8 MB"
			return receipt, nil
		}
		file, e := os.Open(f.Path)
		if e != nil {
			receipt.Message = "无法读取图片"
			return receipt, nil
		}
		current, e := file.Stat()
		if e != nil || !os.SameFile(info, current) {
			file.Close()
			receipt.Message = "图片已改变"
			return receipt, nil
		}
		b, e := io.ReadAll(io.LimitReader(file, 8<<20+1))
		file.Close()
		if e != nil || len(b) > 8<<20 {
			receipt.Message = "无法读取图片"
			return receipt, nil
		}
		req.ContentParts = append(req.ContentParts, wire.PromptContentPart{Type: "image", Data: pointer(base64.StdEncoding.EncodeToString(b)), MimeType: &typ, FileName: &f.Name})
	}
	out, e := s.command(ctx, in.ID, "/sessions/"+idPath(sid)+"/prompt", req)
	receipt.Outcome = productOutcome(out.Outcome)
	if receipt.Outcome == "" {
		receipt.Outcome = "unknown"
	}
	if e != nil {
		receipt.Message = e.Error()
	}
	s.mu.Lock()
	s.state.LastReceipt = receipt
	save := s.saveLocked()
	s.bumpLocked()
	s.mu.Unlock()
	if save != nil {
		return api.Receipt{ID: in.ID, Outcome: "unknown", Message: "发送回执未能保存，请重新连接核对"}, nil
	}
	return receipt, nil
}
func (s *Session) Interrupt(ctx context.Context) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	sid := s.state.Bot.SessionId
	v := s.state.Views[sid]
	if !s.connected || v == nil || !value(v.State.Run.Active) {
		s.mu.Unlock()
		return errors.New("当前没有可停止的主对话")
	}
	target := wire.TurnTarget{HandleId: value(v.State.Run.HandleId), RunId: value(v.State.Run.RunId), TurnId: value(v.State.Run.TurnId)}
	s.mu.Unlock()
	targetBytes, _ := json.Marshal(target)
	op := "cancel-" + digest(append([]byte(s.state.InstanceID+"\x00"+sid+"\x00"), targetBytes...))
	out, e := s.command(ctx, op, "/sessions/"+idPath(sid)+"/cancel", wire.CancelRequest{OperationId: &op, SessionId: &sid, Target: target})
	if e != nil {
		return e
	}
	if !succeeded(out.Outcome) {
		return errors.New("停止结果尚未确认")
	}
	return nil
}
func (s *Session) Decide(ctx context.Context, d api.Decision) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	ref, ok := s.approvalLocked(d.ID)
	c := s.client
	connected := s.connected
	s.mu.Unlock()
	if !ok || !connected {
		return errors.New("此审批已失效，请重新核对")
	}
	// Fetch the authoritative head immediately before resolving; never reuse a stale UI target.
	var head wire.SessionState
	if e := c.json(ctx, "GET", "/sessions/"+idPath(ref.sid)+"/state", nil, &head, "", ""); e != nil {
		return e
	}
	a := head.Approval.Active
	if a == nil || approvalID(s.state.InstanceID, ref.sid, a) != d.ID {
		return errors.New("审批目标已改变，请等待界面刷新")
	}
	raw, _ := json.Marshal(a.Permission)
	var permission nativePermission
	_ = json.Unmarshal(raw, &permission)
	var choice *nativeOption
	for _, o := range permission.Options {
		if o.ID == d.Choice {
			x := o
			choice = &x
		}
	}
	if choice == nil || a.Target == nil || len(d.Answers) > 0 {
		return errors.New("此审批选项不可用")
	}
	op := "approval-" + d.ID
	out, e := s.command(ctx, op, "/sessions/"+idPath(ref.sid)+"/approvals/"+idPath(a.RequestId)+"/resolve", wire.ResolveApprovalRequest{OperationId: &op, SessionId: &ref.sid, ApprovalRequestId: a.RequestId, Target: *a.Target, OptionId: &choice.ID, Outcome: "selected", Approved: strings.HasPrefix(choice.Kind, "allow")})
	if e != nil {
		return e
	}
	if !succeeded(out.Outcome) {
		return errors.New("审批决定尚未被接受")
	}
	return nil
}
func (s *Session) OwnedTasks(ctx context.Context) ([]api.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected && s.state.Bot.Id != "" {
		return nil, errors.New("Caelis 工作状态尚未对账")
	}
	out := make([]api.Task, 0, len(s.works))
	for _, w := range s.works {
		out = append(out, api.Task{ID: w.Id, Title: w.Assignment, Workspace: w.WorkspaceKey, Status: workStatus(w.Status), Result: value(w.Result)})
	}
	return out, nil
}
func (s *Session) AcknowledgePresentation(ctx context.Context, snap api.Snapshot) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	list := slices.Clone(s.completions)
	sid := s.state.Bot.SessionId
	bot := s.state.Bot.Id
	s.mu.Unlock()
	for _, n := range list {
		if n.Acknowledged || n.ReportState != "admitted" {
			continue
		}
		turn := n.ReportExecution.TurnId
		if turn == "" || snap.CurrentTurn != turn {
			continue
		}
		op := "ack-" + n.Id
		out, e := s.command(ctx, op, s.botPath("/work/acknowledge"), wire.BotWorkRequest{BotId: bot, SessionId: &sid, WorkId: &n.Id, OperationId: &op})
		if e != nil {
			return e
		}
		if !succeeded(out.Outcome) {
			return errors.New("完成确认尚未接受")
		}
	}
	return nil
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Work.status comes from the durable execution journal, whose success value is
// "succeeded"; it is distinct from the SSE lifecycle's "completed".
func workStatus(native string) string {
	switch native {
	case "succeeded":
		return "completed"
	case "started", "running":
		return "working"
	case "prepared", "reserved":
		return "pending"
	case "cancel_requested":
		return "interrupting"
	case "unknown_outcome":
		return "unknown"
	default:
		return native
	}
}
