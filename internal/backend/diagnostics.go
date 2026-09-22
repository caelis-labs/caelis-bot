package backend

import (
	"encoding/json"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Allowlist construction, not regex redaction of logs. Native message/error
// strings, conversation bytes, identities, arguments and paths never enter it.
func diagnosticEnum(value string, allowed ...string) string {
	for _, v := range allowed {
		if v == value {
			return value
		}
	}
	return "other"
}
func (s *Service) DiagnosticReport() ([]byte, error) {
	v := s.engine.Snapshot()
	manual := s.RuntimeSettings().CLIPath != ""
	s.mu.Lock()
	draftPresent := s.draft.Text != "" || len(s.draft.ReferenceIDs) > 0
	draftIssue := s.draftLoadError != nil || s.draftNotice != ""
	s.mu.Unlock()
	deps := map[string]string{}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/wailsapp/wails/v3" {
				deps["wails"] = dep.Version
			}
		}
	}
	report := map[string]any{
		"formatVersion": 1, "generatedAt": time.Now().UTC().Format(time.RFC3339),
		"platform":            map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version()},
		"dependencies":        deps,
		"connection":          diagnosticEnum(v.Connection, "offline", "connecting", "login", "ready"),
		"connectionIssue":     diagnosticEnum(v.ConnectionIssue, "", "connection", "runtime_missing", "runtime_protocol", "existing_server", "authentication"),
		"phase":               diagnosticEnum(v.Phase, "idle", "working", "sending", "attention", "interrupting", "completed", "failed", "interrupted", "unknown"),
		"manualCLIConfigured": manual, "loginPending": v.LoginPending,
		"canSend": v.CanSend, "canInterrupt": v.CanInterrupt,
		"loadedItems": len(v.Items), "approvalCount": len(v.Approvals), "reviewCount": len(v.Reviews), "hasEarlierMessages": v.HasEarlier,
		"draftPresent": draftPresent, "draftStorageIssue": draftIssue,
	}
	if detail, ok := s.engine.(api.DiagnosticSource); ok {
		report["backend"] = detail.DiagnosticStatus()
	}
	return json.MarshalIndent(report, "", "  ")
}
