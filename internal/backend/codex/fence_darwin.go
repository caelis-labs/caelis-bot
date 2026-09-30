package codex

import (
	"errors"
	"syscall"
)

// freezeOwned closes the spawn race before leased-owner termination. Authority
// comes only from the original launched root and captured birth identities.
func (p *pipeConnection) freezeOwned() error {
	o := p.tools
	live, err := ownedDarwinProcessLive(o.root, o.born)
	if err != nil {
		return err
	}
	if !live {
		return errors.New("owned runtime exited before fencing proof")
	}
	if err = syscall.Kill(o.root, syscall.SIGSTOP); err != nil {
		return err
	}
	for range 16 {
		o.capture()
		o.mu.Lock()
		n := len(o.children)
		for pid, born := range o.children {
			live, e := ownedDarwinProcessLive(pid, born)
			if e != nil {
				err = errors.Join(err, e)
				continue
			}
			if live {
				if e = syscall.Kill(pid, syscall.SIGSTOP); e != nil && !errors.Is(e, syscall.ESRCH) {
					err = errors.Join(err, e)
				}
			}
		}
		o.mu.Unlock()
		o.capture()
		o.mu.Lock()
		stable := n == len(o.children)
		failure := o.err
		o.mu.Unlock()
		if stable {
			return errors.Join(err, failure)
		}
	}
	return errors.Join(err, errors.New("owned descendant tree did not stabilize"))
}
