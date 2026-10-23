package codex

import (
	"context"
	"errors"
	"os"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

var _ api.WorkInputValidator = (*Session)(nil)
var _ api.WorkInputValidator = (*WorkOwner)(nil)

// ValidateWorkFiles checks prepared host input without connecting or recording
// native work. The task ID belongs to the host route and is not native input.
// Dispatch still validates the original bytes and retains its normal limits.
func (*Session) ValidateWorkFiles(ctx context.Context, _ string, files []api.InputFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(files) > 8 {
		return errors.New("too_many_worker_files")
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := os.Lstat(file.Path)
		if err != nil || !entry.Mode().IsRegular() || entry.Size() > api.MaxWorkFileBytes {
			return errors.New("worker_file_unavailable_or_too_large")
		}
		// Capture identity from an actual handle before reading. Windows Lstat
		// can defer its file ID lookup until SameFile; after a rename that would
		// resolve both old and new snapshots through the replacement path.
		f, err := os.Open(file.Path)
		if err != nil {
			return errors.New("worker_file_unavailable_or_too_large")
		}
		before, statErr := f.Stat()
		closeErr := f.Close()
		if statErr != nil || closeErr != nil || !before.Mode().IsRegular() || before.Size() > api.MaxWorkFileBytes {
			return errors.New("worker_file_unavailable_or_too_large")
		}
		// Use the same bounded byte reader used to bind a native operation's
		// intent. No attachment copy, native request or receipt is prepared here.
		_, err = api.WorkInputDigests([]api.InputFile{file})
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if err != nil {
			return err
		}
		after, err := os.Lstat(file.Path)
		if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			return errors.New("worker_file_changed")
		}
	}
	return ctx.Err()
}

func (w *WorkOwner) ValidateWorkFiles(ctx context.Context, taskID string, files []api.InputFile) error {
	return w.engine.ValidateWorkFiles(ctx, taskID, files)
}
