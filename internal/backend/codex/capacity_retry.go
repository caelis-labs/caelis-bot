package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

const capacityWaitMessage = "模型当前繁忙，正在等待后重试；可以停止等待"
const nativeCapacityWaitMessage = "模型当前繁忙，Codex 正在重试"

// Only a direct native rejection of turn/start proves that a new Turn was not
// admitted. Once a Turn exists, Runtime owns its own retries and all effects.
func capacityNativeError(err error) bool {
	var native *NativeError
	if !errors.As(err, &native) {
		return false
	}
	capacity, exhausted := capacityErrorInfo(native.Message, native.Data)
	return capacity && !exhausted
}

func capacityReportedError(err error) bool {
	var native *NativeError
	if !errors.As(err, &native) {
		return false
	}
	capacity, _ := capacityErrorInfo(native.Message, native.Data)
	return capacity
}

func capacityErrorInfo(message string, data json.RawMessage) (capacity, runtimeExhausted bool) {
	var direct string
	if json.Unmarshal(data, &direct) == nil {
		capacity = direct == "serverOverloaded"
		runtimeExhausted = direct == "responseTooManyFailedAttempts"
	}
	var outer map[string]json.RawMessage
	if json.Unmarshal(data, &outer) == nil {
		info := outer["codexErrorInfo"]
		if len(info) == 0 {
			info = data
		}
		var code string
		if json.Unmarshal(info, &code) == nil {
			capacity = code == "serverOverloaded"
			runtimeExhausted = code == "responseTooManyFailedAttempts"
		} else {
			var variants map[string]json.RawMessage
			if json.Unmarshal(info, &variants) == nil {
				_, capacity = variants["serverOverloaded"]
				_, runtimeExhausted = variants["responseTooManyFailedAttempts"]
			}
		}
	}
	// This is a native error message, never model-generated conversation text.
	message = strings.ToLower(strings.TrimSpace(message))
	capacity = capacity || strings.HasPrefix(message, "selected model is at capacity.")
	return capacity, runtimeExhausted
}

func defaultCapacityBackoff(attempt int) time.Duration {
	base := time.Duration(750<<attempt) * time.Millisecond
	return time.Duration(float64(base) * (0.8 + 0.4*rand.Float64()))
}

// startWithCapacityRetry keeps the original clientUserMessageId and target.
// Native rejection is the only point at which another dispatch is safe. In
// particular, a lost response or a terminal Turn error is never retried here.
func (s *Session) startWithCapacityRetry(ctx context.Context, c *Client, method string, params, result any, thread, request string) error {
	if method != "turn/start" {
		return callDecode(ctx, c, method, params, result)
	}
	var retryCtx context.Context
	var cancel context.CancelFunc
	defer func() {
		if cancel == nil {
			return
		}
		s.mu.Lock()
		s.capacityCancel = nil
		s.capacityThread = ""
		s.capacityWaiting = false
		if s.state.Message == capacityWaitMessage {
			s.state.Message = ""
		}
		s.update()
		s.mu.Unlock()
		cancel()
	}()
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return &RequestError{Method: method, Cause: err}
		}
		callCtx := ctx
		if retryCtx != nil {
			callCtx = retryCtx
			if err := retryCtx.Err(); err != nil {
				return &RequestError{Method: method, Cause: err}
			}
		}
		err := callDecode(callCtx, c, method, params, result)
		if err == nil || !capacityNativeError(err) {
			return err
		}
		if attempt == 2 {
			return fmt.Errorf("模型仍然繁忙，自动重试已耗尽: %w", err)
		}
		s.mu.Lock()
		// An independently correlated native user message wins over a conflicting
		// rejection. Never issue a second Turn for an accepted request.
		accepted := s.state.LastReceipt.ID == request && s.state.LastReceipt.Outcome == "accepted"
		for _, task := range s.binding.Tasks {
			if task != nil && task.Thread == thread && task.Requests[request].Outcome == "accepted" {
				accepted = true
			}
		}
		if accepted {
			s.mu.Unlock()
			return err
		}
		if retryCtx == nil {
			retryCtx, cancel = context.WithCancel(ctx)
			s.capacityCancel, s.capacityThread = cancel, thread
		}
		s.capacityWaiting = true
		if thread == s.binding.ThreadID {
			s.state.Message = capacityWaitMessage
		} else if task := s.taskByThread(thread); task != nil {
			task.View.Status = "pending"
			task.View.Result = capacityWaitMessage
		}
		s.update()
		s.mu.Unlock()
		delay := defaultCapacityBackoff(attempt)
		if s.capacityBackoff != nil {
			delay = s.capacityBackoff(attempt)
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-retryCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return &RequestError{Method: method, Cause: retryCtx.Err()}
		}
		s.mu.Lock()
		s.capacityWaiting = false
		if s.state.Message == capacityWaitMessage {
			s.state.Message = ""
		}
		if task := s.taskByThread(thread); task != nil && task.View.Status == "pending" {
			task.View.Status = "unknown"
			task.View.Result = ""
		}
		s.update()
		s.mu.Unlock()
	}
	return fmt.Errorf("capacity retry invariant")
}

func (s *Session) cancelCapacityWait(thread string) {
	s.mu.Lock()
	if s.capacityCancel != nil && (thread == "" || thread == s.capacityThread) {
		s.capacityCancel()
	}
	s.mu.Unlock()
}
