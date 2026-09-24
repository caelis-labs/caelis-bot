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
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (runErr error) {
	binary := flag.String("codex", "codex", "installed Codex CLI")
	terminal := flag.Bool("terminal", false, "open the isolated worker in the associated terminal; leave a 45s observation window")
	hold := flag.Bool("hold", false, "print an isolated attach target and keep it alive for a bounded native TUI probe")
	ruleFile := flag.String("rules", "", "replay mode: JSON rules (no Codex process)")
	eventFile := flag.String("events", "", "replay mode: trusted JSONL envelopes; empty means stdin")
	flag.Parse()
	if *ruleFile != "" {
		return replay(*ruleFile, *eventFile)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "caelis-event-poc-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	defer func() {
		if runErr != nil {
			if b, e := os.ReadFile(filepath.Join(dir, "runtime.log")); e == nil {
				fmt.Fprintln(os.Stderr, string(b[max(0, len(b)-4000):]))
			}
		}
	}()
	home := filepath.Join(dir, "home")
	if err = os.Mkdir(home, 0700); err != nil {
		return err
	}
	p := newProvider(700 * time.Millisecond)
	defer p.Close()
	config := fmt.Sprintf("model = \"poc\"\nmodel_provider = \"poc\"\n[model_providers.poc]\nname = \"Isolated POC\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", p.URL)
	if err = os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		return err
	}
	socket := filepath.Join(dir, "runtime.sock")
	// Minimal environment: the POC cannot load personal Codex config/auth/MCPs.
	cmd := exec.CommandContext(ctx, *binary, "app-server", "--listen", "unix://"+socket)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "CODEX_HOME=" + home, "TMPDIR=" + dir}
	log, err := os.OpenFile(filepath.Join(dir, "runtime.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
	connect := func() (*client, error) {
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		for {
			c, e := dial(ctx, socket)
			if e == nil {
				return c, nil
			}
			select {
			case <-deadline.C:
				return nil, e
			case <-time.After(50 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	a, err := connect()
	if err != nil {
		return err
	}
	defer a.ws.CloseNow()
	worker, err := a.thread(ctx, dir)
	if err != nil {
		return err
	}
	bot, err := a.thread(ctx, dir)
	if err != nil {
		return err
	}
	// Persist initial history so an independent CLI can resume this exact session.
	if err = a.prompt(ctx, worker, "Initial synthetic worker message"); err != nil {
		return err
	}
	if _, err = a.wait(ctx, "turn/started", worker); err != nil {
		return err
	}
	if _, err = a.wait(ctx, "turn/completed", worker); err != nil {
		return err
	}
	b, err := connect()
	if err != nil {
		return err
	}
	defer b.ws.CloseNow()
	if _, err = b.call(ctx, "thread/resume", map[string]any{"threadId": worker}); err != nil {
		return err
	}
	if err = a.prompt(ctx, worker, "Bot starts delegated work"); err != nil {
		return err
	}
	started, err := a.wait(ctx, "turn/started", worker)
	if err != nil {
		return err
	}
	active, _ := turnID(started)
	if _, err = b.call(ctx, "turn/steer", map[string]any{"threadId": worker, "expectedTurnId": active, "input": []any{map[string]any{"type": "text", "text": "Human adds an instruction through a second protocol client"}}}); err != nil {
		return err
	}
	finished, err := a.wait(ctx, "turn/completed", worker)
	if err != nil {
		return err
	}
	other, err := b.wait(ctx, "turn/completed", worker)
	if err != nil {
		return err
	}
	x, status := turnID(finished)
	y, _ := turnID(other)
	if x != y || status != "completed" {
		return fmt.Errorf("completion mismatch: %s / %s / %s", x, y, status)
	}
	rules := []Rule{{ID: "completion", On: "worker.completed", When: `event.status == 'completed'`, Prompt: "Report the completed worker result"}, {ID: "custom", On: "custom.event", When: `event.priority >= 2 && event.tags.exists(t, t == 'care')`, Prompt: "Perform the configured proactive task", CooldownSeconds: 3600}}
	e, err := newEngine(filepath.Join(dir, "activations.json"), rules)
	if err != nil {
		return err
	}
	_, err = e.receive(ctx, Event{x, "worker.completed", map[string]any{"status": status}}, time.Now())
	if err != nil {
		return err
	}
	yes := true
	deliver := func(v Activation) error {
		if err := a.prompt(ctx, bot, v.Prompt); err != nil {
			return err
		}
		_, err := a.wait(ctx, "turn/completed", bot)
		return err
	}
	n, err := e.deliver(time.Now(), Presence{true, &yes}, deliver)
	if err != nil || n != 1 {
		return fmt.Errorf("completion activation: %d %w", n, err)
	}
	// A one-shot native timer, not a polling loop, generates an arbitrary event.
	timer := time.NewTimer(100 * time.Millisecond)
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	_, err = e.receive(ctx, Event{"timer-1", "custom.event", map[string]any{"priority": 3, "tags": []string{"care"}}}, time.Now())
	if err != nil {
		return err
	}
	locked := false
	n, err = e.deliver(time.Now(), Presence{true, &locked}, deliver)
	if err != nil || n != 0 {
		return errors.New("locked presence was not gated")
	}
	n, err = e.deliver(time.Now(), Presence{true, &yes}, deliver)
	if err != nil || n != 1 {
		return fmt.Errorf("timer activation: %d %w", n, err)
	}
	// User starts a later turn: the Bot sees it through its existing subscription.
	if err = b.prompt(ctx, worker, "Human starts the next worker turn"); err != nil {
		return err
	}
	if _, err = a.wait(ctx, "turn/completed", worker); err != nil {
		return err
	}
	if _, err = b.wait(ctx, "turn/completed", worker); err != nil {
		return err
	}
	if _, err = b.call(ctx, "thread/unsubscribe", map[string]any{"threadId": worker}); err != nil {
		return err
	}
	if err = a.prompt(ctx, worker, "Bot continues after the human detaches"); err != nil {
		return err
	}
	if _, err = a.wait(ctx, "turn/completed", worker); err != nil {
		return err
	}
	nativeTUITurn := false
	if *terminal || *hold {
		path, err := exec.LookPath(*binary)
		if err != nil {
			return err
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return err
		}
		launch := filepath.Join(dir, "Observe Worker.command")
		pidFile := filepath.Join(dir, "terminal.pid")
		quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
		text := "#!/bin/sh\numask 077\necho $$ > " + quote(pidFile) + "\nexport CODEX_HOME=" + quote(home) + "\ncd " + quote(dir) + "\nexec " + quote(path) + " --remote " + quote("unix://"+socket) + " resume " + quote(worker) + "\n"
		if err = os.WriteFile(launch, []byte(text), 0700); err != nil {
			return err
		}
		if *terminal {
			defer func() {
				b, err := os.ReadFile(pidFile)
				if err != nil {
					return
				}
				pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
				if err != nil || pid <= 1 {
					return
				}
				// An external Terminal does not make this child a Go exec.Cmd child.
				// Verify its exact POC target before cleanup; never kill by app name.
				args, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
				if err == nil && strings.Contains(string(args), "--remote unix://"+socket+" resume "+worker) {
					if p, err := os.FindProcess(pid); err == nil {
						_ = p.Signal(syscall.SIGTERM)
					}
				}
			}()
			if out, err := exec.Command("/usr/bin/open", launch).CombinedOutput(); err != nil {
				return fmt.Errorf("terminal launch: %w %s", err, out)
			}
		}
		if *hold {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"home": home, "socket": socket, "worker": worker, "binary": path})
		}
		fmt.Println("POC_TERMINAL_OPEN: shared native Worker; 45 second observation window")
		if err = a.prompt(ctx, worker, "A protocol message visible in the external terminal"); err != nil {
			return err
		}
		if _, err = a.wait(ctx, "turn/completed", worker); err != nil {
			return err
		}
		if *hold {
			m, err := a.wait(ctx, "turn/completed", worker)
			if err != nil {
				return fmt.Errorf("native TUI message: %w", err)
			}
			_, status := turnID(m)
			nativeTUITurn = status == "completed"
			time.Sleep(time.Second)
		} else {
			select {
			case <-time.After(45 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if a.counts["thread/read"] != 0 || b.counts["thread/read"] != 0 {
		return errors.New("worker polling detected")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"result": "pass", "runtime": "real installed Codex app-server", "provider": "synthetic loopback Responses", "presence": "explicit locked/unlocked fixtures, not OS lock proof", "botRequests": a.counts, "humanRequests": b.counts, "providerCalls": p.calls.Load(), "workerStateReads": 0, "sharedCompletion": true, "humanSteer": true, "humanNextTurnObserved": true, "detachPreservesBot": true, "completionActivation": true, "programmableTimerActivation": true, "nativeTuiTurnObserved": nativeTUITurn})
}
