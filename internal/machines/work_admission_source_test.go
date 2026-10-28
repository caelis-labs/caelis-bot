package machines

import (
	"context"
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type sourceAdmissionLocal struct {
	inert
	cause error
}

func (w sourceAdmissionLocal) WorkAdmission(context.Context) error { return w.cause }

func TestRemoteContinuationPreservesResidentSourceRefusal(t *testing.T) {
	cause := errors.New("summary cannot authorize remote continuation")
	s, err := Open(t.TempDir(), sourceAdmissionLocal{cause: &api.WorkAdmissionError{Code: "source_not_authorized", Cause: cause}})
	if err != nil {
		t.Fatal(err)
	}
	s.state.Routes["original-task"] = "unopened-remote"
	_, err = s.SendWork(t.Context(), api.TaskMessage{ID: "original-task", RequestID: "original-request", Prompt: "synthetic"})
	var rejected *api.WorkMessageRejection
	if !errors.As(err, &rejected) || rejected.Code != "source_not_authorized" || !errors.Is(err, cause) {
		t.Fatal("lost source admission or reached remote", err)
	}
}
