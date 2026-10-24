package caelis

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type legacyCollisionReceipt struct {
	reads int
	op    string
}

func (rt *legacyCollisionReceipt) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.reads++
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"operation_id":%q,"outcome":"accepted","target":{"turn_id":"source-A-turn"}}`, rt.op)))}, nil
}
func TestLegacyForeignUnknownCannotAdoptAnotherSourceReceipt(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(fmt.Sprint("background=", background), func(t *testing.T) {
			o, e := NewWorkOwner(Options{Directory: filepath.Join(t.TempDir(), "foreign"), WorkSourceIdentity: "source-B"})
			if e != nil {
				t.Fatal(e)
			}
			s := o.engine
			w := worker{Native: true, Task: api.Task{ID: "source-B-task"}}
			s.state.Workers[w.Task.ID] = w
			intent := workIntent{w.Task.ID, "same-request", "send", "B-body-digest"}
			op := w.operationID("send", intent.RequestID)
			rt := &legacyCollisionReceipt{op: op}
			s.client = &client{origin: "http://127.0.0.1:12345", http: &http.Client{Transport: rt}}
			s.state.Operations[op] = journal{Work: &intent, Outcome: "unknown", Digest: intent.Fingerprint, Path: "/sessions/B/prompt"}
			if background {
				e = s.recoverSelectedOperations(context.Background(), nil)
			} else {
				result, err := o.ReadWorkOperation(t.Context(), api.WorkOperationQuery{TaskID: intent.TaskID, RequestID: intent.RequestID, Kind: intent.Kind, Fingerprint: intent.Fingerprint})
				e = err
				if result.Outcome != "unknown" {
					t.Error("source-B read adopted source-A receipt", result.Outcome)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			j := s.state.Operations[op]
			if j.Outcome != "unknown" || j.TurnID != "" || rt.reads != 0 {
				t.Fatal("legacy foreign receipt polluted by principal-global ID", j.Outcome, j.TurnID, rt.reads)
			}
			// A known old outcome is retained; local/helper reconciliation keeps working.
			s.state.Operations[op] = journal{Work: &intent, Outcome: "accepted"}
			if e = s.recoverOperation(t.Context(), op); e != nil || s.state.Operations[op].Outcome != "accepted" {
				t.Fatal("known historical result lost", e)
			}
			s.state.Operations[op] = journal{Work: &intent, Outcome: "unknown", Digest: intent.Fingerprint}
			s.workSourceIdentity = ""
			if e = s.recoverOperation(t.Context(), op); e != nil || s.state.Operations[op].Outcome != "accepted" {
				t.Fatal("local/helper original recovery changed", e)
			}
		})
	}
}
