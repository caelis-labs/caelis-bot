package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestUnusedSetupProofIsReadOnlyAndFailsClosed(t *testing.T) {
	root := t.TempDir()
	s := NewSession(SessionOptions{Directory: root, StateFile: filepath.Join(root, "binding.json")})
	defer s.cancelLife()
	empty := binding{Version: 1, ThreadID: "unused", Unsubmitted: true, OwnerUnavailable: true, OwnerEndpoint: "unix:///original.sock", OwnerProcess: &ownerProcessIdentity{PID: 1001, Birth: 91}}
	s.binding = empty
	s.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return ownerGone }
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.opts.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !s.CanLeaveUnsubmittedSetup() {
		t.Fatal("proven empty exited setup owner blocked")
	}
	after, _ := os.ReadFile(s.opts.StateFile)
	if !bytes.Equal(before, after) {
		t.Fatal("proof mutated original ledger")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*binding)
	}{
		{"submitted", func(b *binding) { b.Unsubmitted = false }},
		{"unknown owner", func(b *binding) { b.OwnerUnavailable = false }},
		{"missing identity", func(b *binding) { b.OwnerProcess = nil }},
		{"closing", func(b *binding) { b.OwnerClosing = true }},
		{"pending", func(b *binding) { b.Pending = &pendingSubmission{ID: "original"} }},
		{"receipt", func(b *binding) { b.LastReceipt = &api.Receipt{ID: "original", Outcome: "unknown"} }},
		{"worker", func(b *binding) { b.Tasks = map[string]*taskRecord{"original": {}} }},
		{"child", func(b *binding) { b.Children = []string{"original"} }},
		{"approval", func(b *binding) { b.PendingApprovalIDs = []string{"original"} }},
		{"stop", func(b *binding) { b.CapacityStopRequest = "original" }},
		{"renewal", func(b *binding) { b.ToolRenewal = &toolRenewalRecord{Attempted: true} }},
		{"history", func(b *binding) { b.PastThreads = []string{"original"} }},
		{"schedule", func(b *binding) { b.Scheduled = map[string]string{"original": "turn"} }},
		{"lost owner", func(b *binding) { b.LostOwners = []lostOwnerRecord{{Thread: "original"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.binding = empty
			tc.mutate(&s.binding)
			if s.CanLeaveUnsubmittedSetup() {
				t.Fatal("uncertain work admitted")
			}
		})
	}
	s.binding = empty
	for _, life := range []ownerLifecycle{ownerUnknown, ownerAlive} {
		s.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return life }
		if s.CanLeaveUnsubmittedSetup() {
			t.Fatal("unverified exit admitted")
		}
	}
}
