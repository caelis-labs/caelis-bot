package bot

import (
	"errors"
	"fmt"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// reportEventFault is called under step. The store stays faulted after an
// ambiguous write, even if the writer eventually confirms its own disk result.
func (r *Runtime) reportEventFault() {
	log := r.diagnostics
	if log == nil {
		return
	}
	err := r.eventsErr
	if r.events != nil {
		err = r.events.StorageFault()
	}
	if err == nil {
		return
	}
	r.eventWarning.Do(func() {
		var failure *localstate.WriteError
		if !errors.As(err, &failure) {
			log.Write(diagnosticlog.Record{Component: "eventplan", Code: "storage_unavailable"})
			return
		}
		write := localstate.DescribeFailure(failure)
		outcome := eventWriteOutcome(write)
		if failure.Completion != nil {
			outcome = "writer_in_flight"
		}
		log.Write(eventWriteDiagnostic("storage_write_failed", write, outcome))
		if failure.Completion != nil {
			go func() {
				result := <-failure.Completion
				write := localstate.DescribeFailure(result.Err)
				write.ElapsedMS = result.Elapsed.Milliseconds()
				log.Write(eventWriteDiagnostic("storage_write_finished_after_timeout", write, eventWriteOutcome(write)))
			}()
		}
	})
}

func eventWriteOutcome(write localstate.Failure) string {
	switch write.Stage {
	case "encode", "queue":
		return "not_started"
	case "parent_sync":
		return "renamed_durability_unconfirmed"
	case "":
		if write.Kind == "" {
			return "committed"
		}
	}
	return "failed"
}

func eventWriteDiagnostic(code string, write localstate.Failure, outcome string) diagnosticlog.Record {
	return diagnosticlog.Record{
		Component: "eventplan", Code: code, Phase: write.Stage, ProcessingMS: write.ElapsedMS,
		Reason: fmt.Sprintf("%s elapsed_ms=%d outcome=%s", write.Reason(), write.ElapsedMS, outcome),
	}
}
