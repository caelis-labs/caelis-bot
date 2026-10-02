package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLinuxWatchdogStopReapsExactOwnedTree(t *testing.T) {
	helper, native, dir, pidFile := supervisorFixture(t)
	p, err := StartSupervisedProcess(t.Context(), SupervisedProcessOptions{HelperPath: helper, Binary: native, Directory: dir, Socket: filepath.Join(dir, "runtime.sock"), Kind: SupervisedCodexUnix})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := p.Stop(stop); err != nil {
			t.Error(err)
		}
	})
	toolPID := waitSupervisedTool(t, pidFile)
	var identities []linuxProcess
	var handles []int
	for _, pid := range []int{p.PID(), toolPID} {
		identity, err := readLinuxProcess(pid)
		if err != nil {
			t.Fatal("original fixture identity unavailable", pid, err)
		}
		identities = append(identities, identity)
		fd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			t.Fatal(err)
		}
		handles = append(handles, fd)
		t.Cleanup(func() { _ = unix.Close(fd) })
	}
	// Install a real lease renewal after the tool's startup barrier, ensuring
	// the watchdog captures the native ancestry before its exact hard fence.
	if err := p.Renew(t.Context(), "fixture-generation", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	other := exec.Command("/bin/sleep", "60")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })
	stop, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	if err := p.Stop(stop); err != nil {
		t.Fatal(err)
	}
	for i, original := range identities {
		exited, err := linuxHandleExited(handles[i])
		if err != nil || !exited {
			t.Fatal("stop receipt preceded original process exit", original, err)
		}
		current, err := readLinuxProcess(original.pid)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("stop receipt preceded original process reaping", "original", original, "current", current, err)
		}
		if err := unix.Kill(original.pid, 0); !errors.Is(err, unix.ESRCH) {
			t.Fatal("owned fixture process retained after confirmed stop", original, err)
		}
	}
	if err := unix.Kill(other.Process.Pid, 0); err != nil {
		t.Fatal("unrelated child was killed or reaped", err)
	}
}

func TestLinuxWatchdogReapingRejectsExecutingDescendant(t *testing.T) {
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	p, err := readLinuxProcess(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.PidfdOpen(p.pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	tools := &ownedTools{children: map[int]uint64{p.pid: p.born}, handles: map[int]int{p.pid: fd}}
	if err := tools.reapWatchdogChildren(); err == nil {
		t.Fatal("executing descendant was accepted as reaped")
	}
	exited, err := linuxHandleExited(fd)
	if err != nil || exited {
		t.Fatal("reaping observation mutated an executing descendant", exited, err)
	}
	if err := unix.Kill(p.pid, 0); err != nil {
		t.Fatal("executing process was targeted without a stop", err)
	}
}

func TestLinuxZombieParentHelper(t *testing.T) {
	if len(helperArgs()) == 0 {
		return
	}
	child := exec.Command("/bin/sleep", "60")
	if child.Start() != nil {
		os.Exit(2)
	}
	fmt.Println(child.Process.Pid)
	// Deliberately retain our own child's exit status until the test requests
	// reaping. Closing the control input also performs this exact cleanup.
	var command string
	_, _ = fmt.Fscan(os.Stdin, &command)
	_ = child.Process.Kill()
	_ = child.Wait()
	os.Exit(0)
}

func TestLinuxWatchdogReapingPreservesNonWaitableZombieFailure(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(executable, "-test.run=^TestLinuxZombieParentHelper$", "--", "fixture")
	input, err := parent.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = parent.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	t.Cleanup(func() {
		_ = input.Close()
		if !reaped {
			_ = parent.Wait()
		}
	})
	var pidText string
	if _, err := fmt.Fscan(output, &pidText); err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatal(err)
	}
	tools := newOwnedTools(parent.Process.Pid)
	defer tools.terminate()
	tools.capture()
	if err := tools.failure(); err != nil {
		t.Fatal(err)
	}
	born, captured := tools.children[pid]
	fd := tools.handles[pid]
	if !captured || fd < 0 {
		t.Fatal("original descendant was not captured")
	}
	if err := signalLinuxHandle(fd, pid, born, unix.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	exited, err := linuxHandleExitedWithPoll(fd, func(fds []unix.PollFd, _ int) (int, error) {
		remaining := max(0, int(time.Until(deadline).Milliseconds()))
		return unix.Poll(fds, remaining)
	})
	if err != nil || !exited {
		t.Fatal("fixture descendant did not exit", err)
	}
	current, err := readLinuxProcess(pid)
	if err != nil || current.born != born || current.state != "Z" || current.parent != parent.Process.Pid {
		t.Fatal("fixture did not retain exact original zombie", current, err)
	}
	if err := tools.reapWatchdogChildren(); !errors.Is(err, unix.ECHILD) {
		t.Fatal("non-waitable zombie was accepted as reaped", err)
	}
	if _, err := fmt.Fprintln(input, "reap"); err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	if err := parent.Wait(); err != nil {
		t.Fatal(err)
	}
	reaped = true
	if _, err := readLinuxProcess(pid); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("original native parent failed to reap fixture zombie", err)
	}
}

// Run the subreaper in its own process; the test runner's children are unrelated.
func TestLinuxWatchdogAdoptionHelper(t *testing.T) {
	if len(helperArgs()) == 0 {
		return
	}
	if err := prepareWatchdogReaping(); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "daemon.pid")
	root := exec.Command("/bin/sh", "-c", `sleep 60 >/dev/null 2>&1 & echo $! > "$1"`, "fixture", pidFile)
	if err := root.Start(); err != nil {
		t.Fatal(err)
	}
	tools := newWatchdogTools(root.Process.Pid)
	defer tools.releaseHandles()
	if err := root.Wait(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if err := confirmWatchdogChildrenReaped(); err == nil {
		t.Fatal("unobserved adopted child accepted as fully reaped")
	}
	// This daemon was never seen while it was a descendant of the original root.
	identity, err := readLinuxProcess(pid)
	if err != nil || identity.parent != os.Getpid() {
		t.Fatal("fixture was not adopted", identity, err)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	defer func() {
		_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
		var info unix.Siginfo
		_ = unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED, nil)
	}()
	if err := (&pipeConnection{tools: tools}).freezeOwned(); err != nil {
		t.Fatal(err)
	}
	if tools.children[pid] != identity.born {
		t.Fatal("adopted daemon not captured")
	}
	if err := tools.killFencedChildren(); err != nil {
		t.Fatal(err)
	}
	if err := tools.reapWatchdogChildren(); err != nil {
		t.Fatal(err)
	}
	if _, err := readLinuxProcess(pid); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stop preceded adopted child reaping", err)
	}
}
func TestLinuxWatchdogCapturesDaemonAdoptedBetweenScans(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unrelated := exec.Command("/bin/sleep", "60")
	if err = unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestLinuxWatchdogAdoptionHelper$", "--", "adoption")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("adoption fixture failed: %v\n%s", err, output)
	}
	if err := unix.Kill(unrelated.Process.Pid, 0); err != nil {
		t.Fatal("unrelated process affected", err)
	}
}

