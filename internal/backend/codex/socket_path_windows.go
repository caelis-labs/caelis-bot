package codex

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

func windowsSocketRoots() []string {
	roots := []string{os.TempDir()}
	// Do not use an 8.3 alias to bypass the budget: Codex canonicalizes it before
	// binding. ProgramData supplies a short OS-owned fallback parent; the random
	// leaf still belongs only to the current user.
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, home)
	}
	if common, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0); err == nil {
		roots = append(roots, common)
	}
	return roots
}

func windowsSocketDirectory(roots []string) (string, string, error) {
	for _, root := range roots {
		// Match the native server's canonical path, including junctions and 8.3
		// expansion, before calculating the AF_UNIX UTF-8 byte budget.
		real, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		name, err := windows.UTF16PtrFromString(real)
		if err != nil {
			continue
		}
		long := make([]uint16, 32768)
		n, err := windows.GetLongPathName(name, &long[0], uint32(len(long)))
		if err != nil || n == 0 || n >= uint32(len(long)) {
			continue
		}
		root = windows.UTF16ToString(long[:n])
		if !filepath.IsAbs(root) || len(filepath.Join(root, "cb-4294967295", "s")) > 107 {
			continue
		}
		dir, err := os.MkdirTemp(root, "cb-")
		if err != nil {
			continue
		}
		path := filepath.Join(dir, "s")
		if len(path) <= 107 && localstate.ProtectDirectory(dir) == nil && localstate.IsPrivate(dir) {
			return dir, path, nil
		}
		_ = os.Remove(dir)
	}
	return "", "", errors.New("no writable private directory fits the Codex local socket address limit")
}
