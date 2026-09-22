// Real Codex acceptance with synthetic input, a private Bot store and no UI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

func wait(ctx context.Context, s *codex.Session) error {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		v := s.Snapshot()
		for _, a := range v.Approvals {
			if a.Status == "pending" {
				return fmt.Errorf("unexpected approval after per-tool configuration: %s", a.Title)
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
func submit(ctx context.Context, s *codex.Session, id, text string) error {
	r, e := s.Submit(ctx, api.Submission{ID: id, Text: text}, nil)
	if e != nil {
		return e
	}
	if r.Outcome != "accepted" {
		return errors.New("submission not accepted")
	}
	return wait(ctx, s)
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	dir, e := os.MkdirTemp("", "caelis-bot-integration-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	opts := codex.SessionOptions{Binary: os.Getenv("CODEX_BIN"), Directory: filepath.Join(dir, "work"), StateFile: filepath.Join(dir, "binding.json")}
	initial := codex.NewSession(opts)
	if e = initial.Connect(ctx); e != nil {
		return e
	}
	if e = submit(ctx, initial, "initial-bot-check", "Only reply: continuity-marker. Do not call tools."); e != nil {
		_ = initial.Close(ctx)
		return e
	}
	if e = initial.Close(ctx); e != nil {
		return e
	}
	var gestures atomic.Int32
	companion, e := bot.New(filepath.Join(dir, "bot.json"), func(string) error { gestures.Add(1); return nil })
	if e != nil {
		return e
	}
	bridge, e := bot.Serve(companion)
	if e != nil {
		return e
	}
	defer bridge.Close()
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	opts.BotTools = bridge.Config(executable)
	s := codex.NewSession(opts)
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = s.Close(c)
	}()
	if e = s.Connect(ctx); e != nil {
		return e
	}
	found := false
	for _, i := range s.Snapshot().Items {
		if strings.Contains(i.Text, "continuity-marker") {
			found = true
		}
	}
	if !found {
		return errors.New("resume lost history")
	}
	prompt := `This is an isolated Caelis Bot integration test. Discover the appropriate tools using the native discovery entry point (tool_search, or ALL_TOOLS metadata if only Code Mode is exposed). Read the local time, briefly nod the desktop pet, then save a reminder with id synthetic-check, label Synthetic, prompt Only reply synthetic-reminder, and everyMinutes 60; list reminders. Do not use shell, files, external services or network. Reply only bot-tools-ok after successful tool receipts.`
	fmt.Fprintln(os.Stderr, "stage: tools on resumed thread")
	if e = submit(ctx, s, "tools-after-resume", prompt); e != nil {
		return e
	}
	if gestures.Load() != 1 || len(companion.State().Schedules) != 1 {
		return fmt.Errorf("MCP tools missing after resume: gestures=%d schedules=%d", gestures.Load(), len(companion.State().Schedules))
	}
	if e = companion.Remove("synthetic-check"); e != nil {
		return e
	}
	_, e = companion.Upsert(bot.Schedule{ID: "wake-check", Label: "Synthetic wake", Prompt: "Use only bot_gesture with action attention, then reply scheduled-wake-ok. Do not use other tools or access files or network.", At: time.Now().Add(2 * time.Second).Format(time.RFC3339)})
	if e != nil {
		return e
	}
	fmt.Fprintln(os.Stderr, "stage: scheduled activation")
	companion.Start(s)
	defer companion.Close()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		w := companion.State().Wake
		if w != nil && w.Status == "accepted" {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	if e = wait(ctx, s); e != nil {
		return e
	}
	if gestures.Load() != 2 {
		return errors.New("scheduled activation did not reach character driver")
	}
	prompt = `This is an isolated worker integration test. Start two native worker threads in parallel. Each must only return a different short marker, without tools, files, network or shell. Communicate one follow-up to one worker using native inter-agent communication, asking it to append done. Wait for both and return one short combined answer. Do not create tasks in the Codex desktop application.`
	fmt.Fprintln(os.Stderr, "stage: native workers")
	if e = submit(ctx, s, "workers-check", prompt); e != nil {
		return e
	}
	workers, spawns, messages := 0, 0, 0
	kinds := map[string]int{}
	for _, i := range s.Snapshot().Items {
		if i.Kind == "activity" && i.Text == "协作处理" {
			workers++
			kinds[i.Details]++
			if i.Details == "spawnAgent" || i.Details == "started" {
				spawns++
			}
			if i.Details == "sendInput" || i.Details == "sendMessage" || i.Details == "followupTask" || i.Details == "interacted" {
				messages++
			}
		}
	}
	var binding struct {
		Children []string `json:"children"`
	}
	data, e := os.ReadFile(opts.StateFile)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(data, &binding); e != nil {
		return e
	}
	if len(binding.Children) < 2 || spawns < 2 || messages < 1 {
		return fmt.Errorf("worker acceptance incomplete: children=%d spawn=%d communication=%d events=%d kinds=%v", len(binding.Children), spawns, messages, workers, kinds)
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{"testedCodex": codex.TestedVersion, "resumedHistory": true, "mcpOnResume": true, "scheduledActivation": true, "characterCalls": gestures.Load(), "workerEvents": workers, "workers": len(binding.Children), "communicationEvents": messages})
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--bot-tools" {
		if bot.RunStdio(os.Stdin, os.Stdout) != nil {
			os.Exit(1)
		}
		return
	}
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
