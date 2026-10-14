package codex

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
)

func TestWorkModelResolutionAndPinnedContinuation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		botMode     string
		config      any
		custom      api.WorkExecutionSettings
		want        api.WorkExecutionSettings
		wantSandbox string
		fail        bool
	}{
		{name: "runtime", config: map[string]any{"config": map[string]any{"model": "runtime-astra", "model_reasoning_effort": "high", "service_tier": "priority"}}, want: api.WorkExecutionSettings{Model: "runtime-astra", Effort: "high", ServiceTier: "priority"}, wantSandbox: "workspace-write"},
		{name: "runtime missing model", config: map[string]any{"config": map[string]any{"model": nil, "model_reasoning_effort": "high"}}, want: api.WorkExecutionSettings{Model: "bot-luna", Effort: "low", ServiceTier: "fast"}, wantSandbox: "workspace-write"},
		{name: "custom bypasses unavailable config", config: &NativeError{Code: -32601, Message: "unavailable"}, custom: api.WorkExecutionSettings{Model: "work-sol", Effort: "medium"}, want: api.WorkExecutionSettings{Model: "work-sol", Effort: "medium"}, wantSandbox: "workspace-write"},
		{name: "Worker full access with Bot auto", botMode: "auto", custom: api.WorkExecutionSettings{Model: "work-sol", ApprovalMode: "full-access"}, want: api.WorkExecutionSettings{Model: "work-sol", ApprovalMode: "full-access"}, wantSandbox: "danger-full-access"},
		{name: "failed read is not missing model", config: &NativeError{Code: -32601, Message: "unavailable"}, fail: true},
		{name: "missing model field is not unconfigured", config: map[string]any{"config": map[string]any{}}, fail: true},
		{name: "malformed read is not missing model", config: map[string]any{}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			botMode := tc.botMode
			if botMode == "" {
				botMode = "full-access"
			}
			bot := api.ExecutionSettings{Model: "bot-luna", Effort: "low", ServiceTier: "fast", ApprovalMode: botMode}
			if !tc.fail && tc.want.ApprovalMode == "" {
				tc.want.ApprovalMode = "auto"
			}
			s, f, d, m := taskPair(t)
			s.mu.Lock()
			s.opts.Execution = bot
			s.opts.WorkExecution = tc.custom
			s.mu.Unlock()
			f.mu.Lock()
			d.config = tc.config
			f.mu.Unlock()
			sendSynthetic(t, s, "work-model-parent")
			v, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "work-model-start", Title: "Fixture", Prompt: "Synthetic work"})
			if tc.fail {
				f.mu.Lock()
				starts := d.starts
				f.mu.Unlock()
				if err == nil || starts != 0 {
					t.Fatal("failed resolution dispatched work", err, starts)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			record := s.binding.Tasks[v.ID]
			got := *record.Execution
			s.mu.Unlock()
			if got != tc.want {
				t.Fatalf("work = %+v; want %+v", got, tc.want)
			}
			f.mu.Lock()
			p := d.threadParams
			firstTurn := d.turnParams
			root := f.lastParams
			f.mu.Unlock()
			if p["sandbox"] != tc.wantSandbox || p["model"] != tc.want.Model || string(root["model"]) != `"bot-luna"` {
				t.Fatal("models or permission boundary crossed", p)
			}
			firstKind := "workspaceWrite"
			if tc.want.ApprovalMode == "full-access" {
				firstKind = "dangerFullAccess"
			}
			wantPolicy := map[bool]string{true: "never", false: "on-request"}[tc.want.ApprovalMode == "full-access"]
			wantReviewer := map[bool]string{true: "user", false: "auto_review"}[tc.want.ApprovalMode == "full-access"]
			if firstTurn["sandboxPolicy"].(map[string]any)["type"] != firstKind || firstTurn["approvalPolicy"] != wantPolicy || firstTurn["approvalsReviewer"] != wantReviewer {
				t.Fatal("first native Turn did not use the task permission", firstTurn)
			}
			if tc.want.ApprovalMode == "full-access" {
				if p["approvalPolicy"] != "never" || p["approvalsReviewer"] != "user" {
					t.Fatal("native full-access approval parameters missing", p)
				}
			} else if p["approvalPolicy"] != "on-request" || p["approvalsReviewer"] != "auto_review" {
				t.Fatal("worker inherited Bot full-access", p)
			}
			loaded := NewSession(s.opts)
			defer loaded.cancelLife()
			if loaded.loadErr != nil || *loaded.binding.Tasks[v.ID].Execution != tc.want {
				t.Fatal("work model lost on restart", loaded.loadErr)
			}
			restored := loaded.workerParams(v.Workspace, botpolicy.WorkerInstructions, loaded.binding.Tasks[v.ID])
			resumedAfterRestart := map[string]any{"config": map[string]any{}}
			turnAfterRestart := map[string]any{}
			loaded.applyWorkExecution(resumedAfterRestart, true, loaded.binding.Tasks[v.ID])
			loaded.applyWorkExecution(turnAfterRestart, false, loaded.binding.Tasks[v.ID])
			if restored["sandbox"] != tc.wantSandbox || restored["approvalPolicy"] != wantPolicy || restored["approvalsReviewer"] != wantReviewer || resumedAfterRestart["sandbox"] != tc.wantSandbox || resumedAfterRestart["approvalPolicy"] != wantPolicy || resumedAfterRestart["approvalsReviewer"] != wantReviewer || turnAfterRestart["sandboxPolicy"].(map[string]any)["type"] != firstKind || turnAfterRestart["approvalPolicy"] != wantPolicy || turnAfterRestart["approvalsReviewer"] != wantReviewer {
				t.Fatal("restored task changed native permission", restored)
			}
			s.mu.Lock()
			s.opts.Execution.Model = "changed-bot"
			s.opts.WorkExecution = api.WorkExecutionSettings{Model: "changed-work"}
			s.mu.Unlock()
			f.mu.Lock()
			d.config = &NativeError{Code: -32601, Message: "runtime unavailable"}
			f.mu.Unlock()
			// Same request and later continuation must not resolve configuration again.
			again, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "work-model-start", Title: "Fixture", Prompt: "Synthetic work"})
			if err != nil || again.ID != v.ID {
				t.Fatal("retry reselected model", err)
			}
			finishTask(s, f, v.ID, "completed")
			if _, err = m.ReadTask(t.Context(), v.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "work-model-followup", Prompt: "Continue synthetic work"}); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			resume, turn := d.resumed, d.turnParams
			f.mu.Unlock()
			if resume["model"] != tc.want.Model || turn["model"] != tc.want.Model || resume["modelProvider"] != "fixture-provider" {
				t.Fatal("continuation changed model", resume, turn)
			}
			if resume["sandbox"] != tc.wantSandbox || resume["approvalPolicy"] != wantPolicy || resume["approvalsReviewer"] != wantReviewer || turn["sandboxPolicy"].(map[string]any)["type"] != firstKind || turn["approvalPolicy"] != wantPolicy || turn["approvalsReviewer"] != wantReviewer {
				t.Fatal("continuation changed pinned permission", resume)
			}
			s.mu.Lock()
			continuedMode := s.binding.Tasks[v.ID].Execution.ApprovalMode
			s.mu.Unlock()
			if continuedMode != tc.want.ApprovalMode {
				t.Fatal("native resume receipt changed task permission", continuedMode)
			}
			if tc.want.ServiceTier == "" && (resume["serviceTier"] != nil || turn["serviceTier"] != nil) {
				t.Fatal("manual standard inherited Bot Fast")
			}
		})
	}
}

