package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) callLoop(ctx context.Context) {
	defer s.wg.Done()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		ready := s.connected && !s.closed
		c := s.client
		binding := s.state.Session
		life := s.state.Connection
		s.mu.Unlock()
		if !ready || binding.SessionId == "" {
			continue
		}
		if e := s.pollCalls(ctx, c, binding, life); e != nil && ctx.Err() == nil {
			s.fail(e)
		}
	}
}
func (s *Session) pollCalls(ctx context.Context, c *client, b wire.ApplicationBinding, life wire.ApplicationConnection) error {
	var calls []wire.ApplicationCall
	if e := c.json(ctx, "GET", "/application/sessions/"+idPath(b.SessionId)+"/calls", nil, &calls, "", ""); e != nil {
		return e
	}
	for _, call := range calls {
		if call.State != "pending" && call.State != "claimed" {
			continue
		}
		if e := s.handleCall(ctx, c, b, life, call); e != nil {
			return e
		}
	}
	return nil
}
func (s *Session) handleCall(ctx context.Context, c *client, b wire.ApplicationBinding, life wire.ApplicationConnection, call wire.ApplicationCall) error {
	// Only the authenticated opaque ID is an effect identity. Arguments and the
	// provider's reusable call_id are never trusted invocation context.
	s.mu.Lock()
	host := s.catalogs[call.ToolsVersion][call.Name]
	s.mu.Unlock()
	if call.Id == "" || call.TurnId == "" || call.ItemId == "" || call.SessionId != b.SessionId || call.ApplicationId != b.ApplicationId || call.ConnectionId != life.ConnectionId || call.PrincipalId != life.PrincipalId {
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
	if !known || op.Path != "/application/sessions/"+idPath(b.SessionId)+"/prompt" || op.Source.Kind != call.Source.Kind || value(op.Source.GrantId) != value(call.Source.GrantId) {
		return errors.New("Caelis 工具来源不属于当前应用提交")
	}
	if exists && !sameInvocation(record.Call, call) {
		return errors.New("Caelis 工具调用内容发生改变")
	}
	path := "/application/sessions/" + idPath(b.SessionId) + "/calls/" + idPath(call.Id)
	if !exists {
		record = callRecord{Call: call, Phase: "claiming"}
		if e := s.saveCall(call.Id, record); e != nil {
			return e
		}
		if call.State == "pending" {
			var claimed wire.ApplicationCall
			e := c.json(ctx, "POST", path+"/claim", struct{}{}, &claimed, "", "")
			if e == nil && claimed.State == "claimed" && sameInvocation(claimed, call) {
				record.Phase = "executing"
				if e = s.saveCall(call.Id, record); e != nil {
					return e
				}
				args, _ := json.Marshal(call.Arguments)
				result := api.ToolResult{IsError: true, Content: []map[string]string{{"type": "text", "text": "The original application tool version is unavailable; no effect was performed."}}}
				if host != nil {
					result = host.CallTool(context.WithValue(ctx, invocationKey{}, call), call.Name, args)
				}
				content, _ := json.Marshal(result.Content)
				outcome := "succeeded"
				if result.IsError {
					outcome = "failed"
				}
				record.Receipt = &wire.ApplicationCallResult{Outcome: outcome, Content: content}
			}
			// Lost claim response never grants an effect, even if a later read says
			// claimed. A crash after executing is equally uncertain, never replayed.
		}
	}
	if record.Receipt == nil {
		record.Receipt = &wire.ApplicationCallResult{Outcome: "unknown", Content: json.RawMessage(`[{"type":"text","text":"Tool outcome is uncertain; do not automatically repeat the effect."}]`)}
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
func sameInvocation(a, b wire.ApplicationCall) bool {
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
