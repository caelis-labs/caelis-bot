package codex

import "errors"

// The process birth value is OS-specific and only compared on the same host.
// A PID alone cannot distinguish the original owner from a reused PID.
type ownerProcessIdentity struct {
	PID   int    `json:"pid"`
	Birth uint64 `json:"birth"`
}

func (id *ownerProcessIdentity) valid() bool {
	return id != nil && id.PID > 1 && id.Birth != 0
}

type ownerLifecycle uint8

const (
	ownerUnknown ownerLifecycle = iota
	ownerAlive
	ownerGone
)

func (c *Client) privateOwnerIdentity() *ownerProcessIdentity {
	if c == nil || c.owner == nil {
		return nil
	}
	if source, ok := c.owner.(interface{ processIdentity() ownerProcessIdentity }); ok {
		id := source.processIdentity()
		if id.valid() {
			return &id
		}
	}
	return nil
}

// Called with s.mu held. An unreadable identity is never proof of absence.
func (s *Session) probePrivateOwnerLocked() ownerLifecycle {
	if !s.binding.OwnerProcess.valid() || s.binding.OwnerEndpoint == "" {
		return ownerUnknown
	}
	if s.ownerLifecycleProbe != nil {
		return s.ownerLifecycleProbe(*s.binding.OwnerProcess)
	}
	return privateOwnerLifecycle(*s.binding.OwnerProcess)
}

// Revalidate even a persisted OwnerUnavailable marker: an older process could
// have crashed after writing that marker but before stopping its owner.
func (s *Session) reconcilePrivateOwnerLocked() (ownerLifecycle, error) {
	life := s.probePrivateOwnerLocked()
	previous := s.binding
	s.ownerUnavailableVerified = false
	switch life {
	case ownerAlive:
		if s.binding.OwnerClosing || s.binding.OwnerUnavailable {
			s.binding.OwnerClosing, s.binding.OwnerUnavailable = false, false
			if err := s.save(); err != nil {
				s.binding = previous
				return ownerUnknown, err
			}
		}
		return life, nil
	case ownerGone:
		if !s.binding.OwnerUnavailable || s.binding.OwnerClosing {
			s.binding.OwnerClosing, s.binding.OwnerUnavailable = false, true
			if err := s.save(); err != nil {
				s.binding = previous
				return ownerUnknown, err
			}
		}
		s.ownerUnavailableVerified = true
		return life, nil
	default:
		return ownerUnknown, errors.New("原 owner 进程身份或存活状态无法核实；原请求保持未知，暂不建立新连接")
	}
}
