//go:build (darwin && cgo) || linux

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func helperArgs() []string {
	for i, a := range os.Args {
		if a == "--" {
			return os.Args[i+1:]
		}
	}
	return nil
}

// Test-only helper dispatch. Native production has no power-bypass option.
func TestSupervisedWatchdogProcessHelper(t *testing.T) {
	args := helperArgs()
	if len(args) == 0 {
		return
	}
	if len(args) != 1 || args[0] != "owned-runtime-watchdog" {
		os.Exit(2)
	}
	err := RunSupervisedRuntimeWithPower(context.Background(), os.NewFile(3, "private-control"), func(ctx context.Context, suspend, wake func()) (func(), error) {
		path := os.Getenv("CAELIS_WATCHDOG_FIXTURE_POWER")
		if path == "" {
			return func() {}, nil
		}
		life, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-life.Done():
					return
				case <-tick.C:
					b, err := os.ReadFile(path + ".request")
					if err == nil && string(b) == "suspend" {
						suspend()
						_ = os.WriteFile(path+".ack", []byte("ack-after-native-stop"), 0600)
						return
					}
				}
			}
		}()
		return func() { cancel(); <-done }, nil
	})
	if err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
func TestSupervisedNativeProcessHelper(t *testing.T) {
	args := helperArgs()
	if len(args) == 0 {
		return
	}
	tool := exec.Command("/bin/sleep", "60")
	if tool.Start() != nil {
		os.Exit(7)
	}
	if os.WriteFile(args[0]+".tool", []byte(strconv.Itoa(tool.Process.Pid)), 0600) != nil {
		os.Exit(8)
	}
	TestWorkerProcessHelper(t)
}

type supervisedOwnerStatus struct{ Native, Tool, Watchdog int }

