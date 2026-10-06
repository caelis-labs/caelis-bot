package caelis

import (
	"bytes"
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
	var recoveredClient *client
	var recoveredSession string
	var recoveredGeneration uint64
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
		generation, observationCtx := s.generation, s.streamCtx
		s.mu.Unlock()
		if !ready || binding.SessionId == "" {
			recoveredClient = nil
			continue
		}
		var e error
		if recoveredClient != c || recoveredSession != binding.SessionId || recoveredGeneration != generation {
			// Recovery includes claimed calls; waiting only returns new pending
			// calls and must never replace this uncertain-effect reconciliation.
			e = s.pollCalls(ctx, c, binding, life)
			if e == nil {
				recoveredClient, recoveredSession, recoveredGeneration = c, binding.SessionId, generation
			}
		} else {
			if observationCtx == nil {
				observationCtx = ctx
			}
			var calls []wire.ApplicationCall
			calls, e = waitCalls(observationCtx, c, binding.SessionId)
			s.mu.Lock()
			current := s.connected && !s.closed && s.client == c && s.generation == generation && s.state.Session.SessionId == binding.SessionId
			s.mu.Unlock()
			if !current {
				recoveredClient = nil
				continue
			}
			if e == nil {
				for _, call := range calls {
					if call.State != "pending" {
						e = errors.New("Caelis 等待调用返回无效状态")
						break
					}
					if e = s.handleCall(ctx, c, binding, life, call); e != nil {
						break
					}
				}
			}
		}
		if e != nil && ctx.Err() == nil {
			recoveredClient = nil
			s.componentError("application_calls", binding.SessionId, e)
		}
	}
}

func waitCalls(ctx context.Context, c *client, sid string) ([]wire.ApplicationCall, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	observation, closeTransport := c.observationClient()
	defer closeTransport()
	var calls []wire.ApplicationCall
	err := observation.jsonTimeout(ctx, "GET", "/application/sessions/"+idPath(sid)+"/calls?wait=true", nil, &calls, "", "", time.Minute)
	// An idle read wait or replacement of its owning stream has no uncertain
	// effect to reconcile and is not a disconnection. Other failures still are.
	if err != nil && ctx.Err() != nil {
		return nil, nil
	}
	return calls, err
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
	contentV1 := s.state.ContentCatalogs[call.ToolsVersion][call.Name]
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
				blocks := result.Content
				if contentV1 {
					blocks = contentV1Blocks(result)
				}
				content, _ := json.Marshal(blocks)
				outcome := "succeeded"
				if result.IsError {
					outcome = "failed"
				}
				record.Receipt = &wire.ApplicationCallResult{Outcome: outcome, Content: json.RawMessage(content)}
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
		record.Receipt = &wire.ApplicationCallResult{Outcome: "unknown", Content: json.RawMessage(`[{"type":"text","text":"Tool outcome is uncertain; do not automatically repeat the effect."}]`)}
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
