package backend

import (
	"context"
	"errors"
	"time"
)

// ShutdownForUpdate releases this app's observation without converting active
// work or an unanswered decision into a user Stop. Adapters with live owners
// must implement DetachForUpdate and retain their original recovery targets.
func (s *Service) CanDetachForUpdate() error {
	if _, ok := s.engine.(interface{ DetachForUpdate(context.Context) error }); ok {
		if preparer, ok := s.engine.(interface{ PrepareDetachForUpdate(context.Context) error }); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			return preparer.PrepareDetachForUpdate(ctx)
		}
		return nil
	}
	v := s.engine.Snapshot()
	if v.CanInterrupt || v.Phase == "unknown" || v.Phase == "sending" || v.LastReceipt.Outcome == "unknown" {
		return errors.New("当前 Runtime 无法保存活跃工作以完成更新")
	}
	for _, approval := range v.Approvals {
		if approval.Status != "resolved" {
			return errors.New("当前 Runtime 无法保存待处理决定以完成更新")
		}
	}
	return nil
}

func (s *Service) ShutdownForUpdate() error {
	if err := s.CanDetachForUpdate(); err != nil {
		return err
	}
	s.recoveryMu.Lock()
	if s.recoveryFlight != nil {
		s.recoveryFlight.cancel()
	}
	s.recoveryMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if detacher, ok := s.engine.(interface{ DetachForUpdate(context.Context) error }); ok {
		return detacher.DetachForUpdate(ctx)
	}
	return s.engine.Close(ctx)
}
