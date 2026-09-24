package codex

import (
	"encoding/json"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func (s *Session) logEvent(event Notification, code, reason string) {
	var target struct {
		Thread       string `json:"threadId"`
		Turn         string `json:"turnId"`
		Item         string `json:"itemId"`
		Name         string `json:"name"`
		NativeThread struct {
			ID string `json:"id"`
		} `json:"thread"`
		NativeTurn struct {
			ID string `json:"id"`
		} `json:"turn"`
		NativeItem struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	// Invalid target fields do not prevent logging the method and fingerprint.
	_ = json.Unmarshal(event.Params, &target)
	if target.Thread == "" {
		target.Thread = target.NativeThread.ID
	}
	if target.Turn == "" {
		target.Turn = target.NativeTurn.ID
	}
	if target.Item == "" {
		target.Item = target.NativeItem.ID
	}
	s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: code,
		Method: event.Method, Thread: target.Thread, Turn: target.Turn, Item: target.Item, Server: target.Name,
		Sequence: event.Sequence, Reason: reason, Fingerprint: diagnosticlog.Fingerprint(event.Params), Bytes: len(event.Params)})
}

func (s *Session) decodeEvent(event Notification, out any, blocking bool) bool {
	if err := json.Unmarshal(event.Params, out); err != nil {
		s.logEvent(event, "event_decode_failed", diagnosticlog.DecodeReason(err))
		if blocking {
			// Only an owned lifecycle/decision failure makes execution uncertain.
			s.state.Phase = "unknown"
			s.state.Message = "无法确认当前工作的状态，请重新连接核对"
		}
		return false
	}
	return true
}

func (s *Session) componentEvent(event Notification) {
	var n struct {
		Status  string `json:"status"`
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if !s.decodeEvent(event, &n, false) {
		return
	}
	if n.Error != "" || n.Status == "failed" || n.Success != nil && !*n.Success {
		s.logEvent(event, "component_failed", diagnosticlog.Reason(n.Error))
	}
}
