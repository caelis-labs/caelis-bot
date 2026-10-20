package tasks

import (
	"testing"
	"time"
)

func TestTaskListPresentsOriginalCreationTimeWithoutInventingLegacyTime(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "codex", f)
	now := time.Date(2026, 10, 20, 1, 2, 3, 123000000, time.FixedZone("fixture", 3600))
	m.now = func() time.Time { return now }
	dated := start(t, m, "dated-original-request")
	legacy := start(t, m, "legacy-original-request")
	m.mu.Lock()
	m.state.Records[legacy.ID].CreatedAt = 0
	m.mu.Unlock()
	got := m.ListTasks()
	if len(got) != 2 || f.starts != 2 {
		t.Fatal("list changed original work", len(got), f.starts)
	}
	for _, task := range got {
		want := ""
		if task.ID == dated.ID {
			want = now.UTC().Format(time.RFC3339Nano)
		}
		if task.CreatedAt != want || task.Result != "" {
			t.Fatal("incorrect read-only presentation", task.ID, task.CreatedAt, want)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Records[dated.ID].View.CreatedAt != "" || m.state.Records[legacy.ID].CreatedAt != 0 {
		t.Fatal("presentation mutated stored task")
	}
}
