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

func Write(path string, value any) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entry, _ := writers.LoadOrStore(path, make(chan struct{}, 1))
	gate := entry.(chan struct{})
	attempt.stage.Store(writeQueue)
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return attempt.failure(ctx.Err())
	}
	attempt.stage.Store(writeDispatch)
	done := make(chan error, 1)
	go func() { defer func() { <-gate }(); done <- writeBytes(path, body, attempt) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return attempt.failure(ctx.Err())
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
	entry, _ := writers.LoadOrStore(path, make(chan struct{}, 1))
	gate := entry.(chan struct{})
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
