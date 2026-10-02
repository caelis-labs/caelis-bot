package codex

import (
	"errors"
	"syscall"
	"time"
)

func (o *ownedTools) killFencedChildren() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	var result error
	for pid, born := range o.children {
		live, err := ownedDarwinProcessLive(pid, born)
		result = errors.Join(result, err)
		if live {
			err = syscall.Kill(pid, syscall.SIGKILL)
			if !errors.Is(err, syscall.ESRCH) {
				result = errors.Join(result, err)
			}
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		live := false
		for pid, born := range o.children {
			running, err := ownedDarwinProcessLive(pid, born)
			result = errors.Join(result, err)
			live = live || running || err != nil
		}
		if !live {
			return result
		}
		if !time.Now().Before(deadline) {
			return errors.Join(result, errors.New("fenced descendant exit unconfirmed"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
