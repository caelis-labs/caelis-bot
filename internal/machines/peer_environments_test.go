package machines

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
	"time"
)

func TestTaskEnvironmentsExcludeUnapprovedPeers(t *testing.T) {
	s, err := Open(t.TempDir(), inert{})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "unknown", "rejected", "expired", "revoked", "approved"} {
		s.state.Profiles[status] = profile{View: api.Machine{ID: status, Name: status, State: "ready", Runtime: "codex", ConnectionType: "bot", PairingStatus: status, CheckedAt: time.Now().UnixMilli()}}
	}
	items := s.TaskMachines()
	if len(items) != 1 || items[0].ID != "approved" {
		t.Fatal("unapproved peer offered for delegation", items)
	}
	for _, status := range []string{"pending", "unknown", "rejected", "expired", "revoked"} {
		if _, err := s.BindWork(t.Context(), api.TaskStart{Machine: status}, "task-"+status, ""); err == nil {
			t.Fatal("unapproved peer reserved native work", status)
		}
	}
	if len(s.state.Routes) != 0 {
		t.Fatal("pending created routes")
	}
}
