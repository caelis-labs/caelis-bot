//go:build !windows

package hostfixture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type processIdentity struct {
	pid, parent int
	birth       string
}

var processLine = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\S+\s+\S+\s+\d+\s+\S+\s+\d+)\s+(\S+)\s+(.+)$`)

func (s *Store) capture(ctx context.Context) ([]processIdentity, error) {
	cmd := exec.CommandContext(ctx, "/bin/ps", "-ww", "-axo", "pid=,ppid=,lstart=,stat=,command=")
	cmd.WaitDelay = time.Second
	cmd.Env = []string{"LC_ALL=C"}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect private Store processes: %w", err)
	}
	all, live := map[int]processIdentity{}, map[int]processIdentity{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		match := processLine.FindStringSubmatch(line)
		if match == nil {
			// A process can exit between ps reading the identity and argv.
			continue
		}
		pid, _ := strconv.Atoi(match[1])
		ppid, _ := strconv.Atoi(match[2])
		if pid <= 1 || pid == os.Getpid() || strings.HasPrefix(match[4], "Z") {
			continue
		}
		p := processIdentity{pid: pid, parent: ppid, birth: match[3]}
		all[pid] = p
		command := match[5] + " "
		isHost := strings.Contains(command, " serve --store-dir "+s.Path+" ")
		isPrivateExecutable := strings.HasPrefix(command, s.Root+"/")
		if isHost || isPrivateExecutable || s.observed[pid].birth == p.birth ||
			s.foreground != nil && !s.foreground.Exited() && s.foreground.PID == pid {
			live[pid] = p
		}
	}
	return s.recordDescendants(all, live), nil
}

func (p processIdentity) kill(ctx context.Context) error {
	// Recheck birth immediately before signaling a process captured by ps.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(p.pid), "-o", "lstart=")
	cmd.Env = []string{"LC_ALL=C"}
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && len(out) == 0 {
		return os.ErrProcessDone
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != p.birth {
		return os.ErrProcessDone
	}
	if err := syscall.Kill(p.pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	} else {
		return err
	}
}
