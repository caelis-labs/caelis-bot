package api

import (
	"context"
	"errors"
	"testing"
)

func TestWorkerReadinessReasonsAreBoundedAndDoNotExposeNativeText(t *testing.T) {
	for _, code := range []string{"worker_runtime_missing", "worker_login_required", "worker_model_required", "worker_model_authorization_required", "worker_effort_unsupported", "worker_tier_unsupported", "worker_configuration_invalid", "worker_runtime_unavailable"} {
		if WorkerReadinessCode(errors.Join(&SetupBlockedError{Code: code, Message: "private path/token"}, errors.New("private detail"))) != code {
			t.Fatal(code)
		}
	}
	for _, err := range []error{errors.New("/private/token worker_runtime_missing"), &SetupBlockedError{Code: "private/token", Message: "private"}} {
		if WorkerReadinessCode(err) != "" {
			t.Fatal("unclassified native text became authority")
		}
	}
	if WorkerReadinessCode(context.DeadlineExceeded) != "worker_readiness_timeout" {
		t.Fatal("timeout claimed missing settings")
	}
}
