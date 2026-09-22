// Explicit opt-in model/tool smoke. All inputs and artifacts are synthetic.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

var scenario = flag.String("scenario", "files", "files, approval, interrupt, background")
var approvalCount int

func wait(ctx context.Context, s *codex.Session) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	last := ""
	for {
		view := s.Snapshot()
		if view.Phase != last {
			fmt.Fprintf(os.Stderr, "workflow phase: %s\n", view.Phase)
			last = view.Phase
		}
		for _, a := range view.Approvals {
			if a.Status == "pending" {
				if *scenario != "approval" {
					return errors.New("unexpected approval; not authorizing it")
				}
				command := strings.Split(a.Details, "\n位置：")[0]
				if command != `python3 -c 'print("caelis-approval-ok")'` && command != `python3 -c "print('caelis-approval-ok')"` && command != `/bin/zsh -lc "python3 -c 'print(\"caelis-approval-ok\")'"` {
					return fmt.Errorf("unexpected synthetic approval command: %s", command)
				}
				choice := ""
				for _, c := range a.Choices {
					if c.Label == "允许这一次" {
						choice = c.ID
					}
				}
				if choice == "" {
					return errors.New("native one-shot choice missing")
				}
				if err := s.Decide(ctx, api.Decision{ID: a.ID, Choice: choice}); err != nil {
					return err
				}
				approvalCount++
			}
		}
		if view.Phase == "completed" {
			return nil
		}
		if view.Phase == "failed" || view.Connection == "offline" {
			return fmt.Errorf("workflow failed: %s", view.Message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func run() error {
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp("", "caelis-bot-workflow-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	work := filepath.Join(dir, "work")
	state := filepath.Join(dir, "binding.json")
	s := codex.NewSession(codex.SessionOptions{Binary: os.Getenv("CODEX_BIN"), Directory: work, StateFile: state, RequireApproval: *scenario == "approval"})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	}()
	if err = s.Connect(ctx); err != nil {
		return err
	}
	if !s.Snapshot().CanSend {
		return errors.New("existing account cannot start a turn")
	}
	var files []api.InputFile
	prompt := "Read the attached text file, then use a local tool to write acceptance.txt in the current working directory with exactly the attachment contents. Do not access any other files or network. Give a short final reply linking acceptance.txt."
	switch *scenario {
	case "files":
		source := filepath.Join(dir, "synthetic-input.txt")
		if err := os.WriteFile(source, []byte("caelis-adapter-ok\n"), 0600); err != nil {
			return err
		}
		files = []api.InputFile{{Name: "synthetic-input.txt", Path: source}}
	case "approval":
		prompt = `This is an isolated approval integration test. Run exactly this command using the shell tool: python3 -c 'print("caelis-approval-ok")' . Do not modify the command, access files or network, or use another tool. After its output reply with that exact marker.`
	case "interrupt", "background":
		prompt = "This is an isolated lifecycle integration test. Run exactly this shell command in the current directory: echo $$ > live.pid; exec /bin/sleep 60 . Use a 1000ms yield timeout so it continues as a background command. Then immediately send a final reply without waiting or polling. Do not run anything else."
	default:
		return errors.New("unknown scenario")
	}
	r, err := s.Submit(ctx, api.Submission{ID: "workflow-" + *scenario, Text: prompt}, files)
	if err != nil {
		return err
	}
	if r.Outcome != "accepted" {
		return errors.New("turn was not accepted: " + r.Message)
	}
	var pid int
	if *scenario == "interrupt" || *scenario == "background" {
		for pid == 0 {
			b, _ := os.ReadFile(filepath.Join(work, "live.pid"))
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			if pid > 0 {
				break
			}
			if s.Snapshot().Phase == "failed" {
				return errors.New("lifecycle turn failed")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		if *scenario == "interrupt" {
			if err = s.Interrupt(ctx); err != nil {
				return err
			}
			for s.Snapshot().Phase != "interrupted" {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
			}
			deadline := time.Now().Add(3 * time.Second)
			for processAlive(pid) && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			if processAlive(pid) {
				info, _ := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "pid=,ppid=,stat=,comm=").Output()
				return fmt.Errorf("tool survived interrupt cleanup before application close: %s", strings.TrimSpace(string(info)))
			}
		} else if err = wait(ctx, s); err != nil {
			return err
		}
	} else if err = wait(ctx, s); err != nil {
		return err
	}
	if *scenario == "files" {
		b, err := os.ReadFile(filepath.Join(work, "acceptance.txt"))
		if err != nil || string(b) != "caelis-adapter-ok\n" {
			return errors.New("attachment/artifact verification failed")
		}
	}
	if *scenario == "approval" && approvalCount == 0 {
		return errors.New("real server did not request approval")
	}

	if pid > 0 && *scenario == "background" && !processAlive(pid) {
		return errors.New("background tool finished before cleanup; sample cannot prove shutdown")
	}
	before := s.Snapshot()
	closeCtx, closeCancel := context.WithTimeout(ctx, 15*time.Second)
	err = s.Close(closeCtx)
	closeCancel()
	if err != nil {
		return err
	}
	if pid > 0 {
		deadline := time.Now().Add(3 * time.Second)
		for processAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if processAlive(pid) {
			return errors.New("owned tool process survived shutdown")
		}
	}
	resumed := codex.NewSession(codex.SessionOptions{Binary: os.Getenv("CODEX_BIN"), Directory: work, StateFile: state, RequireApproval: *scenario == "approval"})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = resumed.Close(ctx)
	}()
	if err = resumed.Connect(ctx); err != nil {
		return err
	}
	after := resumed.Snapshot()
	if len(after.Items) == 0 || !after.CanSend {
		return errors.New("history did not recover to idle")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"testedCodex": codex.TestedVersion, "scenario": *scenario, "realTurn": "passed", "approvals": approvalCount, "toolProcessReaped": pid > 0, "reconnectHistory": "passed", "itemsBefore": len(before.Items), "itemsAfter": len(after.Items), "modelRequestSent": true, "privateInput": false})
}
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
