package taskterminal

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
)

// Update is the explicit stronger close action: end the original observer
// client, never the runtime Host, Worker, terminal application or a Job tree.
func (w *consoleWindow) CloseClientForUpdate(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if w.process == 0 {
		return nil
	} // Unclaimed descriptors are revoked by cancellation.
	if !w.observer {
		return ErrWindowUnsupported
	} // Setup TUIs may own execution.
	if state, err := windows.WaitForSingleObject(w.process, 0); err == nil && state == windows.WAIT_OBJECT_0 {
		return nil
	}
	pid, err := windows.GetProcessId(w.process)
	if err != nil || int(pid) != w.pid {
		return ErrWindowIdentity
	}
	birth, err := processBirth(w.process)
	if err != nil {
		return err
	}
	client, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(client)
	actual, err := processBirth(client)
	if err != nil || actual != birth {
		return ErrWindowIdentity
	}
	if err := windows.TerminateProcess(client, 0); err != nil {
		if state, _ := windows.WaitForSingleObject(client, 0); state != windows.WAIT_OBJECT_0 {
			return err
		}
	}
	for {
		state, err := windows.WaitForSingleObject(client, 50)
		if err != nil || state == windows.WAIT_OBJECT_0 {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (m *WindowManager) CloseClientsForUpdate(ctx context.Context) error {
	m.CancelAll()
	m.mu.Lock()
	entries := make([]*managedEntry, 0, len(m.entries))
	for _, entry := range m.entries {
		entries = append(entries, entry)
	}
	m.mu.Unlock()
	var result error
	for _, entry := range entries {
		// Cancellation drains confirmation/revocation before taking the original
		// binding lease. This is not list dismissal and does not remove task cards.
		entry.launcher.lockOperation()
		for _, window := range entry.launcher.windows {
			if client, ok := window.(interface{ CloseClientForUpdate(context.Context) error }); ok {
				result = errors.Join(result, client.CloseClientForUpdate(ctx))
			}
		}
		entry.launcher.mu.Unlock()
	}
	return result
}
