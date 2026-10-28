package app

import (
	"context"
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type sourceAdmissionOwner struct {
	*workerFixture
	admit error
}

func (w *sourceAdmissionOwner) WorkAdmission(context.Context) error { return w.admit }

func TestResidentAdmissionRefusalKeepsSourceAndCauseOnContinuation(t *testing.T) {
	for _, typed := range []bool{false, true} {
		cause := errors.New("source refused")
		admission := error(cause)
		reason := "work_admission_failed"
		if typed {
			reason = "source_not_authorized"
			admission = &api.WorkAdmissionError{Code: reason, Cause: cause}
		}
		owner := &sourceAdmissionOwner{workerFixture: &workerFixture{runtime: "caelis", states: map[string]api.WorkState{}}, admit: admission}
		pool := workPool(t.TempDir(), "caelis", map[string]*workerFixture{"caelis": owner.workerFixture})
		pool.resident = owner
		_, err := pool.SendWork(t.Context(), api.TaskMessage{ID: "original-task", RequestID: "original-request", Prompt: "synthetic continuation"})
		var rejection *api.WorkMessageRejection
		if !errors.As(err, &rejection) || rejection.Code != reason || !errors.Is(err, cause) || owner.sends != 0 || owner.connections != 0 {
			t.Fatal("lost admission cause or dispatched without authority", err)
		}
	}
}
