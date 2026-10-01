package productrpc

import (
	"context"
	"errors"
	"net/http"

	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

func (s *Server) readExecution(w http.ResponseWriter, r *http.Request, scope Scope) {
	if s.execution == nil {
		problem(w, 409, "execution-unavailable")
		return
	}
	v, err := s.execution.ExecutionSettings(r.Context(), managementScope(scope))
	if err != nil {
		problem(w, 409, "execution-unavailable")
		return
	}
	if v.Scope != managementScope(scope) || !productmanagement.ValidExecutionView(v) {
		problem(w, 502, "invalid-execution-view")
		return
	}
	s.write(w, v)
}

func (s *Server) executeExecution(ctx context.Context, c Command) Result {
	v := productmanagement.ExecutionResult{Scope: c.Execution.Scope, ID: c.ID, Outcome: "rejected", Code: "execution-unavailable"}
	result := Result{ID: c.ID, Outcome: v.Outcome, Code: v.Code, Execution: &v}
	if s.execution == nil {
		return result
	}
	if s.journal.executionUnresolved(c.ID) {
		v.Code = "execution-outcome-unresolved"
		result.Code = v.Code
		return result
	}
	native, err := s.execution.ChangeExecutionSettings(ctx, *c.Execution)
	if native.Scope != v.Scope || native.ID != c.ID || !receiptOutcome(native.Outcome) || (err != nil && native.Outcome != "unknown") {
		v.Outcome, v.Code = "unknown", "invalid-execution-receipt"
	} else {
		v = native
	}
	result.Outcome, result.Code = v.Outcome, v.Code
	return result
}

func (c *Client) ExecutionSettings(ctx context.Context) (productmanagement.ExecutionView, error) {
	var v productmanagement.ExecutionView
	err := c.managementQuery(ctx, "/v1/management/execution", "", &v)
	if err != nil {
		return productmanagement.ExecutionView{}, err
	}
	scope, err := c.scope()
	if err != nil {
		return productmanagement.ExecutionView{}, err
	}
	if v.Scope != managementScope(scope) || !productmanagement.ValidExecutionView(v) {
		return productmanagement.ExecutionView{}, errors.New("execution view identity mismatch")
	}
	return v, nil
}
func (c *Client) ChangeExecutionSettings(ctx context.Context, in productmanagement.ExecutionCommand) (productmanagement.ExecutionResult, error) {
	scope, err := c.scope()
	if err != nil {
		return productmanagement.ExecutionResult{}, err
	}
	in.Scope = managementScope(scope)
	unknown := productmanagement.ExecutionResult{Scope: in.Scope, ID: in.ID, Outcome: "unknown", Code: "response-unobserved"}
	r, err := c.Command(ctx, Command{ID: in.ID, Kind: "configure-execution", Execution: &in})
	if err != nil {
		return unknown, err
	}
	if r.ID != in.ID || !receiptOutcome(r.Outcome) {
		return unknown, errors.New("execution receipt identity mismatch")
	}
	if r.Execution == nil {
		unknown.Outcome, unknown.Code = r.Outcome, r.Code
		return unknown, nil
	}
	v := *r.Execution
	// Retained original receipts keep their original generation across a restart;
	// only lookup/replay observes them, never a new native settings dispatch.
	if v.ID != in.ID || v.BotID != in.BotID || !receiptOutcome(v.Outcome) || v.Outcome != r.Outcome {
		return unknown, errors.New("execution receipt identity mismatch")
	}
	return v, nil
}
