package machines

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestTaskEnvironmentsOfferBothConnectionsWithoutMovingOriginalTasks(t *testing.T) {
	s, err := Open(t.TempDir(), inert{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.state.Profiles["legacy"] = profile{View: api.Machine{ID: "legacy", Name: "old helper", State: "ready", Runtime: "codex"}}
	s.state.Profiles["bot-peer"] = profile{View: api.Machine{ID: "bot-peer", Name: "Bot B", State: "ready", Runtime: "codex", ConnectionType: "bot"}}
	s.state.Routes["old-task"] = "legacy"
	s.state.Runtimes["old-task"] = "codex"
	items := s.TaskMachines()
	if len(items) != 2 {
		t.Fatal("explicit SSH and Bot environments missing", items)
	}
	if s.route("old-task") != "legacy" || len(s.Machines()) != 2 {
		t.Fatal("legacy owner or repair settings removed")
	}
}
