package tasks

import (
	"path/filepath"
	"testing"
	"time"
)

// A and B can both be unknown without an intervening working observation.
// Each native execution gets its own grace, surviving restart and late A.
func TestNewUnknownExecutionGetsOwnRecoveryGrace(t *testing.T) {
	for _, provider := range []string{"codex", "caelis"} {
		t.Run(provider, func(t *testing.T) {
			root, f := t.TempDir(), newRuntime()
			now := time.Unix(100000, 0)
			m := openFixture(t, root, provider, f)
			m.now = func() time.Time { return now }
			v := start(t, m, "unknown-generation")
			state := f.states[v.ID]
			state.Task.Status = "unknown"
			f.states[v.ID] = state
			if err := m.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			now = now.Add(31 * time.Second)
			if got, err := m.ClaimUnknownNotices(); err != nil || len(got) != 1 || got[0] != v.ID {
				t.Fatalf("A: got %v, %v", got, err)
			}
			aReport := m.state.Records[v.ID].ReportID
			state.ExecutionKey = "native-turn-B"
			f.states[v.ID] = state
			if err := m.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			if got, err := m.ClaimUnknownNotices(); err != nil || len(got) != 0 {
				t.Fatalf("B alerted before its own grace: %v, %v", got, err)
			}
			m = openFixture(t, root, provider, f)
			m.now = func() time.Time { return now }
			now = now.Add(29 * time.Second)
			if got, err := m.ClaimUnknownNotices(); err != nil || len(got) != 0 {
				t.Fatalf("restart reset B grace: %v, %v", got, err)
			}
			now = now.Add(time.Second)
			if got, err := m.ClaimUnknownNotices(); err != nil || len(got) != 1 || got[0] != v.ID {
				t.Fatalf("B was not alerted at its own deadline: %v, %v", got, err)
			}
			r := m.state.Records[v.ID]
			if r.ReportID == aReport || len(r.PreviousReports) != 1 || r.PreviousReports[0].ID != aReport {
				t.Fatal("A receipt was replaced", r.ReportID, r.PreviousReports)
			}
			m = openFixture(t, root, provider, f)
			m.now = func() time.Time { return now }
			if got, err := m.ClaimUnknownNotices(); err != nil || len(got) != 0 || f.starts != 1 || f.reports != 0 {
				t.Fatalf("restart replayed B or dispatched work: %v, %v, starts=%d reports=%d", got, err, f.starts, f.reports)
			}
		})
	}
}

func TestLateAProjectionCannotDismissBUnknown(t *testing.T) {
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	v := start(t, m, "review-late-A")
	a := f.states[v.ID]
	a.Task.Status = "unknown"
	f.states[v.ID] = a
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	aNotice := m.TaskPreviews()[0].NoticeGeneration
	b := a
	b.ExecutionKey = "native-turn-B"
	f.states[v.ID] = b
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	bNotice := m.TaskPreviews()[0].NoticeGeneration
	if aNotice == bNotice {
		t.Fatal("new execution retained old notification identity")
	}
	lateA := a
	lateA.Task.Status = "completed"
	f.states[v.ID] = lateA
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if r := m.state.Records[v.ID]; r.Execution != "native-turn-B" || r.View.Status != "unknown" {
		t.Fatalf("fixture did not retain B: %+v", r)
	}
	previews := m.TaskPreviews()
	if len(previews) != 1 {
		t.Fatalf("fixture preview missing: %+v", previews)
	}
	for _, p := range previews {
		if p.ID == v.ID && (p.Status != "unknown" || p.NoticeGeneration != bNotice) {
			t.Fatalf("dock sees late A as %q and would dismiss B notice", p.Status)
		}
	}
	f.states[v.ID] = b
	if p := m.TaskPreviews(); len(p) != 1 || p[0].Status != "unknown" {
		t.Fatal("B did not recover its own preview", p)
	}
	b.Task.Status = "completed"
	f.states[v.ID] = b
	if p := m.TaskPreviews(); len(p) != 1 || p[0].Status != "completed" {
		t.Fatal("current B completion was hidden", p)
	}
}

func TestNewExecutionWaitingApprovalStartsUnknownGraceOnUnknown(t *testing.T) {
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(200000, 0)
	m.now = func() time.Time { return now }
	v := start(t, m, "waiting-new-execution")
	state := f.states[v.ID]
	state.Task.Status = "unknown"
	f.states[v.ID] = state
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	state.ExecutionKey, state.Task.Status = "native-turn-B", "waiting_approval"
	f.states[v.ID] = state
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if p := m.TaskPreviews(); len(p) != 1 || p[0].Status != "waiting_approval" {
		t.Fatal("real pending approval lost its task status", p)
	}
	if got, err := m.ClaimUnknownNotices(); err != nil || len(got) != 0 {
		t.Fatal("approval was called unknown", got, err)
	}
	now = now.Add(31 * time.Second)
	state.Task.Status = "unknown"
	f.states[v.ID] = state
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if got, err := m.ClaimUnknownNotices(); err != nil || len(got) != 0 {
		t.Fatal("new unknown did not get its own grace", got, err)
	}
}
