package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botmemory"
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/care"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

// Opt-in inference uses real installed runtimes/account credentials, private
// product bindings and a disposable UI. It never logs private conversations.
func runCompact() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	provider := os.Getenv("BOT_ACCEPTANCE_RUNTIME")
	if provider == "" {
		provider = "codex"
	}
	if provider != "codex" && provider != "caelis" {
		return errors.New("invalid acceptance runtime")
	}
	base := os.Getenv("BOT_ACCEPTANCE_EVIDENCE")
	if base != "" {
		if e := os.MkdirAll(base, 0700); e != nil {
			return e
		}
	}
	dir, e := os.MkdirTemp(base, "compact-"+provider+"-")
	if e != nil {
		return e
	}
	if base == "" {
		defer os.RemoveAll(dir)
	}
	var gestures atomic.Int32
	resident, e := bot.NewForRuntime(filepath.Join(dir, "bot.json"), provider, func(string) error { gestures.Add(1); return nil })
	if e != nil {
		return e
	}
	defer resident.Close()
	memory, e := botmemory.Open(ctx, filepath.Join(dir, "personal"), resident.State().ID)
	if e != nil {
		return e
	}
	defer memory.Close()
	if e = resident.ConfigurePersonal(memory); e != nil {
		return e
	}
	// Presence is synthetic to keep condition tests deterministic. No test event
	// is published and all saved event predicates are false.
	yes := true
	if e = resident.ConfigureCare(func() care.Sample { return care.Sample{Presence: care.Presence{Awake: true, Unlocked: &yes}} }); e != nil {
		return e
	}
	helper := os.Getenv("BOT_ACCEPTANCE_DESKTOP_HELPER")
	if helper != "" {
		driver := desktopcontrol.New(helper, filepath.Join(dir, "desktop"))
		defer driver.Close()
		resident.ConfigureDesktopControl(driver)
	}
	bridge, e := bot.Serve(resident)
	if e != nil {
		return e
	}
	defer bridge.Close()
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	config := bridge.Config(executable)
	skill, e := botskills.Install(dir)
	if e != nil {
		return e
	}
	config.Instructions = "You are the resident Caelis Bot in an isolated functional acceptance. Complete explicit user requests through Bot-owned tools. Do not use native subagents or change runtime/account settings. Only read the supplied Bot skill files, and use desktop tools solely for the disposable Caelis Desktop Control Fixture. " + botskills.Instructions(skill)
	config.PrepareTurn = func(context.Context) error { resident.BeginDesktopTurn(); return nil }
	config.FinishTurn = resident.StopDesktopTurn
	model := os.Getenv("BOT_ACCEPTANCE_MODEL")
	effort := os.Getenv("BOT_ACCEPTANCE_EFFORT")
	newEngine := func() (api.Engine, error) {
		var engine api.Engine
		if provider == "codex" {
			engine = codex.NewSession(codex.SessionOptions{Binary: os.Getenv("CODEX_BIN"), Directory: filepath.Join(dir, "work"), StateFile: filepath.Join(dir, "binding.json"), Execution: api.ExecutionSettings{Model: model, Effort: effort}, WorkExecution: api.WorkExecutionSettings{Model: model, Effort: effort}})
		} else {
			engine = caelis.New(caelis.Options{Directory: filepath.Join(dir, "binding"), Settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: os.Getenv("CAELIS_BIN"), CaelisStore: os.Getenv("BOT_ACCEPTANCE_CAELIS_STORE")}, Execution: api.ExecutionSettings{Model: model, Effort: effort}, WorkExecution: api.WorkExecutionSettings{Model: model, Effort: effort}})
		}
		if e := engine.(api.BotToolBinder).ConfigureBotTools(config); e != nil {
			return nil, e
		}
		return engine, nil
	}
	engine, e := newEngine()
	if e != nil {
		return e
	}
	if e = engine.Connect(ctx); e != nil {
		return e
	}
	if e = compactSubmit(ctx, engine, "compact-continuity", "Only reply COMPACT_CONTINUITY. Do not call tools."); e != nil {
		engine.Close(ctx)
		return e
	}
	if e = engine.Close(ctx); e != nil {
		return e
	}
	engine, e = newEngine()
	if e != nil {
		return e
	}
	defer engine.Close(context.Background())
	manager, e := tasks.Open(filepath.Join(dir, "tasks.json"), filepath.Join(dir, "Tasks"), provider, engine.(api.WorkRuntime), engine.(api.ReportSubmitter), engine.Snapshot)
	if e != nil {
		return e
	}
	if e = resident.ConfigureTasks(manager, manager); e != nil {
		return e
	}
	if e = engine.Connect(ctx); e != nil {
		return e
	}
	resumed := false
	for _, i := range engine.Snapshot().Items {
		if strings.Contains(i.Text, "COMPACT_CONTINUITY") {
			resumed = true
		}
	}
	if !resumed {
		return errors.New("resume history lost")
	}
	resident.Start(engine)
	evidence := map[string]any{"runtime": provider, "model": model, "privateBindings": true, "resumedHistory": true, "presenceFixture": true, "tools": len(resident.Definitions())}
	run := func(id, prompt string) error {
		fmt.Fprintln(os.Stderr, "compact stage:", provider, id)
		started := time.Now()
		err := compactSubmit(ctx, engine, id, prompt)
		evidence[id+"Seconds"] = time.Since(started).Seconds()
		if err != nil {
			b, _ := json.Marshal(engine.Snapshot())
			os.WriteFile(filepath.Join(dir, "failed-synthetic-snapshot.json"), b, 0600)
		}
		return err
	}
	if e = run("memory-schedule", `For this isolated test, read fresh local time, nod once, remember the synthetic preference "COMPACT_ACCEPTANCE concise" with requestId compact-memory-once, then recall it. Discover the available event sources and test this pure condition against activeSeconds=7300,idleSeconds=20: event.activeSeconds >= 7200 && event.idleSeconds < 120, source desktop.usage, current local timezone. Save two arrangements: a calendar reminder id compact-calendar, label Compact calendar, prompt Only reply compact-reminder, every 60 minutes; and an event rule id compact-event, label Compact event, prompt Only reply compact-event, source clock.minute, condition false. Use the current timezone. List both saved arrangements. Do not publish events or use shell, files (except the provided skill), network, native subagents or external services. Report verified registrations briefly.`); e != nil {
		return e
	}
	if gestures.Load() != 1 || len(resident.State().Schedules) != 1 {
		return fmt.Errorf("model did not complete gestures/calendar: %d/%d", gestures.Load(), len(resident.State().Schedules))
	}
	recalled, e := memory.ReadMemory(ctx, "COMPACT_ACCEPTANCE")
	if e != nil || len(recalled.Evidence) != 1 {
		return errors.New("actual memory receipt missing")
	}
	r := resident.CallTool(ctx, "bot_schedule", json.RawMessage(`{"request":{"type":"list","kind":"event"}}`))
	b, _ := json.Marshal(r.StructuredContent)
	if r.IsError || !strings.Contains(string(b), "event:compact-event") {
		return fmt.Errorf("actual event registration missing: %s", b)
	}
	evidence["memoryCalendarEventPureTest"] = true
	// Native Caelis workspace keys survive this disposable application binding.
	// Each acceptance has fresh IDs; each operation retains its ID within the run.
	workerRequest := "compact-worker-" + filepath.Base(dir)
	followupRequest := "compact-followup-" + filepath.Base(dir)
	if e = run("owned-worker", fmt.Sprintf(`Start exactly one independent local Bot worker using requestId %s, title Compact worker, assignment: "Run exactly one shell command that writes COMPACT_WORKER_OK followed by a newline to worker.txt in your current workspace. Create no other files. Report done." The worker may use its native command tool. The resident must not use a shell or native subagents. Use the Bot delegation tool, retain its actual task receipt, briefly report acceptance and finish your turn; do not poll.`, workerRequest)); e != nil {
		return e
	}
	owned := manager.ListTasks()
	if len(owned) != 1 {
		return fmt.Errorf("expected one owned task, got %d", len(owned))
	}
	waitTask := func() error {
		for {
			task, err := manager.ReadTask(ctx, owned[0].ID)
			if err != nil {
				return err
			}
			if task.Status == "completed" {
				data, err := os.ReadFile(filepath.Join(task.Workspace, "worker.txt"))
				if err != nil || string(data) != "COMPACT_WORKER_OK\n" {
					return errors.New("worker artifact bytes mismatch")
				}
				entries, err := os.ReadDir(task.Workspace)
				if err != nil || len(entries) != 1 {
					return errors.New("worker wrote unrelated files")
				}
				return nil
			}
			if task.Status == "failed" || strings.Contains(task.Status, "approval") {
				return fmt.Errorf("worker blocked: %s", task.Status)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	if e = waitTask(); e != nil {
		return e
	}
	if e = run("task-read-continue", fmt.Sprintf(`Read the original Bot task by its requestId %s. Lock and then unlock its watchlist card, unpin and pin it; clear unlocked cards without stopping the task. Continue the same task with requestId %s, asking: "Read worker.txt and report its exact text. Do not write files." Do not start another task or use resident shell/native subagents. Report accepted follow-up and finish; do not poll.`, workerRequest, followupRequest)); e != nil {
		return e
	}
	if e = waitTask(); e != nil {
		return e
	}
	if len(manager.ListTasks()) != 1 {
		return errors.New("continuation created duplicate task")
	}
	if _, e = manager.ReadTaskRequest(ctx, followupRequest); e != nil {
		return fmt.Errorf("follow-up original request lookup: %w", e)
	}
	evidence["ownedWorkerExactBytesAndContinuation"] = true
	if e = run("compact-cleanup", `Remove exactly the two saved arrangements calendar:compact-calendar and event:compact-event. Correct the synthetic COMPACT_ACCEPTANCE memory evidence to "COMPACT_ACCEPTANCE brief", verify it, then forget that evidence. Use stable mutation request IDs. Do not delete Notebook files, unrelated memory or task workspaces. Nod once when done.`); e != nil {
		return e
	}
	recalled, e = memory.ReadMemory(ctx, "COMPACT_ACCEPTANCE")
	if e != nil || len(recalled.Evidence) != 0 || len(resident.State().Schedules) != 0 {
		return errors.New("arrangement/memory cleanup incomplete")
	}
	evidence["typedRemoveMemoryCorrectForget"] = true
	if helper != "" {
		desktopTitle := os.Getenv("BOT_ACCEPTANCE_DESKTOP_TITLE")
		if desktopTitle == "" {
			desktopTitle = "Caelis Desktop Control Fixture"
		}
		if e = run("compact-desktop", fmt.Sprintf(`Use only the resident Desktop World tools and the supplied desktop skill. Find the existing disposable window titled %q in the Caelis Desktop Control Fixture application; authorize that exact application for this test. Set its "Verification text" field to COMPACT_DESKTOP_OK, then invoke "Submit once" exactly once. Verify Submitted: 1 and the field content using bounded metadata/text and a cursor delta. Query the original action receipt with the desktop result tool. Do not operate other applications, create another fixture, use shell/AppleScript, or replay a partial/unknown action. Finish with the verified result.`, desktopTitle)); e != nil {
			return e
		}
		resultPath := os.Getenv("BOT_ACCEPTANCE_DESKTOP_RESULT")
		data, e := os.ReadFile(resultPath)
		if e != nil {
			return e
		}
		var result struct {
			Submissions int    `json:"submissions"`
			Text        string `json:"text"`
		}
		if json.Unmarshal(data, &result) != nil || result.Submissions != 1 || result.Text != "COMPACT_DESKTOP_OK" {
			return errors.New("actual native UI result mismatch")
		}
		evidence["nativeDesktopExactOnce"] = true
	}
	// Register through the model's real user turn. Caelis requires the native
	// background grant; directly inserting host state cannot replace it.
	before := gestures.Load()
	if e = run("wake-registration", `Read fresh local time, then save exactly one one-off Bot reminder with id compact-wake, label Compact wake, scheduled 90 seconds after that fresh time. Its exact prompt must be: Use only bot_gesture with action attention, then reply COMPACT_WAKE_DONE. Do not access files, shell, network or other tools. Use the clock's actual named timezone. Confirm its native receipt and finish your turn. Do not sleep or poll.`); e != nil {
		return e
	}
	for {
		wake := resident.State().Wake
		if wake != nil && wake.Status == "accepted" {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if e = compactWait(ctx, engine); e != nil {
		return e
	}
	if gestures.Load() != before+1 {
		return errors.New("real scheduled activation missed character callback")
	}
	resident.Remove("compact-wake")
	evidence["scheduledModelActivation"] = true
	// Count authoritative native callbacks, never mentions in generated prose.
	// Only names/counts leave the test-owned binding; arguments stay private.
	if provider == "caelis" {
		raw, err := os.ReadFile(filepath.Join(dir, "binding", "application.json"))
		if err != nil {
			return err
		}
		var binding struct {
			Calls map[string]struct {
				Call struct {
					Name string `json:"name"`
				} `json:"call"`
			} `json:"calls"`
		}
		if err := json.Unmarshal(raw, &binding); err != nil {
			return err
		}
		calls := map[string]int{}
		for _, record := range binding.Calls {
			calls[record.Call.Name]++
		}
		evidence["nativeToolCallbacks"] = calls
		evidence["toolCallMetricSource"] = "persisted native application calls"
	}
	data, _ := json.MarshalIndent(evidence, "", "  ")
	if base != "" {
		os.WriteFile(filepath.Join(dir, "summary.json"), data, 0600)
	}
	fmt.Println(string(data))
	return nil
}
func compactWait(ctx context.Context, s api.Engine) error {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		v := s.Snapshot()
		for _, a := range v.Approvals {
			if a.Status == "pending" {
				return fmt.Errorf("native manual approval required: %s", a.Title)
			}
		}
		if v.Phase == "completed" {
			return nil
		}
		if v.Connection == "offline" || v.Phase == "failed" {
			return fmt.Errorf("backend phase %s: %s", v.Phase, v.Message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
func compactSubmit(ctx context.Context, s api.Engine, id, text string) error {
	// Connect may return before the first native projection is ready. Follow
	// the same canSend gate as the product UI, without replaying a submission.
	ready, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for !s.Snapshot().CanSend {
		select {
		case <-ready.Done():
			return fmt.Errorf("native not ready for user input: phase=%s", s.Snapshot().Phase)
		case <-time.After(100 * time.Millisecond):
		}
	}
	receipt, e := s.Submit(ctx, api.Submission{ID: id, Text: text}, nil)
	if e != nil {
		return e
	}
	if receipt.Outcome != "accepted" {
		return fmt.Errorf("submission outcome=%s: %s", receipt.Outcome, receipt.Message)
	}
	return compactWait(ctx, s)
}
