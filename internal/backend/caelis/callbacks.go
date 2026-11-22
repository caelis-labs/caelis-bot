package caelis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) ensureCallObserverLocked(sid string) {
	if sid == "" || s.retainedWorkers || s.callObservers[sid] || sid != s.state.Session.SessionId && !slices.Contains(s.state.PastSessions, sid) {
		return
	}
	if s.callObservers == nil {
		s.callObservers = map[string]bool{}
	}
	s.callObservers[sid] = true
	binding := wire.SessionBinding{SessionId: sid, PrincipalId: s.state.PrincipalID}
	s.wg.Add(1)
	go s.callLoop(s.streamCtx, s.client, binding, s.generation)
}

func (s *Session) callLoop(ctx context.Context, c *client, binding wire.SessionBinding, generation uint64) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		if s.generation == generation {
			delete(s.callObservers, binding.SessionId)
		}
		s.mu.Unlock()
	}()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	recovered := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		current := s.state.Session.SessionId == binding.SessionId
		v := s.state.Views[binding.SessionId]
		settled := !current && v != nil && v.CommandCaughtUp && !value(v.State.Run.Active) && v.State.Approval.Active == nil && value(v.State.Run.Status) != "unknown"
		ready := s.connected && !s.closed && s.client == c && s.generation == generation
		s.mu.Unlock()
		if settled {
			return
		}
		if !ready {
			recovered = false
			continue
		}
		var e error
		if !recovered {
			// Recovery includes claimed calls; waiting only returns new pending
			// calls and must never replace this uncertain-effect reconciliation.
			e = s.pollCalls(ctx, c, binding, binding.PrincipalId)
			recovered = e == nil
		} else {
			var calls []wire.SessionToolCall
			calls, e = waitCalls(ctx, c, binding.SessionId)
			s.mu.Lock()
			ready = s.connected && !s.closed && s.client == c && s.generation == generation
			s.mu.Unlock()
			if !ready {
				recovered = false
				continue
			}
			if e == nil {
				for _, call := range calls {
					if call.State != "pending" {
						e = errors.New("Caelis 等待调用返回无效状态")
						break
					}
					if e = s.handleCall(ctx, c, binding, binding.PrincipalId, call); e != nil {
						break
					}
				}
			}
		}
		if e != nil && ctx.Err() == nil {
			recovered = false
			s.componentError("application_calls", binding.SessionId, e)
		}
	}
}

