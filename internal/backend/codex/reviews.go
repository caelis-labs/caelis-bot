package codex

import (
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// The native reviewer owns its decision. Only a separate server request may
// offer user approval; a denied/timed-out review never creates an Allow button.
func (s *Session) applyReview(event Notification) {
	var n struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ID       string `json:"reviewId"`
		Review   struct {
			Status    string `json:"status"`
			Rationale string `json:"rationale"`
		} `json:"review"`
		Action struct {
			Type     string   `json:"type"`
			Command  string   `json:"command"`
			Program  string   `json:"program"`
			Argv     []string `json:"argv"`
			Files    []string `json:"files"`
			Target   string   `json:"target"`
			ToolName string   `json:"toolName"`
			Server   string   `json:"server"`
			Reason   string   `json:"reason"`
		} `json:"action"`
	}
	if !s.decodeEvent(event, &n, false) || n.ID == "" || n.TurnID == "" || !s.ownsThread(n.ThreadID) {
		return
	}
	if n.Review.Status == "inProgress" && terminal(s.runs[n.TurnID]) {
		return
	}
	id := opaque(n.ThreadID, n.TurnID, n.ID)
	action := n.Action.Command
	switch n.Action.Type {
	case "execve":
		action = n.Action.Program + " " + strings.Join(n.Action.Argv, " ")
	case "applyPatch":
		action = "修改文件：" + strings.Join(n.Action.Files, "、")
	case "networkAccess":
		action = "访问网络：" + n.Action.Target
	case "mcpToolCall":
		action = n.Action.Server + " / " + n.Action.ToolName
	case "requestPermissions":
		action = n.Action.Reason
	case "writeStdin":
		action = "向运行中的命令输入内容"
	}
	review := api.Review{ID: id, Status: n.Review.Status, Action: action, Rationale: n.Review.Rationale}
	for i, old := range s.state.Reviews {
		if old.ID == id {
			if old.Status != "inProgress" && review.Status == "inProgress" {
				return
			}
			s.state.Reviews[i] = review
			return
		}
	}
	s.state.Reviews = append(s.state.Reviews, review)
}
