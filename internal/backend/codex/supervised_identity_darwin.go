package codex

import (
	"errors"
	"syscall"
	"time"
)

func (o *ownedTools) watchdogRootLive() (bool, error) {
	o.mu.Lock()
	pid, born := o.root, o.born
	o.mu.Unlock()
	return ownedDarwinProcessLive(pid, born)
}
func (o *ownedTools) watchdogForceRoot() error {
	o.mu.Lock()
	pid, born := o.root, o.born
	o.mu.Unlock()
	live, err := ownedDarwinProcessLive(pid, born)
	if err != nil {
		return err
	}
	if live {
		err = syscall.Kill(pid, syscall.SIGKILL)
		if err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	for range 100 {
		live, err = ownedDarwinProcessLive(pid, born)
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
