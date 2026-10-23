package caelis

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

var _ api.WorkInputValidator = (*Session)(nil)
var _ api.WorkInputValidator = (*WorkOwner)(nil)

// ValidateWorkFiles only reads the prepared host files. Native identity,
// configuration, uploads and operation receipts remain dispatch responsibilities.
func (*Session) ValidateWorkFiles(ctx context.Context, _ string, files []api.InputFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(files) > 4 {
		_, err := readWorkerInputs(files) // Preserve the native encoder's limit.
		return err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := readWorkerInputs([]api.InputFile{file})
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (w *WorkOwner) ValidateWorkFiles(ctx context.Context, taskID string, files []api.InputFile) error {
	return w.engine.ValidateWorkFiles(ctx, taskID, files)
}
