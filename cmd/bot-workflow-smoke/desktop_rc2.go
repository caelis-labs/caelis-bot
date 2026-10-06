package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
)

// This opt-in acceptance uses the installed native Codex login, but owns a
// fresh Bot store, Codex thread, App Server process and synthetic UI fixture.
// It never attaches to the standard shared App Server socket.
func runDesktopRC2() error {
	base := os.Getenv("BOT_ACCEPTANCE_EVIDENCE")
	helper := os.Getenv("BOT_ACCEPTANCE_DESKTOP_HELPER")
	title := os.Getenv("BOT_ACCEPTANCE_DESKTOP_TITLE")
	resultPath := os.Getenv("BOT_ACCEPTANCE_DESKTOP_RESULT")
	oncePath := os.Getenv("BOT_ACCEPTANCE_DESKTOP_ONCE")
	if base == "" || helper == "" || title == "" || resultPath == "" || oncePath == "" {
		return errors.New("desktop acceptance requires private evidence dir, packaged helper, exact fixture title, app-owned result path and durable per-fixture once marker")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(base, "model-desktop-")
	if err != nil {
		return err
	}
	// This path is deliberately absent. Session probes it, then starts an
	// attachable private App Server; it cannot attach to a user-owned socket.
	privateSocket := filepath.Join(dir, "absent-shared.sock")
	if _, err := os.Lstat(privateSocket); !errors.Is(err, os.ErrNotExist) {
		return errors.New("private App Server selector unexpectedly exists")
	}
	once, err := os.OpenFile(oncePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = once.WriteString("one synthetic desktop user turn\n")
	closeErr := once.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	resident, err := bot.NewForRuntime(filepath.Join(dir, "bot.json"), "codex", func(string) error { return nil })
	if err != nil {
		return err
	}
	defer resident.Close()
	driver := desktopcontrol.New(helper, filepath.Join(dir, "desktop-assets"))
	defer driver.Close()
	resident.ConfigureDesktopControl(driver)
	bridge, err := bot.Serve(resident)
	if err != nil {
		return err
	}
	defer bridge.Close()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	config := bridge.Config(executable)
	skill, err := botskills.Install(dir)
	if err != nil {
		return err
	}
	config.Instructions = "You are the resident Caelis Bot in an isolated native desktop acceptance. Follow Bot desktop skill guidance. You may use the native shell solely to read the supplied Bot skill file and its linked desktop references; do not use it for desktop interaction or any other purpose. Use Bot desktop tools only on the exact disposable window named by the user. Do not use a browser, native subagent, other application, or account/configuration tool. Preserve original request IDs; never replay uncertain input. " + botskills.Instructions(skill)
	config.PrepareTurn = func(context.Context) error { resident.BeginDesktopTurn(); return nil }
	config.FinishTurn = resident.StopDesktopTurn
	s := codex.NewSession(codex.SessionOptions{Binary: os.Getenv("CODEX_BIN"), Socket: privateSocket, Directory: filepath.Join(dir, "work"), StateFile: filepath.Join(dir, "binding.json"), BotTools: config})
	defer func() {
		closeCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		_ = s.Close(closeCtx)
	}()
	proof := map[string]any{"nativeIdentity": "installed login (not copied)", "appServer": "private owned instance via absent dedicated socket", "botData": "new private directory", "fixtureTitle": title, "expectedToolCatalog": []string{"bot_desktop_inspect", "bot_desktop_authorize", "bot_desktop_act", "bot_desktop_result"}, "requestId": "rc2-model-desktop-once"}
	save := func(status string) error {
		v := s.Snapshot()
		proof["status"] = status
		proof["connection"] = v.Connection
		proof["phase"] = v.Phase
		proof["lastSubmissionReceiptOutcome"] = v.LastReceipt.Outcome
		pending := []string{}
		for _, a := range v.Approvals {
			if a.Status == "pending" {
				pending = append(pending, a.Title)
			}
		}
		proof["pendingNativeApprovals"] = pending
		reviews := []map[string]string{}
		for _, review := range v.Reviews {
			reviews = append(reviews, map[string]string{"status": review.Status, "action": review.Action})
		}
		proof["nativeReviewDecisions"] = reviews
		b, err := json.MarshalIndent(proof, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "summary.json"), b, 0600)
	}
	if err := s.Connect(ctx); err != nil {
		proof["errorClass"] = "connect_failed"
		_ = save("blocked")
		return err
	}
	ready, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	for !s.Snapshot().CanSend {
		select {
		case <-ready.Done():
			_ = save("native_not_ready")
			return ready.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if _, err := os.Stat(resultPath); !errors.Is(err, os.ErrNotExist) {
		return errors.New("synthetic fixture result must be absent before the only model request")
	}
	prompt := fmt.Sprintf(`Use the four compact Bot desktop tools and the supplied skill. You may read only that skill and its linked desktop references with the native file reader. Find only the existing disposable window titled %q in the Caelis Desktop Control Fixture application. Inspect narrowly, authorize exactly that observed application for this task, set its "Verification text" field to RC2_MODEL_DESKTOP_OK, invoke "Submit once" exactly once, then query the original action receipt and independently read back the field and Submitted: 1. If native approval is required, wait for it; never imply it was granted. If any action has partial or unknown outcome, query only its original requestId and stop. Do not touch any other application or use a shell for interaction.`, title)
	receipt, err := s.Submit(ctx, api.Submission{ID: "rc2-model-desktop-once", Text: prompt}, nil)
	if err != nil || receipt.Outcome != "accepted" {
		proof["submissionOutcome"] = receipt.Outcome
		_ = save("submission_unconfirmed")
		return fmt.Errorf("desktop model submission unconfirmed: %v", err)
	}
	proof["submissionOutcome"] = receipt.Outcome
	for {
		v := s.Snapshot()
		for _, a := range v.Approvals {
			if a.Status == "pending" {
				return save("native_approval_pending")
			}
		}
		if v.Phase == "completed" {
			var actual struct {
				Submissions int    `json:"submissions"`
				Text        string `json:"text"`
			}
			b, err := os.ReadFile(resultPath)
			if err != nil || json.Unmarshal(b, &actual) != nil || actual.Submissions != 1 || actual.Text != "RC2_MODEL_DESKTOP_OK" {
				_ = save("completed_without_app_effect")
				return errors.New("model turn completed without independently verified exact-once app effect")
			}
			proof["appOwnedEffect"] = map[string]any{"submissions": actual.Submissions, "text": actual.Text}
			return save("app_effect_verified")
		}
		if v.Connection == "offline" || v.Phase == "failed" {
			_ = save("backend_failed")
			return errors.New("model desktop turn failed")
		}
		select {
		case <-ctx.Done():
			_ = save("outcome_unknown")
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
