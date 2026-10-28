package tasks

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestLegacyFinishMigratesOnlyProvenGenerationWithoutReissuingReport(t *testing.T) {
	for _, delivery := range []string{"pending", "observed", "accepted", "unknown"} {
		t.Run(delivery, func(t *testing.T) {
			root := t.TempDir()
			f := newRuntime()
			m := openFixture(t, root, "codex", f)
			task, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "original-start", Prompt: "original"})
			if err != nil {
				t.Fatal(err)
			}
			s := f.states[task.ID]
			s.Task.Status = "completed"
			s.Activity = "idle"
			s.ExecutionKey = "native-one"
			s.LegacyExecutionKey = "legacy-report-one"
			f.states[task.ID] = s
			r := m.state.Records[task.ID]
			r.Execution = "legacy-report-one"
			r.FinishedExecution = r.Execution
			r.ReportID = "original-report-id"
			r.ReportState = delivery
			r.ReportText = "frozen original report"
			if err = m.write(); err != nil {
				t.Fatal(err)
			}
			if !m.InteractionStates(f.WorkStates())[0].TaskEnded {
				t.Fatal("proven legacy finish lost before refresh")
			}
			m = openFixture(t, root, "codex", f)
			if err = m.refresh(); err != nil {
				t.Fatal(err)
			}
			r = m.state.Records[task.ID]
			if r.Execution != "native-one" || r.FinishedExecution != "native-one" || r.ReportID != "original-report-id" || r.ReportState != delivery || r.ReportText != "frozen original report" || len(r.PreviousReports) != 0 || len(r.RetiredExecutions) != 0 {
				t.Fatal("migration changed report receipt", r)
			}
			m = openFixture(t, root, "codex", f)
			if !m.InteractionStates(f.WorkStates())[0].TaskEnded {
				t.Fatal("restart lost migrated finish")
			}
			s.ExecutionKey = "native-two"
			s.LegacyExecutionKey = ""
			s.Task.Status = "working"
			s.Activity = "active"
			f.states[task.ID] = s
			if m.InteractionStates(f.WorkStates())[0].TaskEnded {
				t.Fatal("old finish applied to active continuation")
			}
			if err = m.refresh(); err != nil {
				t.Fatal(err)
			}
			if m.state.Records[task.ID].FinishedExecution != "native-one" || f.reports != 0 {
				t.Fatal("continuation rewrote finish or sent a report")
			}
		})
	}
}

func TestLegacyFinishedPreviousReportDoesNotFenceContinuationAnswer(t *testing.T) {
	root := t.TempDir()
	f := newRuntime()
	m := openFixture(t, root, "codex", f)
	task, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "original", Prompt: "original"})
	if err != nil {
		t.Fatal(err)
	}
	r := m.state.Records[task.ID]
	r.Execution = "legacy-one"
	r.FinishedExecution = "legacy-one"
	if err = m.write(); err != nil {
		t.Fatal(err)
	}
	s := f.states[task.ID]
	s.ExecutionKey = "native-two"
	s.Task.Status = "working"
	f.states[task.ID] = s
	m = openFixture(t, root, "codex", f)
	if m.InteractionStates(f.WorkStates())[0].TaskEnded {
		t.Fatal("previous report matched current generation")
	}
	if _, err = m.SendTask(api.WithWorkerQuestionAnswer(t.Context(), "original-call"), api.TaskMessage{ID: task.ID, RequestID: "original-answer", Prompt: "answer"}); err != nil {
		t.Fatal("legacy finish blocked current answer", err)
	}
	// A late old snapshot may carry the old alias even after migration. It cannot
	// replace the current generation or emit an old completion report.
	s.ExecutionKey = "native-one"
	s.LegacyExecutionKey = "legacy-one"
	s.Task.Status = "completed"
	f.states[task.ID] = s
	if err = m.refresh(); err != nil {
		t.Fatal(err)
	}
	if m.InteractionStates(f.WorkStates())[0].TaskEnded {
		t.Fatal("late retired snapshot closed current questions")
	}
	if m.state.Records[task.ID].View.Status != "working" {
		t.Fatal("old legacy completion rewound task")
	}
}