func waitCalls(ctx context.Context, c *client, sid string) ([]wire.SessionToolCall, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	observation, closeTransport := c.observationClient()
	defer closeTransport()
	var calls []wire.SessionToolCall
	err := observation.jsonTimeout(ctx, "GET", "/sessions/"+idPath(sid)+"/tool-calls?wait=true", nil, &calls, "", "", time.Minute)
	// An idle read wait or replacement of its owning stream has no uncertain
	// effect to reconcile and is not a disconnection. Other failures still are.
	if err != nil && ctx.Err() != nil {
		return nil, nil
	}
	return calls, err
}
func (s *Session) pollCalls(ctx context.Context, c *client, b wire.SessionBinding, principal string) error {
	var calls []wire.SessionToolCall
	if e := c.json(ctx, "GET", "/sessions/"+idPath(b.SessionId)+"/tool-calls", nil, &calls, "", ""); e != nil {
		return e
	}
	for _, call := range calls {
		if call.State != "pending" && call.State != "claimed" {
			continue
		}
		if e := s.handleCall(ctx, c, b, principal, call); e != nil {
			return e
		}
	}
	return nil
}
func (s *Session) handleCall(ctx context.Context, c *client, b wire.SessionBinding, principal string, call wire.SessionToolCall) error {
	// Only the authenticated opaque ID is an effect identity. Arguments and the
	// provider's reusable call_id are never trusted invocation context.
	s.mu.Lock()
	host := s.catalogs[call.ToolsVersion][call.Name]
	contentV1 := s.state.ContentCatalogs[call.ToolsVersion][call.Name]
	s.mu.Unlock()
	if call.Id == "" || call.TurnId == "" || call.ItemId == "" || call.SessionId != b.SessionId || call.PrincipalId != principal || b.PrincipalId != principal {
		return errors.New("Caelis 工具调用绑定不匹配")
	}
	if call.Source.Kind != "user" && call.Source.Kind != "application_summary" && call.Source.Kind != "external_material" && call.Source.Kind != "authorized_background" {
		return errors.New("Caelis 工具调用来源无效")
	}
	// This client admits calls only from its own durably recorded prompt. Model
	// arguments cannot create authority or select another source operation.
	s.mu.Lock()
	op, known := s.state.Operations[call.Source.OperationId]
	record, exists := s.state.Calls[call.Id]
	s.mu.Unlock()
	if !known || op.Path != "/sessions/"+idPath(b.SessionId)+"/prompt" || op.Source.Kind != call.Source.Kind || value(op.Source.GrantId) != value(call.Source.GrantId) {
		return errors.New("Caelis 工具来源不属于当前应用提交")
	}
	if exists && !sameInvocation(record.Call, call) {
		return errors.New("Caelis 工具调用内容发生改变")
	}
	if exists && record.Phase == "completed" {
		return nil
	}
	path := "/sessions/" + idPath(b.SessionId) + "/tool-calls/" + idPath(call.Id)
	if !exists {
		record = callRecord{Call: call, Phase: "claiming"}
		if e := s.saveCall(call.Id, record); e != nil {
			return e
		}
		if call.State == "pending" {
			var claimed wire.SessionToolCall
			e := c.json(ctx, "POST", path+"/claim", struct{}{}, &claimed, "", "")
			if e == nil && claimed.State == "claimed" && sameInvocation(claimed, call) {
				record.Phase = "executing"
				if e = s.saveCall(call.Id, record); e != nil {
					return e
				}
				args, _ := json.Marshal(call.Arguments)
				result := api.ToolResult{IsError: true, Content: []map[string]string{{"type": "text", "text": "The original application tool version is unavailable; no effect was performed."}}}
				if call.Name == "bot_care" || call.Name == "bot_care_read" {
					result.Content = []map[string]string{{"type": "text", "text": "This tool is retired; use schedule. No effect was performed."}}
				} else if host != nil {
					invocation := api.ToolInvocation{Provider: "caelis", CallID: call.Id, Session: call.SessionId, Turn: call.TurnId, TurnKey: call.TurnId}
					callCtx := api.WithToolInvocation(context.WithValue(ctx, invocationKey{}, call), invocation)
					s.mu.Lock()
					resident := s.state.Session.SessionId == call.SessionId
					s.mu.Unlock()
					if resident {
						result = host.CallTool(callCtx, call.Name, args)
					} else {
						result.Content = []map[string]string{{"type": "text", "text": "This conversation has handed off. Finish the current turn without starting more work."}}
					}
				}
				if result.CheckTurnComplete != nil && result.CheckTurnComplete() != nil {
					result = api.ToolResult{IsError: true, Content: []map[string]string{{"type": "text", "text": "User activity superseded this Dream; continue in the current conversation."}}}
				}
				blocks := result.Content
				if contentV1 {
					blocks = contentV1Blocks(result)
				}
				content, _ := json.Marshal(blocks)
				outcome := "succeeded"
				if result.IsError {
					outcome = "failed"
				}
				record.Receipt = &wire.SessionToolCallResult{Outcome: outcome, Content: json.RawMessage(content)}
				if contentV1 {
					record.Receipt.ResultFormat = pointer("content-v1")
					record.Receipt.StructuredContent = result.StructuredContent
				}
			}
			// Lost claim response never grants an effect, even if a later read says
			// claimed. A crash after executing is equally uncertain, never replayed.
		}
	}
	if record.Receipt == nil {
		record.Receipt = &wire.SessionToolCallResult{Outcome: "unknown", Content: json.RawMessage(`[{"type":"text","text":"Tool outcome is uncertain; do not automatically repeat the effect."}]`)}
		if contentV1 {
			record.Receipt.ResultFormat = pointer("content-v1")
		}
	}
	record.Phase = "receipt"
	if e := s.saveCall(call.Id, record); e != nil {
		return e
	}
	// Result writes are safe to retry with identical bytes; no business callback
	// runs on this path. Terminal calls disappear from the pending/claimed list.
	if e := c.json(ctx, "POST", path+"/result", record.Receipt, nil, "", ""); e != nil {
		if isRemoteStatus(e, 409) || isRemoteStatus(e, 410) {
			return nil
		}
		return e
	}
	record.Phase = "completed"
	return s.saveCall(call.Id, record)
}

