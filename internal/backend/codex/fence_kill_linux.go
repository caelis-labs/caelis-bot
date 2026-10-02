package codex

import (
	"errors"
	"golang.org/x/sys/unix"
	"time"
)

func (o *ownedTools) killFencedChildren() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	var result error
	for pid, born := range o.children {
		fd, ok := o.handles[pid]
		if !ok {
			return errors.New("fenced descendant stable handle unavailable")
		}
		result = errors.Join(result, signalLinuxHandle(fd, pid, born, unix.SIGKILL))
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		live := false
		for pid, born := range o.children {
			running, err := linuxOwnedHandleLive(o.handles[pid], pid, born)
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
