package workerpeer

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type receiptOnlyOwner struct {
	*fixtureOwner
	connects, reads int
	outcome         string
}

func (o *receiptOnlyOwner) Connect(context.Context) error {
	o.connects++
	return errors.New("original_owner_exited")
}
func (o *receiptOnlyOwner) ReadWorkURL(context.Context, string, string) (api.WorkURLPage, error) {
	return api.WorkURLPage{}, errors.New("original_request_invalid")
}
func (o *receiptOnlyOwner) SubmitWorkURL(context.Context, api.WorkURLSubmission) (api.WorkOperation, error) {
	return api.WorkOperation{}, errors.New("original_request_invalid")
}
func (o *receiptOnlyOwner) ReadWorkURLSubmission(_ context.Context, task, id, op string) (api.WorkOperation, error) {
	o.reads++
	if task != "original-task" || id != "original-request" || op != "original-op" {
		return api.WorkOperation{}, errors.New("wrong_original_reference")
	}
	return api.WorkOperation{WorkOperationQuery: api.WorkOperationQuery{TaskID: task, RequestID: op, Kind: api.WorkURLSubmitKind, Fingerprint: "original-digest"}, Outcome: o.outcome}, nil
}
func TestURLReceiptSurvivesOwnerExitWithoutConnectOrSourceEscape(t *testing.T) {
	owner := &receiptOnlyOwner{fixtureOwner: newFixtureOwner(), outcome: "unknown"}
	s, err := Open(t.TempDir(), func(context.Context, string, string, string) (Owner, string, *api.WorkExecutionSettings, error) {
		return owner, "codex", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Enable(true); err != nil {
		t.Fatal(err)
	}
	// Persisted grant/binding is the execution route even though the native owner is gone.
	s.state.Grants["source-a"] = grant{Peer: "source-a", Hash: tokenHash("original-proof")}
	s.state.Grants["source-b"] = grant{Peer: "source-b", Hash: tokenHash("other-proof")}
	s.state.Bindings["source-a"] = map[string]string{"original-task": "codex"}
	h := httptest.NewServer(s)
	defer h.Close()
	c := Connection{Address: h.URL, Peer: s.state.Identity, Token: "original-proof"}
	in := Request{Action: "url-operation", ID: "original-task", Ref: "original-request", Query: api.WorkOperationQuery{RequestID: "original-op"}}
	out, err := Call(t.Context(), c, in)
	if err != nil || out.Operation.Outcome != "unknown" || out.Operation.Fingerprint != "original-digest" || owner.connects != 0 || owner.reads != 1 {
		t.Fatalf("original receipt blocked/reconnected: %+v %v connects=%d reads=%d", out, err, owner.connects, owner.reads)
	}
	owner.outcome = "accepted"
	out, err = Call(t.Context(), c, in)
	if err != nil || out.Operation.Outcome != "accepted" || owner.connects != 0 || owner.reads != 2 {
		t.Fatal("known original outcome lost after native exit", out, err)
	}
	c.Token = "other-proof"
	if _, err = Call(t.Context(), c, in); err == nil || owner.reads != 2 {
		t.Fatal("another authenticated source read original receipt", err, owner.reads)
	}
}
