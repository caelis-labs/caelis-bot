package productmanagement

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func validScope(scope Scope) bool {
	return identifier.MatchString(scope.BotID) && identifier.MatchString(scope.Generation)
}
func validRuntime(provider string) bool { return provider == "codex" || provider == "caelis" }
func validOutcome(value string) bool {
	return value == "accepted" || value == "rejected" || value == "unknown"
}
func validRuntimeCommand(command RuntimeCommand) bool {
	if !identifier.MatchString(command.ID) || !validRuntime(command.Runtime) || len(command.Version) > 128 || len(command.ExpectedVersion) > 128 {
		return false
	}
	switch command.Action {
	case "detect", "check-update":
		return command.Version == "" && command.ExpectedVersion == ""
	case "install":
		return command.Version != "" && command.ExpectedVersion == ""
	case "update":
		return command.Version != "" && command.ExpectedVersion != ""
	case "resolve":
		return command.Version != ""
	}
	return false
}

func publicText(value string, limit int) bool {
	return len(value) <= limit && !strings.ContainsAny(value, "\x00\r\n")
}

func validConfigurationChange(change api.RuntimeConfigurationChange) bool {
	if change.ExpectedRevision == "" || len(change.ExpectedRevision) > 20 {
		return false
	}
	if _, err := strconv.ParseUint(change.ExpectedRevision, 10, 64); err != nil {
		return false
	}
	if !publicText(change.ID, 256) || !publicText(change.Name, 256) || len(change.Description) > 8192 || strings.ContainsRune(change.Description, '\x00') || !publicText(change.Selection.Model, 512) || !publicText(change.Selection.Effort, 64) || !publicText(change.Selection.ServiceTier, 64) {
		return false
	}
	emptySelection := change.Selection == (api.WorkExecutionSettings{})
	switch change.Action {
	case "main":
		return change.Selection.Model != "" && change.ID == "" && change.Name == "" && change.Description == ""
	case "bind":
		return change.ID != "" && change.Selection.Model != "" && change.Name == "" && change.Description == ""
	case "reset", "delete-role", "remove-model", "disconnect-agent":
		return change.ID != "" && change.Name == "" && change.Description == "" && emptySelection
	case "create-role":
		return change.ID != "" && change.Name == "" && emptySelection
	case "save-set", "apply-set", "delete-set":
		return change.Name != "" && change.ID == "" && change.Description == "" && emptySelection
	}
	return false
}