func TestLinuxDaemonizingRuntimeHelper(t *testing.T) {
	args := helperArgs()
	if len(args) == 0 {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(args[0] + ".spawn"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("launch barrier unavailable")
		}
		time.Sleep(5 * time.Millisecond)
	}
	launcher := exec.Command("/bin/sh", "-c", `sleep 60 >/dev/null 2>&1 & echo $! > "$1"`, "fixture", args[0]+".tool")
	if err := launcher.Run(); err != nil {
		t.Fatal(err)
	}
	TestWorkerProcessHelper(t)
}

func TestLinuxWatchdogLossFixtureHelper(t *testing.T) {
	args := helperArgs()
	if len(args) == 0 {
		return
	}
	if err := prepareWatchdogReaping(); err != nil {
		t.Fatal(err)
	}
	helper, native, dir, pidFile := supervisorFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	body := "#!/bin/sh\nexec " + quote(executable) + " -test.run='^TestLinuxDaemonizingRuntimeHelper$' -- " + quote(pidFile) + " \"$@\"\n"
	if err = os.WriteFile(native, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "native.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	p, err := StartSupervisedProcess(t.Context(), SupervisedProcessOptions{HelperPath: helper, Binary: native, Directory: dir, Socket: filepath.Join(dir, "runtime.sock"), Kind: SupervisedCodexUnix, Stdout: logFile, Stderr: logFile})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop(context.Background())
	if err = os.WriteFile(pidFile+".spawn", nil, 0600); err != nil {
		t.Fatal(err)
	}
	daemon := 0
	for range 300 {
		raw, _ := os.ReadFile(pidFile + ".tool")
		daemon, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		if daemon > 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if daemon < 1 {
		body, _ := os.ReadFile(logPath)
		t.Fatalf("native daemon barrier missing, live=%v, output=%s", p.Live(), body)
	}
	identity, err := readLinuxProcess(daemon)
	if err != nil || identity.parent != p.cmd.Process.Pid {
		t.Fatal("daemon not adopted by actual watchdog", identity, err)
	}
	fd, err := unix.PidfdOpen(daemon, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	defer func() {
		_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
		var info unix.Siginfo
		_ = unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED, nil)
	}()
	p.tools.capture()
	if _, known := p.tools.children[daemon]; known {
		t.Fatal("parent fixture accidentally captured adopted daemon")
	}
	unrelated := exec.Command("/bin/sleep", "60")
	if err = unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
	killed := args[0] == "killed"
	if args[0] == "deadline" {
		if err = p.Renew(t.Context(), "fixture-deadline", 2*time.Second); err != nil {
			t.Fatal(err)
		}
		select {
		case <-p.exited:
		case <-time.After(4 * time.Second):
			t.Fatal("autonomous watchdog stop did not finish")
		}
	}
	if killed {
		if err = p.cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-p.exited:
		case <-time.After(3 * time.Second):
			t.Fatal("watchdog did not exit")
		}
	}
	stop, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	err = p.Stop(stop)
	if killed {
		if err == nil {
			t.Fatal("incomplete parent capture manufactured stop proof")
		}
		if again := p.Stop(t.Context()); again != err {
			t.Fatal("unknown stop result was replaced", again, err)
		}
		if !sameLiveProcess(daemon, identity.born) {
			t.Fatal("fixture no longer proves missing descendant")
		}
	} else if err != nil {
		t.Fatal("live watchdog could not prove complete stop", err)
	}
	if live, e := readLinuxProcess(p.PID()); e == nil && live.state != "Z" && live.state != "X" {
		t.Fatal("fallback did not clean the known native root", live)
	}
	if !killed {
		if _, err = readLinuxProcess(daemon); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("live watchdog did not reap adopted daemon", err)
		}
	}
	if err = unix.Kill(unrelated.Process.Pid, 0); err != nil {
		t.Fatal("unrelated fixture process affected", err)
	}
	// After watchdog death this dedicated test subreaper owns any root zombie.
	var info unix.Siginfo
	_ = unix.Waitid(unix.P_PID, p.PID(), &info, unix.WEXITED|unix.WNOHANG, nil)
}

func TestLinuxWatchdogLossCannotEraseAdoptedDaemonUncertainty(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"live", "killed", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestLinuxWatchdogLossFixtureHelper$", "--", mode)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("watchdog %s fixture failed: %v\n%s", mode, err, output)
			}
		})
	}
}
