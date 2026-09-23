package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
)

// Explicitly opt in with an isolated, already configured Host. Model credentials
// remain in Caelis; this client uses only the public Control API. The shared Host
// is never stopped by this test. Only synthetic workspace commands are approved.
func TestConfiguredModelIntegration(t *testing.T) {
	store := os.Getenv("CAELIS_BOT_LIVE_STORE")
	if store == "" {
		t.Skip("set CAELIS_BOT_LIVE_STORE to an isolated configured Host")
	}
	modelName := os.Getenv("CAELIS_BOT_LIVE_MODEL")
	if modelName == "" {
		t.Fatal("CAELIS_BOT_LIVE_MODEL is required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	root := t.TempDir()
	settings := api.RuntimeSettings{Runtime: "caelis", CaelisStore: store}
	var gestures, clocks, reminderSaves atomic.Int32
	resident, err := bot.New(filepath.Join(root, "desktop.json"), func(action string) error {
		if action != "nod" {
			return fmt.Errorf("unexpected synthetic gesture")
		}
		gestures.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resident.Close()
	s := New(Options{Directory: filepath.Join(root, "binding"), Settings: settings})
	err = s.BindDesktop(api.DesktopEffects{Execute: func(action string, args json.RawMessage) (json.RawMessage, error) {
		if action == "clock" {
			clocks.Add(1)
		}
		if action == "reminders" {
			var v struct{ Operation string }
			_ = json.Unmarshal(args, &v)
			if v.Operation == "save" {
				reminderSaves.Add(1)
			}
		}
		return resident.ExecuteDesktop(action, args)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if e := s.Close(closeCtx); e != nil {
			t.Error("client cleanup:", e)
		}
	}()
	if err = s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	approved := map[string]bool{}
	allowedCommands := map[string]bool{
		"printf CAELIS_MIMO_WORK_A > acceptance.txt; cat acceptance.txt": true,
		"printf CAELIS_MIMO_WORK_B > acceptance.txt; cat acceptance.txt": true,
	}
	await := func(stage string, pred func(api.Snapshot) bool) api.Snapshot {
		t.Helper()
		stageCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
		defer stop()
		for {
			snap := s.Snapshot()
			for _, a := range snap.Approvals {
				if approved[a.ID] {
					continue
				}
				var input struct {
					Command string `json:"command"`
				}
				if json.Unmarshal([]byte(a.Details), &input) != nil || !allowedCommands[input.Command] {
					t.Fatal(stage + ": unexpected approval; no decision sent")
				}
				option := ""
				for _, c := range a.Choices {
					if c.Scope == "allow_once" {
						option = c.ID
					}
				}
				if option == "" {
					t.Fatal(stage + ": native allow_once option absent")
				}
				if e := s.Decide(stageCtx, api.Decision{ID: a.ID, Choice: option}); e != nil {
					t.Fatal(stage+": approval failed:", e)
				}
				approved[a.ID] = true
				t.Log(stage + ": approved one exact synthetic command")
			}
			if pred(snap) {
				t.Log(stage + ": passed")
				return snap
			}
			if snap.Phase == "failed" {
				t.Fatal(stage + ": native turn failed")
			}
			if _, e := s.WaitSnapshot(stageCtx, snap.Revision); e != nil {
				s.mu.Lock()
				works, completions := len(s.works), len(s.completions)
				s.mu.Unlock()
				t.Fatalf("%s: did not reach expected state (phase=%s works=%d completions=%d approvals=%d)", stage, snap.Phase, works, completions, len(approved))
			}
		}
	}
	hasText := func(v api.Snapshot, text string) bool {
		for _, i := range v.Items {
			if i.Kind == "assistant" && strings.Contains(i.Text, text) {
				return true
			}
		}
		return false
	}
	send := func(id, text string) {
		t.Helper()
		v, e := s.Submit(ctx, api.Submission{ID: id, Text: text}, nil)
		if e != nil || v.Outcome != "accepted" {
			t.Fatalf("%s: send not accepted (%s): %v", id, v.Outcome, e)
		}
	}
	await("connected", func(v api.Snapshot) bool { return v.CanSend })
	models, err := s.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var selected *api.ModelOption
	for _, m := range models {
		if strings.Contains(m.Model, modelName) {
			if selected == nil || m.Default {
				x := m
				selected = &x
			}
			if m.Default {
				break
			}
		}
	}
	if selected == nil {
		t.Fatal("requested model missing from authenticated catalog")
	}
	if err = s.ChangeExecution(ctx, api.ExecutionSettings{Model: selected.Model, Effort: selected.DefaultEffort, ApprovalMode: "workspace-write"}, func() error { return nil }); err != nil {
		t.Fatal("model selection:", err)
	}
	prefs, err := s.CurrentExecutionSettings(ctx)
	if err != nil || !strings.Contains(prefs.Model, modelName) {
		t.Fatal("selected model not authoritative")
	}
	t.Log("requested model selected:", modelName)
	send("live-chat", "只回复 CAELIS_MIMO_CHAT_OK，不要调用工具。")
	await("real model chat", func(v api.Snapshot) bool { return v.CanSend && hasText(v, "CAELIS_MIMO_CHAT_OK") })
	send("live-delegate", `这是隔离联调任务。请用 CreateWork 创建恰好两个独立工作 A 和 B，你自己不要执行命令。给每个工作完整指令：在自己的工作目录中只运行一次下列精确命令，使用 RunCommand 的 sandbox_permissions=require_escalated 请求一次审批；获批后运行并返回输出，不要重试、不要读取任何已有文件，不要访问网络或创建更多工作。A 的命令是：printf CAELIS_MIMO_WORK_A > acceptance.txt; cat acceptance.txt 。B 的命令是：printf CAELIS_MIMO_WORK_B > acceptance.txt; cat acceptance.txt 。创建后先简短回复，等完成后由自动报告汇总结果。`)
	await("two real model workers and reports", func(v api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.works) > 2 {
			t.Fatal("model created more than two workers")
		}
		if !v.CanSend || len(s.works) != 2 || len(s.completions) != 2 || len(approved) != 2 {
			return false
		}
		foundA, foundB := false, false
		for _, w := range s.works {
			if w.Status != "succeeded" {
				return false
			}
			foundA = foundA || strings.Contains(value(w.Result), "CAELIS_MIMO_WORK_A")
			foundB = foundB || strings.Contains(value(w.Result), "CAELIS_MIMO_WORK_B")
		}
		for _, c := range s.completions {
			if c.ReportState != "admitted" {
				return false
			}
		}
		return foundA && foundB && s.works[0].WorkspaceKey != s.works[1].WorkspaceKey
	})
	s.mu.Lock()
	var first wire.BotWork
	for _, w := range s.works {
		if strings.Contains(value(w.Result), "CAELIS_MIMO_WORK_A") {
			first = w
		}
	}
	s.mu.Unlock()
	if err = s.AcknowledgePresentation(ctx, s.Snapshot()); err != nil {
		t.Fatal("presentation acknowledgement:", err)
	}
	send("live-continue", "请使用 ContinueWork 继续之前的工作 A，沿用其工作区，不要创建新工作。给它的指令：只用 Read 读取自己工作区的 acceptance.txt，返回文件内容并加上 CAELIS_MIMO_CONTINUED_OK。不要运行命令、不要修改文件或访问网络。")
	await("real model continuation", func(v api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !v.CanSend || len(s.works) != 2 || len(s.completions) != 3 {
			return false
		}
		for _, c := range s.completions {
			if c.ReportState != "admitted" {
				return false
			}
		}
		for _, w := range s.works {
			if w.Id == first.Id {
				return w.WorkspaceKey == first.WorkspaceKey && w.Execution.RunId != first.Execution.RunId && w.Status == "succeeded" && strings.Contains(value(w.Result), "CAELIS_MIMO_CONTINUED_OK")
			}
		}
		return false
	})
	send("live-desktop", "请调用 DesktopClock 查看当前时间，再调用 DesktopGesture 执行 nod 一次。不要调用其他工具。最后回复 CAELIS_MIMO_DESKTOP_OK。")
	await("real model desktop tools", func(v api.Snapshot) bool {
		return v.CanSend && clocks.Load() > 0 && gestures.Load() == 1 && hasText(v, "CAELIS_MIMO_DESKTOP_OK")
	})
	due := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	send("live-reminder", fmt.Sprintf("请只调用一次 DesktopReminders 保存一个一次性提醒，operation=save，id=mimo-acceptance，label=联调提醒，at=%s，prompt=只回复 CAELIS_MIMO_REMINDER_OK，不要调用工具。不要创建其他提醒，保存后只回复已设置。", due))
	await("real model reminder wake", func(v api.Snapshot) bool {
		if !v.CanSend || reminderSaves.Load() != 1 || !hasText(v, "CAELIS_MIMO_REMINDER_OK") {
			return false
		}
		var occurrences []wire.BotReminderFire
		if e := s.client.json(ctx, "GET", s.botPath("/client/reminder-occurrences"), nil, &occurrences, "", ""); e != nil {
			return false
		}
		return len(occurrences) == 1 && occurrences[0].State == "admitted" && occurrences[0].Execution.TurnId == v.CurrentTurn
	})
	if gestures.Load() != 1 || reminderSaves.Load() != 1 {
		t.Fatal("native effect repeated")
	}
	if err = s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	d, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	host, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = initialize(ctx, host); err != nil {
		t.Fatal("client exit affected shared Host")
	}
	t.Log("real model acceptance passed; isolated Host preserved; no credentials printed or copied")
}
