package bot

import (
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func warmDream(t *testing.T, used int64) (*Runtime, *dreamEngine, *time.Time, *DreamEnvironment) {
	t.Helper()
	r, e, now := dreamFixture(t)
	env := &DreamEnvironment{Available: true}
	r.ConfigureDreamEnvironment(func() DreamEnvironment { return *env })
	*now = now.Add(time.Second)
	e.conversation.Usage = api.ContextUsage{Used: used, Window: 100000, ModelAt: *now}
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r, e, now, env
}

func dreamSeconds(t *testing.T, r *Runtime, now *time.Time, seconds int) {
	t.Helper()
	for range seconds {
		*now = now.Add(time.Second)
		r.step.Lock()
		err := r.tickDream(t.Context(), true)
		r.step.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDreamContextThresholds(t *testing.T) {
	for _, c := range []struct {
		used int64
		wait int
		want bool
	}{
		{29999, 600, false}, {30000, 299, false}, {30000, 300, true},
		{69999, 299, false}, {70000, 89, false}, {70000, 90, true},
	} {
		r, e, now, _ := warmDream(t, c.used)
		dreamSeconds(t, r, now, c.wait)
		if (len(e.dreams) == 1) != c.want {
			t.Fatalf("used=%d wait=%d: %s", c.used, c.wait, r.dream.policy.reason)
		}
	}
}

func TestDreamSleepAndDiscontinuitiesConsumeOpportunity(t *testing.T) {
	for _, kind := range []string{"sleep", "brief-wake", "lock", "offline", "clock-back", "clock-forward", "restart"} {
		t.Run(kind, func(t *testing.T) {
			r, e, now, env := warmDream(t, 80000)
			dreamSeconds(t, r, now, 30)
			switch kind {
			case "sleep", "clock-forward":
				*now = now.Add(30 * time.Minute)
			case "brief-wake":
				env.Epoch++
			case "lock":
				env.Available = false
			case "offline":
				e.conversation.Observed = false
			case "clock-back":
				*now = now.Add(-time.Minute)
			case "restart":
				r = restartDreamRuntime(t, r, e)
				r.ConfigureDreamEnvironment(func() DreamEnvironment { return *env })
			}
			_ = r.Tick(t.Context())
			env.Available, e.conversation.Observed = true, true
			dreamSeconds(t, r, now, 600)
			if len(e.dreams) != 0 {
				t.Fatal("caught up stale opportunity after", kind)
			}
			// A genuine new model response, not more elapsed idle time, permits it.
			e.conversation.Turn = "fresh-turn"
			*now = now.Add(time.Second)
			e.conversation.Usage.ModelAt = *now
			_ = r.Tick(t.Context())
			dreamSeconds(t, r, now, 90)
			if len(e.dreams) != 1 {
				t.Fatal("fresh conversation cannot dream", r.dream.policy.reason)
			}
		})
	}
}

func TestDreamUnknownUsageLongToolAndDeadline(t *testing.T) {
	for _, kind := range []string{"missing", "window-missing", "long-tool", "future", "reserve"} {
		t.Run(kind, func(t *testing.T) {
			r, e, now, _ := warmDream(t, 80000)
			switch kind {
			case "missing":
				e.conversation.Usage = api.ContextUsage{}
			case "window-missing":
				e.conversation.Usage.Window = 0
			case "future":
				e.conversation.Usage.ModelAt = now.Add(time.Hour)
			case "reserve":
				e.conversation.Usage.ModelAt = now.Add(-11 * time.Minute)
			case "long-tool":
				e.conversation.Idle = false
				dreamSeconds(t, r, now, 13*60)
				e.conversation.Idle = true
			}
			dreamSeconds(t, r, now, 300)
			if len(e.dreams) != 0 {
				t.Fatal("unexpected cold/unknown Dream", kind)
			}
		})
	}
}

func TestDreamGrowthCooldownDraftAndCompaction(t *testing.T) {
	for _, kind := range []string{"growth", "cooldown", "draft", "compaction", "model-window"} {
		t.Run(kind, func(t *testing.T) {
			r, e, now, env := warmDream(t, 80000)
			switch kind {
			case "growth":
				r.dream.policy.baseline = 75000
			case "cooldown":
				r.dream.state.LastAttempt = *now
			case "compaction":
				e.conversation.Usage.Used = 71000
			case "model-window":
				e.conversation.Usage.Window = 110000
			}
			for range 180 {
				if kind == "draft" {
					env.DraftRevision++
				}
				dreamSeconds(t, r, now, 1)
			}
			if len(e.dreams) != 0 {
				t.Fatal("admission ignored", kind, r.dream.policy.reason)
			}
		})
	}
}

func TestDreamCooldownAndAttemptSurviveRestart(t *testing.T) {
	r, e, now, env := warmDream(t, 80000)
	dreamSeconds(t, r, now, 90)
	e.conversation.Status, e.conversation.Idle = "failed", true
	_ = r.Tick(t.Context())
	r = restartDreamRuntime(t, r, e)
	r.ConfigureDreamEnvironment(func() DreamEnvironment { return *env })
	e.conversation.Turn = "new-content"
	*now = now.Add(time.Second)
	e.conversation.Usage = api.ContextUsage{Used: 92000, Window: 100000, ModelAt: *now}
	dreamSeconds(t, r, now, 300)
	if len(e.dreams) != 1 || r.dream.policy.reason != "cooldown" {
		t.Fatal("cooldown lost on restart", r.dream.policy.reason)
	}
	dreamSeconds(t, r, now, 30*60)
	if len(e.dreams) != 1 {
		t.Fatal("expired opportunity dispatched at cooldown boundary")
	}
}

func TestDreamCompactionRegrowthAcrossRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "continuous", true: "restart"}[restart], func(t *testing.T) {
			r, e, now, env := warmDream(t, 80000)
			dreamSeconds(t, r, now, 90)
			e.conversation.Status, e.conversation.Idle = "failed", true
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			// The prior attempt durably recorded 80k. Native compaction later
			// lowers the context to 40k without creating another Dream attempt.
			dreamSeconds(t, r, now, 30*60)
			*now = now.Add(time.Second)
			e.conversation.Turn = "after-compaction"
			e.conversation.Usage = api.ContextUsage{Used: 40000, Window: 100000, ModelAt: *now}
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if restart {
				r = restartDreamRuntime(t, r, e)
				r.ConfigureDreamEnvironment(func() DreamEnvironment { return *env })
				// Restored facts alone must not regain cache freshness.
				dreamSeconds(t, r, now, 300)
				if len(e.dreams) != 1 || r.dream.policy.reason != "fresh_activity_required" {
					t.Fatal("restart reused stale model evidence", r.dream.policy.reason)
				}
			}
			// The first fresh gauge after restart is still below the persisted
			// baseline. Subsequent real growth must use the compacted baseline.
			*now = now.Add(time.Second)
			e.conversation.Turn = "fresh-after-compaction"
			e.conversation.Usage.ModelAt = *now
			dreamSeconds(t, r, now, 300)
			if len(e.dreams) != 1 || r.dream.policy.reason != "growth_small" {
				t.Fatal("compaction alone admitted Dream", r.dream.policy.reason)
			}
			*now = now.Add(time.Second)
			e.conversation.Turn = "regrowth"
			e.conversation.Usage.Used, e.conversation.Usage.ModelAt = 60000, *now
			dreamSeconds(t, r, now, 301)
			if len(e.dreams) != 2 {
				t.Fatal("fresh regrowth blocked after compaction", r.dream.policy.reason)
			}
		})
	}
}
