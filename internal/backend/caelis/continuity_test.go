package caelis

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestContinuitySamplesNativeAccountingAndExcludesReplay(t *testing.T) {
	s := New(Options{Directory: t.TempDir()})
	s.tools, s.connected = &api.ToolConnection{}, true
	s.state.Session.SessionId = "resident"
	v := &view{State: wire.SessionState{SessionId: "resident"}, Seen: map[string]bool{}, CommandCaughtUp: true}
	s.state.Views["resident"] = v
	now := time.Now().Round(0)
	emit := func(e wire.Envelope) { s.observeContinuity(v, e, now); applyEnvelope(v, e) }
	event := func(id, turn, kind string) wire.Envelope {
		return wire.Envelope{Kind: kind, EventId: pointer(id), SessionId: pointer("resident"), TurnId: pointer(turn), Scope: pointer("main"), OccurredAt: &now}
	}
	lifecycle := func(id, turn, state string) {
		e := event(id, turn, "caelis/lifecycle")
		e.Lifecycle = &wire.LifecycleEvent{State: state}
		emit(e)
	}
	usage := func(id, turn, basis, input, cache string) wire.Envelope {
		e := event(id, turn, "session/update")
		e.UsageSemantics = wire.UsageSemanticsProviderUsage
		b := json.RawMessage(fmt.Sprintf(`{"sessionUpdate":"usage_update","used":"0","size":"0","_meta":{"caelis":{"sdk":{"model":"fixture","provider":"fixture","usage":{"prompt_tokens":%q,"cached_input_tokens":%q,"cache_read_accounting":%q}}}}}`, input, cache, basis))
		e.Update = &b
		return e
	}
	lifecycle("start", "one", "started")
	e := usage("input", "one", "separate", "4000", "30000")
	emit(e)
	emit(e) // exact projection ID replay
	lifecycle("finish", "one", "completed")
	if o := s.ContinuityObservation(); o.Turns != 1 || o.Inputs[0] != 34000 || !o.InputKnown {
		t.Fatal("full input normalization", o)
	}
	lifecycle("start2", "two", "started")
	emit(usage("input2", "two", "included", "34000", "0"))
	lifecycle("finish2", "two", "completed")
	if o := s.ContinuityObservation(); o.Inputs[1] != 34000 {
		t.Fatal("cold/warm basis differs", o)
	}
	lifecycle("start3", "three", "started")
	bad := usage("unknown", "three", "", "99000", "0")
	emit(bad)
	bad = usage("worker", "three", "included", "99000", "0")
	bad.SessionId = pointer("worker")
	emit(bad)
	bad = usage("review", "three", "included", "99000", "0")
	bad.ApprovalRequestId = pointer("review")
	emit(bad)
	bad = usage("stale", "three", "included", "99000", "0")
	old := now.Add(-time.Hour)
	bad.OccurredAt = &old
	emit(bad)
	lifecycle("finish3", "three", "completed")
	if o := s.ContinuityObservation(); o.InputKnown || o.InputCount != 2 {
		t.Fatal("missing/foreign inputs became evidence", o)
	}
	lifecycle("start4", "four", "started")
	compact := event("compact", "four", "caelis/notice")
	compact.NoticeKind = wire.NoticeKindCompact
	emit(compact)
	emit(compact)
	emit(usage("after-compact", "four", "included", "99000", "0"))
	lifecycle("finish4", "four", "completed")
	if o := s.ContinuityObservation(); o.Compactions != 1 || o.InputKnown {
		t.Fatal("compaction duplicated or input mislabelled", o)
	}
	lifecycle("start5", "five", "started")
	s.state.Operations["original-system"] = journal{TurnID: "five", HostNotice: true}
	lifecycle("finish5", "five", "completed")
	if s.ContinuityObservation().Turns != 4 {
		t.Fatal("internal notification counted")
	}
}
