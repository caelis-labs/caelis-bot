package bot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestEventWriteDiagnosticsSeparateWaitAndFinalDurability(t *testing.T) {
	r, _ := systemFixture(t)
	r.events.Close()
	r.events = nil
	done := make(chan localstate.WriteCompletion, 1)
	r.eventsErr = &localstate.WriteError{Stage: "sync", Elapsed: 2 * time.Second, Err: context.DeadlineExceeded, Completion: done}
	dir := t.TempDir()
	log := diagnosticlog.New(dir)
	r.ConfigureDiagnostics(log)
	r.ConfigureDiagnostics(log)
	done <- localstate.WriteCompletion{Elapsed: 2300 * time.Millisecond, Err: &localstate.WriteError{
		Stage: "parent_sync", Elapsed: 2300 * time.Millisecond,
		Err: &os.PathError{Op: "sync", Path: "/private-document", Err: syscall.EIO},
	}}
	path := filepath.Join(dir, "error.jsonl")
	var records string
	for limit := time.Now().Add(5 * time.Second); time.Now().Before(limit); {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		records = string(raw)
		if strings.Contains(records, "storage_write_finished_after_timeout") {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for _, want := range []string{"kind=deadline stage=sync", "elapsed_ms=2000 outcome=writer_in_flight",
		"kind=filesystem stage=parent_sync operation=sync", "elapsed_ms=2300 outcome=renamed_durability_unconfirmed"} {
		if !strings.Contains(records, want) {
			t.Fatalf("missing %q in diagnostics: %s", want, records)
		}
	}
	if strings.Contains(records, "/private-document") || strings.Count(records, "\n") != 2 {
		t.Fatalf("diagnostics exposed a path or duplicated the first fault: %s", records)
	}
}
