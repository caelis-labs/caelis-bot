package websurface

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type workerURLFixture struct{ calls []api.WorkURLSubmission }

func (f *workerURLFixture) WorkerApprovalTaskID(id string) (string, error) {
	return "original-task", nil
}
func (f *workerURLFixture) ReadWorkURL(context.Context, string, string) (api.WorkURLPage, error) {
	return api.WorkURLPage{Revision: "original-revision", Fields: []api.WorkURLField{}}, nil
}
func (f *workerURLFixture) SubmitWorkURL(_ context.Context, in api.WorkURLSubmission) (api.WorkOperation, error) {
	f.calls = append(f.calls, in)
	return api.WorkOperation{WorkOperationQuery: api.WorkOperationQuery{RequestID: in.RequestID}, Outcome: "unknown"}, nil
}
func (f *workerURLFixture) ReadWorkURLSubmission(context.Context, string, string, string) (api.WorkOperation, error) {
	return api.WorkOperation{Outcome: "unknown"}, nil
}
func TestURLPageActionUsesOriginalOwnerWithoutWebSecretJournal(t *testing.T) {
	s := testServer(t)
	owner := &workerURLFixture{}
	s.services.Backend.ConfigureWorkerInteractions(owner)
	c := pairClient(t, s)
	response := c.request("/api/v1/conversation/approval-page-submit", map[string]any{"args": []any{"source-original-request", "stable-original-url-op", "original-revision", map[string]string{"secret": "synthetic-sensitive"}}})
	var out struct{ Value api.WorkURLReceipt }
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &out) != nil || out.Value.Outcome != "unknown" {
		t.Fatal(response.Code, response.Body.String())
	}
	if len(owner.calls) != 1 || owner.calls[0].TaskID != "original-task" || owner.calls[0].ApprovalID != "source-original-request" || owner.calls[0].RequestID != "stable-original-url-op" {
		t.Fatal(owner.calls)
	}
	if len(s.operations) != 0 {
		t.Fatal("created duplicate Web operation/secret journal")
	}
}
