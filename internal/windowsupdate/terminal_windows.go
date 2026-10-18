package windowsupdate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The packaged console helper is a pure observer launcher, without a Job tree.
// Stale helpers from a previous Bot lifetime can therefore be ended by their
// exact installation image. Runtime children and terminal hosts are not adopted.
func CloseTerminalHelpers(ctx context.Context, target string) error {
	if !filepath.IsAbs(target) {
		return errors.New("invalid terminal helper owner")
	}
	path := filepath.Join(target, "caelis-terminal.exe")
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "caelis-terminal.exe") {
			continue
		}
		handle, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, entry.ProcessID)
		if e != nil {
			continue
		}
		e = closeTerminalHelper(ctx, handle, path)
		windows.CloseHandle(handle)
		if e != nil {
			return e
		}
	}
	if err != nil && !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	return nil
}

func closeTerminalHelper(ctx context.Context, handle windows.Handle, path string) error {
	var image [32768]uint16
	length := uint32(len(image))
	if err := windows.QueryFullProcessImageName(handle, 0, &image[0], &length); err != nil {
		return nil
	}
	if !strings.EqualFold(filepath.Clean(windows.UTF16ToString(image[:length])), filepath.Clean(path)) {
		return nil
	}
	if err := windows.TerminateProcess(handle, 0); err != nil {
		if state, _ := windows.WaitForSingleObject(handle, 0); state != windows.WAIT_OBJECT_0 {
			return err
		}
	}
	for {
		state, err := windows.WaitForSingleObject(handle, 50)
		if err != nil || state == windows.WAIT_OBJECT_0 {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}
