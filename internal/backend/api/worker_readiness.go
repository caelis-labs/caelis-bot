package api

import (
	"context"
	"errors"
)

// A bounded, public reason allowlist. Native error text, paths and credentials
// must not become pairing UI or HTTP error details.
func WorkerReadinessCode(err error) string {
	var blocked *SetupBlockedError
	var rejected *ActionRejectedError
	code := ""
	if errors.As(err, &blocked) {
		code = blocked.Code
	} else if errors.As(err, &rejected) {
		code = rejected.Code
	}
	switch code {
	case "worker_runtime_missing", "worker_runtime_unavailable", "worker_runtime_incompatible", "worker_login_required", "worker_model_authorization_required", "worker_model_required", "worker_model_catalog_unavailable", "worker_model_unavailable", "worker_effort_unsupported", "worker_tier_unsupported", "worker_configuration_invalid", "worker_configuration_changed", "worker_readiness_timeout":
		return code
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "worker_readiness_timeout"
	}
	return ""
}
