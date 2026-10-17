package codex

import (
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsSocketPathUsesByteBudgetAndPrivateFallback(t *testing.T) {
	root := t.TempDir()
	long := filepath.Join(root, strings.Repeat("中文", 30))
	if err := os.Mkdir(long, 0700); err != nil {
		t.Fatal(err)
	}
	dir, path, err := windowsSocketDirectory(append([]string{long}, windowsSocketRoots()...))
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if len(path) > 107 || filepath.Dir(dir) == long || !localstate.IsPrivate(dir) {
		t.Fatal("socket path not bounded/private")
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal("native AF_UNIX address rejected", err)
	}
	l.Close()
	if _, _, err := windowsSocketDirectory([]string{long}); err == nil {
		t.Fatal("oversized path accepted")
	}
}
