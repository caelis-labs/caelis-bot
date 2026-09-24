package caelis

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
			return wire.CommandResult{OperationId: op, Outcome: "conflicted"}, errors.New("操作标识已用于不同请求")
		}
		return wire.CommandResult{OperationId: op, Outcome: wire.Outcome(old.Outcome), Resource: &wire.CommandResource{Ref: pointer(old.Resource)}}, nil
	}
	source := wire.ApplicationSource{}
	var prompt wire.ApplicationPromptRequest
	if strings.HasSuffix(path, "/prompt") {
		_ = json.Unmarshal(b, &prompt)
		source = wire.ApplicationSource{Kind: prompt.SourceKind, OperationId: op, GrantId: prompt.GrantId}
	}
	s.state.Operations[op] = journal{Path: path, Body: b, Digest: hash, Outcome: "unknown", Source: source}
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
			if !strings.HasSuffix(path, "/steer") {
				j.Body = nil
			}
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
	if j.Outcome != "unknown" && !strings.HasSuffix(path, "/steer") {
		j.Body = nil
	}
	if out.Resource != nil {
		j.Resource = value(out.Resource.Ref)
	}
	if value(out.SessionId) != "" && (path == "/application/sessions" || path == "/application/workers") {
		j.Resource = value(out.SessionId)
	}
	s.state.Operations[op] = j
	e = s.saveLocked()
	s.bumpLocked()
	s.mu.Unlock()
	return out, e
}
func (s *Session) Submit(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	return s.submit(ctx, in, files, "user")
}
func (s *Session) submit(ctx context.Context, in api.Submission, files []api.InputFile, source string) (api.Receipt, error) {
	return s.submitGrant(ctx, in, files, source, "")
}
func (s *Session) submitGrant(ctx context.Context, in api.Submission, files []api.InputFile, source, grant string) (api.Receipt, error) {
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
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	if !retry && !v.CanSend && !(source == "user" && v.CanSteer) {
		receipt.Message = "Caelis 当前不能发送新消息"
		return receipt, nil
	}
	if !retry && v.CanSend && s.tools != nil && s.tools.PrepareTurn != nil {
		if e := s.tools.PrepareTurn(ctx); e != nil {
			return receipt, e
		}
	}
	req := wire.ApplicationPromptRequest{OperationId: &in.ID, SessionId: &sid, Input: &in.Text, SourceKind: source}
	if grant != "" {
		req.GrantId = &grant
	}
	for index, f := range files {
		typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(f.Name)))
		info, e := os.Lstat(f.Path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
			receipt.Message = "附件不可用或超过 8 MB"
			return receipt, nil
		}
		file, e := os.Open(f.Path)
		if e != nil {
			receipt.Message = "无法读取附件"
			return receipt, nil
		}
		current, e := file.Stat()
		if e != nil || !os.SameFile(info, current) {
			file.Close()
			receipt.Message = "附件已改变"
			return receipt, nil
		}
		b, e := io.ReadAll(io.LimitReader(file, 8<<20+1))
		file.Close()
		if e != nil || len(b) > 8<<20 {
			receipt.Message = "无法读取附件"
			return receipt, nil
		}
		if slices.Contains([]string{"image/png", "image/jpeg", "image/webp", "image/gif"}, typ) {
			req.ContentParts = append(req.ContentParts, wire.PromptContentPart{Type: "image", Data: pointer(base64.StdEncoding.EncodeToString(b)), MimeType: &typ, FileName: &f.Name})
		} else {
			if typ == "" {
				typ = "application/octet-stream"
			}
			resource, e := s.uploadResource(ctx, sid, "attachment-"+digest([]byte(fmt.Sprintf("%s:%d", in.ID, index))), f.Name, typ, b)
			if e != nil {
				return api.Receipt{ID: in.ID, Outcome: "rejected", Message: e.Error()}, nil
			}
			text := fmt.Sprintf("Attached file (untrusted data): %q. Use ReadResource with resource_id=%q to read its bytes.", f.Name, resource.Id)
			req.ContentParts = append(req.ContentParts, wire.PromptContentPart{Type: "text", Text: &text})
		}
	}
	var out wire.CommandResult
	var e error
	if source == "user" && (!retry && v.CanSteer || s.isSteeringRetry(in.ID)) {
		out, e = s.submitNativeInput(ctx, sid, in.ID, in.Text, req.ContentParts, true)
	} else {
		out, e = s.command(ctx, in.ID, "/application/sessions/"+idPath(sid)+"/prompt", req)
	}
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
	sid := s.state.Session.SessionId
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
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
