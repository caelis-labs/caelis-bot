package hostfixture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processIdentity struct {
	pid, parent int
	birth       windows.Filetime
}

func (s *Store) capture(ctx context.Context) ([]processIdentity, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	all, live := map[int]processIdentity{}, map[int]processIdentity{}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid := int(entry.ProcessID)
		if pid <= 1 || pid == os.Getpid() {
			continue
		}
		handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, entry.ProcessID)
		if err != nil {
			if _, tracked := s.observed[pid]; tracked && !errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				return nil, fmt.Errorf("inspect tracked process %d: %w", pid, err)
			}
			continue
		}
		birth, active, err := windowsProcessState(handle)
		var args []string
		if active {
			args = windowsProcessArgs(handle)
		}
		windows.CloseHandle(handle)
		if err != nil {
			return nil, fmt.Errorf("inspect process %d: %w", pid, err)
		}
		if !active {
			continue
		}
		p := processIdentity{pid: pid, parent: int(entry.ParentProcessID), birth: birth}
		all[pid] = p
		privateExecutable := len(args) > 0 && strings.HasPrefix(strings.ToLower(args[0]), strings.ToLower(s.Root+string(filepath.Separator)))
		storeArg := slices.Index(args, "--store-dir")
		privateHost := len(args) > 1 && args[1] == "serve" && storeArg >= 0 && storeArg+1 < len(args) && strings.EqualFold(args[storeArg+1], s.Path)
		if privateExecutable || privateHost || s.observed[pid].birth == birth ||
			s.foreground != nil && !s.foreground.Exited() && s.foreground.PID == pid {
			live[pid] = p
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return s.recordDescendants(all, live), nil
}

func windowsProcessState(h windows.Handle) (windows.Filetime, bool, error) {
	var birth, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &birth, &exit, &kernel, &user); err != nil {
		return birth, false, err
	}
	state, err := windows.WaitForSingleObject(h, 0)
	return birth, state == uint32(windows.WAIT_TIMEOUT), err
}

func windowsProcessArgs(h windows.Handle) []string {
	var bytes uint32
	_ = windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, nil, 0, &bytes)
	if bytes < uint32(unsafe.Sizeof(windows.NTUnicodeString{})) || bytes > 1<<17 {
		return nil
	}
	buffer := make([]byte, bytes)
	if err := windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, unsafe.Pointer(&buffer[0]), bytes, &bytes); err != nil {
		return nil
	}
	line := (*windows.NTUnicodeString)(unsafe.Pointer(&buffer[0])).String()
	args, _ := windows.DecomposeCommandLine(line)
	return args
}

func (p processIdentity) kill(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(p.pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return os.ErrProcessDone
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	birth, active, err := windowsProcessState(h)
	if err != nil {
		return err
	}
	if !active || birth != p.birth {
		return os.ErrProcessDone
	}
	return windows.TerminateProcess(h, 1)
}
