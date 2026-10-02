//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

func (s *Session) startSupervised(ctx context.Context, opts Options) (*Client, error) {
	if !OwnedRuntimeSupported() {
		return nil, ErrOwnedRuntimeUnsupported
	}
	s.mu.Lock()
	helper, epoch, deadline := s.opts.WatchdogHelper, s.ownedEpoch, s.ownedDeadline
	s.mu.Unlock()
	if epoch == "" || time.Until(deadline) <= time.Second {
		return nil, errors.New("owned watchdog requires a live native grant before execution")
	}
	binary, err := runtimeBinary(opts.Binary)
	if err != nil {
		return nil, err
	}
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		inRead.Close()
		inWrite.Close()
		return nil, err
	}
	p, err := StartSupervisedProcess(ctx, SupervisedProcessOptions{HelperPath: helper, Binary: binary, Directory: opts.Directory, Kind: SupervisedCodexStdio, Stdin: inRead, Stdout: outWrite})
	inRead.Close()
	outWrite.Close()
	if err != nil {
		inWrite.Close()
		outRead.Close()
		return nil, fmt.Errorf("managed native launch: %w", err)
	}
	stop := func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_ = p.Stop(stopCtx)
	}
	if err = p.Renew(ctx, epoch, time.Until(deadline)); err != nil {
		stop()
		inWrite.Close()
		outRead.Close()
		return nil, fmt.Errorf("managed native grant install: %w", err)
	}
	s.mu.Lock()
	s.supervisor = p
	s.mu.Unlock()
	owned := p.tools
	conn := &pipeConnection{in: inWrite, out: outRead, tools: owned, forceStop: func() error {
		stopCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		return p.Stop(stopCtx)
	}}
	return initializeClient(ctx, conn, stop, opts)
}
