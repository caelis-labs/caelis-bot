package codex

import (
	"errors"
	"golang.org/x/sys/unix"
)

func (p *pipeConnection) freezeOwned() error {
	o := p.tools
	o.mu.Lock()
	rootHandle, root, born := o.rootHandle, o.root, o.born
	o.mu.Unlock()
	if rootHandle < 0 {
		return errors.New("owned runtime stable handle unavailable")
	}
	if err := signalLinuxHandle(rootHandle, root, born, unix.SIGSTOP); err != nil {
		return err
	}
	for range 16 {
		o.capture()
		o.mu.Lock()
		n := len(o.children)
		var err error
		for pid, born := range o.children {
			fd, ok := o.handles[pid]
			if !ok {
				err = errors.Join(err, errors.New("owned descendant stable handle unavailable"))
				continue
			}
			err = errors.Join(err, signalLinuxHandle(fd, pid, born, unix.SIGSTOP))
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
	return errors.New("owned descendant tree did not stabilize")
}