func TestLegacyWorkResumesNativeModelBeforePinning(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "legacy-model-parent")
	v := newTask(t, m, "legacy-model-create")
	finishTask(s, f, v.ID, "completed")
	if _, err := m.ReadTask(t.Context(), v.ID); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.binding.Tasks[v.ID].Execution = nil
	s.opts.Execution.Model = "new-bot"
	s.opts.WorkExecution = api.WorkExecutionSettings{Model: "new-work"}
	s.mu.Unlock()
	if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "legacy-model-continue", Prompt: "Continue"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	resume, turn := d.resumed, d.turnParams
	f.mu.Unlock()
	if _, present := resume["model"]; present || turn["model"] != "native-default" {
		t.Fatal("legacy task model overwritten", resume, turn)
	}
	s.mu.Lock()
	legacyMode := s.binding.Tasks[v.ID].Execution.ApprovalMode
	s.mu.Unlock()
	if legacyMode != "auto" || resume["sandbox"] != "workspace-write" || turn["sandboxPolicy"].(map[string]any)["type"] != "workspaceWrite" {
		t.Fatal("legacy worker inherited new Bot or Worker mode", legacyMode, resume, turn)
	}
}

func TestWorkFallbackUsesNativeResidentModelWithoutBotOverride(t *testing.T) {
	s, f, d, m := taskPair(t)
	if s.residentExecution.Model != "native-default" {
		t.Fatal("resident receipt model not captured")
	}
	sendSynthetic(t, s, "native-bot-fallback-parent")
	newTask(t, m, "native-bot-fallback-start")
	f.mu.Lock()
	p := d.threadParams
	f.mu.Unlock()
	if p["model"] != "native-default" {
		t.Fatal("fallback did not use the resident Bot model", p)
	}
}

func TestRuntimeDefaultDoesNotUseBotOrWorkOverrides(t *testing.T) {
	s, f, d, _ := taskPair(t)
	s.opts.Execution.Model = "bot-override"
	s.opts.WorkExecution.Model = "work-override"
	f.mu.Lock()
	d.config = map[string]any{"config": map[string]any{"model": "configured-runtime", "model_reasoning_effort": "xhigh", "service_tier": "priority"}}
	f.mu.Unlock()
	got, err := s.RuntimeDefault(t.Context())
	want := api.WorkExecutionSettings{Model: "configured-runtime", Effort: "xhigh", ServiceTier: "priority"}
	if err != nil || got != want {
		t.Fatalf("default = %+v, %v", got, err)
	}
	f.mu.Lock()
	d.config = map[string]any{"config": map[string]any{"model": nil}}
	f.mu.Unlock()
	got, err = s.RuntimeDefault(t.Context())
	if err != nil || got.Model != "" {
		t.Fatal("unconfigured model was replaced by an override", got, err)
	}
}
