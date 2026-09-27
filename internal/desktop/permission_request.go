package desktop

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Request outcomes never stand in for the current OS permission state. The UI
// rereads after an action and on focus; only that snapshot drives its switches.
type PermissionRequestResult struct {
	State string `json:"state"`
}

// A bounded asynchronous native callback: no system-settings jump while a consent
// dialog is pending, and no optimistic success on denial/error/cancellation.
func waitScreenPermission(ctx context.Context, poll func() (int, int64)) (PermissionRequestResult, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return PermissionRequestResult{}, err
		}
		status, code := poll()
		switch status {
		case 1:
			return PermissionRequestResult{State: "authorized"}, nil
		case 2:
			return PermissionRequestResult{State: "settingsRequired"}, nil
		case 3:
			return PermissionRequestResult{}, fmt.Errorf("screen permission request failed (%d)", code)
		case 4:
			return PermissionRequestResult{}, errors.New("screen permission is unsupported")
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}
