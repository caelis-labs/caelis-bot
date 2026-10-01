package desktop

import (
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// This explicit acceptance fixture owns only a disposable client in its private
// temporary directory. Its unused endpoint and generation do not represent a
// Runtime, a task ledger record or a user session.
func terminalSmokeTarget(directory, binary string) api.TerminalTarget {
	return api.TerminalTarget{
		Target:     api.WorkTarget{NodeID: "terminal-acceptance-local", Backend: "codex", Role: api.RoleWorker},
		Locality:   api.TerminalLocal,
		Generation: "synthetic-terminal-acceptance",
		Runtime:    "codex",
		Binary:     binary,
		Directory:  directory,
		Endpoint:   "unix://" + filepath.Join(directory, "unused-smoke.sock"),
		Thread:     "synthetic",
	}
}
