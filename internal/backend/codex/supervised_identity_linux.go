package codex

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"time"
)

func (o *ownedTools) watchdogRootLive() (bool, error) {
	o.mu.Lock()
	pid, born := o.root, o.born
	o.mu.Unlock()
	p, err := readLinuxProcess(pid)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return p.born == born && p.state != "Z" && p.state != "X", nil
}
func (o *ownedTools) watchdogForceRoot() error {
	o.mu.Lock()
	fd, pid, born := o.rootHandle, o.root, o.born
	o.mu.Unlock()
	if fd < 0 {
		return errors.New("owned watchdog stable root unavailable")
	}
	if err := signalLinuxHandle(fd, pid, born, unix.SIGKILL); err != nil {
		return err
	}
	for range 100 {
		live, err := o.watchdogRootLive()
		if err != nil {
			return err
		}
		if !live {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("owned watchdog root exit unconfirmed")
}