// Caelis content-v1 projects structuredContent as a provider JSON part. MCP's
// identical text fallback must not become a second copy of the same desktop
// state in every model request. Keep prose, media and nonidentical JSON intact.
func contentV1Blocks(result api.ToolResult) []map[string]string {
	if len(result.StructuredContent) == 0 {
		return result.Content
	}
	structured, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return result.Content
	}
	blocks := make([]map[string]string, 0, len(result.Content))
	for _, block := range result.Content {
		duplicate := false
		if block["type"] == "text" && json.Valid([]byte(block["text"])) {
			var value any
			decoder := json.NewDecoder(bytes.NewBufferString(block["text"]))
			decoder.UseNumber()
			if decoder.Decode(&value) == nil {
				canonical, err := json.Marshal(value)
				duplicate = err == nil && bytes.Equal(canonical, structured)
			}
		}
		if !duplicate {
			blocks = append(blocks, block)
		}
	}
	if len(blocks) == 0 {
		return []map[string]string{{"type": "text", "text": "See the structured tool result."}}
	}
	return blocks
}
func sameInvocation(a, b wire.SessionToolCall) bool {
	a.State = ""
	b.State = ""
	a.Result = nil
	b.Result = nil
	return reflect.DeepEqual(a, b)
}
func (s *Session) saveCall(id string, r callRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Calls[id] = r
	return s.saveLocked()
}

// A saved result is not acknowledgement. If the response was lost, read the
// original callback before authorizing a new resident binding.
func (s *Session) confirmHandoff(ctx context.Context, c *client, id, source, turn string) error {
	s.mu.Lock()
	record := s.state.Calls[id]
	s.mu.Unlock()
	if !record.handoffFrom(id, source, turn) {
		if record.Call.Id == "" || record.Receipt != nil {
			return api.ErrConversationRenewalRejected
		}
	}
	if record.Phase == "completed" && record.Receipt != nil {
		return nil
	}
	var readback wire.SessionToolCall
	if err := c.json(ctx, "GET", "/sessions/"+idPath(source)+"/tool-calls/"+idPath(id), nil, &readback, "", ""); err != nil {
		if isRemoteStatus(err, 404) || isRemoteStatus(err, 410) {
			return api.ErrConversationRenewalRejected
		}
		return err
	}
	if !sameInvocation(record.Call, readback) || slices.Contains([]string{"cancelled", "unknown"}, readback.State) {
		return api.ErrConversationRenewalRejected
	}
	if readback.State != "completed" {
		return errors.New("original handoff callback is still pending")
	}
	if !record.handoffFrom(id, source, turn) {
		return api.ErrConversationRenewalRejected
	}
	want, _ := json.Marshal(record.Receipt)
	got, _ := json.Marshal(readback.Result)
	if !bytes.Equal(want, got) {
		return api.ErrConversationRenewalRejected
	}
	record.Phase = "completed"
	return s.saveCall(id, record)
}
