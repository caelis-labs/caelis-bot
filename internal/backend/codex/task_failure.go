package codex

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type workerTerminal struct {
	Status  string
	Failure *api.TaskFailure
}

// Only App Server's typed enum is evidence. Never use capacityErrorInfo's
// message fallback, additionalDetails, or the diagnostic log classifier here.
func workerFailure(thread, turn string, err *turnError) *api.TaskFailure {
	f := api.UnknownTaskFailure("codex", opaque(thread, turn))
	if err == nil {
		return f
	}
	var code string
	structured := false
	if json.Unmarshal(err.Info, &code) != nil {
		var variants map[string]json.RawMessage
		if json.Unmarshal(err.Info, &variants) != nil || len(variants) != 1 {
			return f
		}
		for k, payload := range variants {
			switch k {
			case "httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts":
				var fields map[string]json.RawMessage
				if json.Unmarshal(payload, &fields) != nil || fields == nil {
					return f
				}
				if status, ok := fields["httpStatusCode"]; ok && string(status) != "null" {
					var number uint16
					if json.Unmarshal(status, &number) != nil {
						return f
					}
				}
				code = k
				structured = true
			default:
				return f
			}
		}
	}
	reason := ""
	switch code {
	case "serverOverloaded":
		reason = "The selected model is at capacity."
	case "rateLimitExceeded":
		reason = "The native model request reached a rate limit."
	case "usageLimitExceeded":
		reason = "The native usage limit was reached."
	case "sessionBudgetExceeded":
		reason = "The native session budget was reached."
	case "contextWindowExceeded":
		reason = "The native context window limit was reached."
	case "unauthorized":
		reason = "The native model request was not authorized."
	case "badRequest":
		reason = "The native model request was invalid."
	case "internalServerError":
		reason = "The native service reported an internal error."
	case "httpConnectionFailed", "responseStreamConnectionFailed":
		if !structured {
			return f
		}
		reason = "The native model connection failed."
	case "responseStreamDisconnected":
		if !structured {
			return f
		}
		reason = "The native response stream disconnected."
	case "responseTooManyFailedAttempts":
		if !structured {
			return f
		}
		reason = "The native response retry limit was reached."
		exhausted := true
		f.RetryExhausted = &exhausted
	case "sandboxError":
		reason = "The native sandbox reported an error."
	case "cyberPolicy", "misalignmentPolicyViolation", "tooManyDenials":
		reason = "The native policy did not permit the request."
	case "flexUnavailable":
		reason = "The native flex service was unavailable."
	}
	if reason != "" {
		f.Code, f.Reason = code, reason
	}
	return f
}

func (s *Session) workerStopAcknowledged(t *taskRecord) bool {
	key := s.workExecutionKeyLocked(t)
	for _, receipt := range t.Requests {
		if receipt.Kind != "stop" {
			continue
		}
		if receipt.Outcome == "accepted" && receipt.ExecutionKey == key {
			return true
		}
		if receipt.ControlTarget != "" && receipt.ControlTarget == t.StopPendingRequest && t.StopPendingTurn == t.Run && t.StopPendingPhase == "acknowledged" {
			return true
		}
	}
	return false
}
