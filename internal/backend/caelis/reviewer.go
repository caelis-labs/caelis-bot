package caelis

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Configure newly created profiles only. Legacy Bot profiles had no user-selectable
// reviewer: manual was hardcoded. A normal Dream handoff adopts the new product
// default without mutating the old Session, replaying work, or relaxing its sandbox.
func (s *Session) configureReviewer(profile *wire.ApplicationProfile) {
	if s.requireApproval || profile.Reviewer != nil {
		return
	}
	model := s.reviewerModel
	if model == "" {
		model = profile.Model
	}
	permissions := wire.ApplicationPermissions{}
	if profile.Permissions != nil {
		permissions = *profile.Permissions
	}
	if value(permissions.Mode) == "" {
		permissions.Mode = pointer("workspace-write")
	}
	permissions.ApprovalMode = pointer("auto-review")
	profile.Permissions = &permissions
	profile.Reviewer = &wire.ApplicationReviewer{Kind: "guardian", Model: model}
}

func (s *Session) checkReviewer(ctx context.Context, b wire.ApplicationBinding) error {
	var state wire.ApplicationReviewerState
	if err := s.client.json(ctx, "GET", "/application/sessions/"+idPath(b.SessionId)+"/reviewer-state", nil, &state, "", ""); err != nil {
		return err
	}
	mode := "manual"
	if b.Profile.Permissions != nil && value(b.Profile.Permissions.ApprovalMode) != "" {
		mode = *b.Profile.Permissions.ApprovalMode
	}
	if state.SessionId != b.SessionId || state.ApprovalMode != mode {
		return errors.New("Caelis 审查配置与当前对话不匹配")
	}
	switch mode {
	case "manual":
		if state.Status == "manual" && state.Reviewer == nil {
			return nil
		}
	case "auto-review":
		if b.Profile.Reviewer == nil || state.Reviewer == nil || *state.Reviewer != *b.Profile.Reviewer {
			return errors.New("Caelis Guardian 配置与当前对话不匹配")
		}
		if state.Status == "ready" {
			return nil
		}
		if state.Status == "unavailable" {
			return errors.New("Caelis Guardian 模型不可用，请在运行时连接设置中恢复该模型；不会转为免审批或人工放行")
		}
	}
	return errors.New("Caelis 审查状态无法确认，请更新并检查运行时")
}

// Automatic review facts cannot mint a manual decision target. The immutable
// Application profile also fences the interval before the first progress event.
func (s *Session) automaticApprovalLocked(sid string, a *wire.ActiveApproval) bool {
	p := s.state.Session.Profile
	if sid == s.state.Session.SessionId && p.Permissions != nil && value(p.Permissions.ApprovalMode) == "auto-review" {
		return true
	}
	v := s.state.Views[sid]
	if v == nil || a == nil || a.Target == nil {
		return false
	}
	id := reviewID(sid, a.Target.TurnId, a.RequestId)
	_, durable := v.Reviews[id]
	_, live := v.LiveReviews[id]
	return durable || live
}

func reviewID(sid, turn, approval string) string {
	return "review-" + digest([]byte(sid+"\x00"+turn+"\x00"+approval))
}

type reviewFact struct {
	api.Review
	TurnID     string `json:"turnID"`
	ApprovalID string `json:"approvalID"`
	ItemID     string `json:"itemID,omitempty"`
	ToolCallID string `json:"toolCallID"`
}

func applyReview(v *view, e wire.Envelope) {
	r := e.ApprovalReview
	if r == nil || value(e.SessionId) == "" || value(e.TurnId) == "" || value(e.ApprovalRequestId) == "" || value(r.ToolCallId) == "" {
		return
	}
	if v.State.SessionId != "" && value(e.SessionId) != v.State.SessionId {
		return
	}
	id := reviewID(value(e.SessionId), value(e.TurnId), value(e.ApprovalRequestId))
	// Replayed decisions and delayed progress share one identity. A decided review
	// is immutable; progress must never resurrect it or affect turn lifecycle.
	if _, decided := v.Reviews[id]; decided {
		return
	}
	status := value(r.Status)
	switch status {
	case "in_progress":
		status = "inProgress"
	case "timed_out":
		status = "timedOut"
	case "approved", "denied", "failed":
	default:
		return
	}
	action, _ := json.MarshalIndent(struct {
		Tool      string          `json:"tool"`
		Arguments wire.JSONObject `json:"arguments"`
	}{value(r.ToolName), r.RawInput}, "", "  ")
	if prior, ok := v.LiveReviews[id]; ok && prior.Status != "inProgress" && status == "inProgress" {
		return
	}
	fact := reviewFact{Review: api.Review{ID: id, Status: status, Action: string(action), Rationale: value(r.Text)}, TurnID: value(e.TurnId), ApprovalID: value(e.ApprovalRequestId), ItemID: value(r.ItemId), ToolCallID: value(r.ToolCallId)}
	if (status == "approved" || status == "denied") && e.Delivery.Mode == wire.DeliveryModeMirror {
		if v.Reviews == nil {
			v.Reviews = map[string]reviewFact{}
		}
		v.Reviews[id] = fact
		delete(v.LiveReviews, id)
	} else {
		if v.LiveReviews == nil {
			v.LiveReviews = map[string]reviewFact{}
		}
		v.LiveReviews[id] = fact
	}
}
