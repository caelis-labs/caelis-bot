// Package localstate persists small private product documents atomically.
package localstate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var writers sync.Map // One in-flight OS write per product document.

func writerGate(path string) chan struct{} {
	entry, _ := writers.LoadOrStore(path, make(chan struct{}, 1))
	return entry.(chan struct{})
}

// ReadAfterWrites reads a checkpoint after any writer that outlived its caller.
// A reopening owner must not rebuild state from a file about to be replaced.
func ReadAfterWrites(path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gate := writerGate(path)
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
		return os.ReadFile(path)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func Write(path string, value any) error {
	return write(path, value, 2*time.Second, writeBytes)
}

func write(path string, value any, timeout time.Duration, commit func(string, []byte, *writeAttempt) error) error {
	if path == "" {
		return nil
	}
	// Encode under the owner's lock; a timed-out disk writer never reads its
	// mutable maps later. Disk timeout is not a successful durable receipt.
	attempt := &writeAttempt{started: time.Now()}
	body, err := json.Marshal(value)
	if err != nil {
		return attempt.failure(err)
	}
	body = append(body, '\n')
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	gate := writerGate(path)
	attempt.stage.Store(writeQueue)
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return attempt.failure(ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		<-gate
		return attempt.failure(err)
	}
	attempt.stage.Store(writeDispatch)
	done := make(chan WriteCompletion, 1)
	go func() {
		defer func() { <-gate }()
		err := commit(path, body, attempt)
		done <- WriteCompletion{Err: err, Elapsed: time.Since(attempt.started)}
	}()
	select {
	case result := <-done:
		return result.Err
	case <-ctx.Done():
		failure := attempt.failure(ctx.Err())
		failure.Completion = done
		return failure
	}
}

// WriteConfirmed waits for the durable filesystem result. Use it for a
// publication boundary that must not release admission while a timed-out
// writer could still commit later. The caller owns the wait if storage stalls.
func WriteConfirmed(path string, value any) error {
	if path == "" {
		return nil
	}
	attempt := &writeAttempt{started: time.Now()}
	body, err := json.Marshal(value)
	if err != nil {
		return attempt.failure(err)
	}
	body = append(body, '\n')
	gate := writerGate(path)
	attempt.stage.Store(writeQueue)
	gate <- struct{}{}
	defer func() { <-gate }()
	return writeBytes(path, body, attempt)
}

func writeBytes(path string, body []byte, attempt *writeAttempt) error {
	if err := attempt.run(writeDirectory, func() error { return EnsurePrivateDirectory(filepath.Dir(path)) }); err != nil {
		return err
	}
	attempt.stage.Store(writeCreate)
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return attempt.failure(err)
	}
	defer os.Remove(f.Name())
	err = attempt.run(writeProtect, func() error { return ProtectOpenFile(f) })
	if err == nil {
		err = attempt.run(writeContent, func() error { _, e := f.Write(body); return e })
	}
	if err == nil {
		err = attempt.run(writeSync, f.Sync)
	}
	closed := attempt.run(writeClose, f.Close)
	if err == nil {
		err = closed
	}
	if err == nil {
		err = attempt.run(writeRename, func() error { return renameStateFile(f.Name(), path) })
	}
	if err == nil {
		err = attempt.run(writeParent, func() error { return SyncParent(path) })
	}
	return err
}