func TestSupervisedAbruptOwnerHelper(t *testing.T) {
	args := helperArgs()
	if len(args) == 0 {
		return
	}
	if len(args) != 5 {
		os.Exit(2)
	}
	p, err := StartSupervisedProcess(context.Background(), SupervisedProcessOptions{HelperPath: args[0], Binary: args[1], Directory: args[2], Socket: filepath.Join(args[2], "runtime.sock"), Kind: SupervisedCodexUnix})
	if err != nil {
		os.Exit(3)
	}
	defer p.Stop(context.Background())
	var tool int
	for range 200 {
		b, err := os.ReadFile(args[3] + ".tool")
		if err == nil {
			tool, _ = strconv.Atoi(string(b))
			if tool > 1 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if tool <= 1 {
		os.Exit(4)
	}
	if p.Renew(context.Background(), "epoch", 30*time.Second) != nil {
		os.Exit(5)
	}
	b, _ := json.Marshal(supervisedOwnerStatus{Native: p.PID(), Tool: tool, Watchdog: p.cmd.Process.Pid})
	if os.WriteFile(args[4], b, 0600) != nil {
		os.Exit(6)
	}
	select {}
}
func supervisorFixture(t *testing.T) (helper, native, dir, pidFile string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err = os.MkdirTemp("/tmp", "caelis-wd-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	pidFile = filepath.Join(dir, "native.pid")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	helper = filepath.Join(dir, "watchdog")
	native = filepath.Join(dir, "native")
	if err = os.WriteFile(helper, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestSupervisedWatchdogProcessHelper$' -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(native, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestSupervisedNativeProcessHelper$' -- "+quote(pidFile)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return
}
func waitSupervisedTool(t *testing.T, pidFile string) int {
	t.Helper()
	for range 200 {
		b, err := os.ReadFile(pidFile + ".tool")
		if err == nil {
			pid, _ := strconv.Atoi(string(b))
			if pid > 1 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native tool did not reach barrier")
	return 0
}
func assertOwnedExited(t *testing.T, root, tool *ownedTools) {
	t.Helper()
	for range 300 {
		a, ea := root.watchdogRootLive()
		b, eb := tool.watchdogRootLive()
		if ea == nil && eb == nil && !a && !b {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("owned native root/tool exit unconfirmed")
}
func TestSupervisedOwnerAbruptDeathFencesNativeAndTool(t *testing.T) {
	helper, native, dir, pidFile := supervisorFixture(t)
	executable, _ := os.Executable()
	statusFile := filepath.Join(dir, "ready.json")
	owner := exec.Command(executable, "-test.run=^TestSupervisedAbruptOwnerHelper$", "--", helper, native, dir, pidFile, statusFile)
	owner.Stdout, owner.Stderr = nil, nil
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
	var status supervisedOwnerStatus
	for range 300 {
		b, err := os.ReadFile(statusFile)
		if err == nil && json.Unmarshal(b, &status) == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.Native <= 1 || status.Tool <= 1 {
		t.Fatal("owner/native/tool barrier unavailable")
	}
	root, tool := newOwnedTools(status.Native), newOwnedTools(status.Tool)
	other := exec.Command("/bin/sleep", "60")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Process.Kill(); _ = other.Wait() }()
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = owner.Wait()
	assertOwnedExited(t, root, tool)
	if err := syscall.Kill(other.Process.Pid, 0); err != nil {
		t.Fatal("unrelated process targeted", err)
	}
}
func TestSupervisedDeadlineAndWatchdogDeath(t *testing.T) {
	for _, scenario := range []string{"deadline", "watchdog-death"} {
		t.Run(scenario, func(t *testing.T) {
			helper, native, dir, pidFile := supervisorFixture(t)
			p, err := StartSupervisedProcess(testContext(t), SupervisedProcessOptions{HelperPath: helper, Binary: native, Directory: dir, Socket: filepath.Join(dir, "runtime.sock"), Kind: SupervisedCodexUnix})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Stop(testContext(t))
			toolPID := waitSupervisedTool(t, pidFile)
			root, tool := newOwnedTools(p.PID()), newOwnedTools(toolPID)
			remaining := 30 * time.Second
			if scenario == "deadline" {
				remaining = 2 * time.Second
			}
			if err = p.Renew(testContext(t), "epoch", remaining); err != nil {
				t.Fatal(err)
			}
			if scenario == "watchdog-death" {
				if err = p.cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				select {
				case <-p.exited:
				case <-time.After(time.Second):
					t.Fatal("watchdog death not observed")
				}
				if err = p.Stop(testContext(t)); err != nil {
					t.Fatal("independent owner fallback failed", err)
				}
			}
			assertOwnedExited(t, root, tool)
			if p.Live() {
				t.Fatal("expired supervision advertised live")
			}
		})
	}
}
func TestSupervisedEpochCannotRebindOwnedGeneration(t *testing.T) {
	helper, native, dir, pidFile := supervisorFixture(t)
	p, err := StartSupervisedProcess(testContext(t), SupervisedProcessOptions{HelperPath: helper, Binary: native, Directory: dir, Socket: filepath.Join(dir, "runtime.sock"), Kind: SupervisedCodexUnix})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop(testContext(t))
	toolPID := waitSupervisedTool(t, pidFile)
	root, tool := newOwnedTools(p.PID()), newOwnedTools(toolPID)
	if err = p.Renew(testContext(t), "epoch", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if err = p.Renew(testContext(t), "other", 30*time.Second); err == nil {
		t.Fatal("generation rebound to a new epoch")
	}
	assertOwnedExited(t, root, tool)
	if !errors.Is(syscall.Kill(p.PID(), 0), syscall.ESRCH) {
		live, _ := root.watchdogRootLive()
		if live {
			t.Fatal("old root remained live")
		}
	}
}

func TestSupervisedIndependentPowerStopsWhileOwnerPausedBeforeAck(t *testing.T) {
	helper, native, dir, pidFile := supervisorFixture(t)
	power := filepath.Join(dir, "power")
	t.Setenv("CAELIS_WATCHDOG_FIXTURE_POWER", power)
	executable, _ := os.Executable()
	statusFile := filepath.Join(dir, "ready.json")
	owner := exec.Command(executable, "-test.run=^TestSupervisedAbruptOwnerHelper$", "--", helper, native, dir, pidFile, statusFile)
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Signal(syscall.SIGCONT); _ = owner.Process.Kill(); _ = owner.Wait() }()
	var status supervisedOwnerStatus
	for range 300 {
		b, err := os.ReadFile(statusFile)
		if err == nil && json.Unmarshal(b, &status) == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.Native <= 1 || status.Tool <= 1 {
		t.Fatal("independent power barrier unavailable")
	}
	root, tool := newOwnedTools(status.Native), newOwnedTools(status.Tool)
	if err := owner.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(power+".request", []byte("suspend"), 0600); err != nil {
		t.Fatal(err)
	}
	ack := false
	for range 400 {
		b, err := os.ReadFile(power + ".ack")
		if err == nil && string(b) == "ack-after-native-stop" {
			ack = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ack {
		t.Fatal("independent power acknowledgement missing")
	}
	aliveRoot, rootErr := root.watchdogRootLive()
	aliveTool, toolErr := tool.watchdogRootLive()
	if rootErr != nil || toolErr != nil || aliveRoot || aliveTool {
		t.Fatal("power acknowledged before native root/tool stop", aliveRoot, aliveTool, rootErr, toolErr)
	}
}

func TestSupervisedClockMismatchClosesNativeAdmission(t *testing.T) {
	helper, native, dir, pidFile := supervisorFixture(t)
	p, err := StartSupervisedProcess(testContext(t), SupervisedProcessOptions{HelperPath: helper, Binary: native, Directory: dir, Socket: filepath.Join(dir, "runtime.sock"), Kind: SupervisedCodexUnix})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop(testContext(t))
	toolPID := waitSupervisedTool(t, pidFile)
	root, tool := newOwnedTools(p.PID()), newOwnedTools(toolPID)
	clock, err := supervisorClock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.call(testContext(t), supervisorFrame{Method: "renew", Epoch: "epoch", DeadlineNs: clock + int64(30*time.Second), WallDeadlineNs: time.Now().UnixNano() + int64(90*time.Second)}); err == nil {
		t.Fatal("mismatched host clocks admitted")
	}
	assertOwnedExited(t, root, tool)
}
