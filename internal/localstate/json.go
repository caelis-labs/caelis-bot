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
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entry, _ := writers.LoadOrStore(path, make(chan struct{}, 1))
	gate := entry.(chan struct{})
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() { defer func() { <-gate }(); done <- writeBytes(path, body) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func writeBytes(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(body)
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err == nil {
		err = closed
	}
	if err == nil {
		err = renameStateFile(f.Name(), path)
	}
	if err == nil {
		err = SyncParent(path)
	}
	return err
}
