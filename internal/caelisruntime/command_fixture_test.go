package caelisruntime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Reuse the native test executable as a CLI, so lifecycle/credential tests
// exercise real argv and child environments on Windows as well as Unix.
func TestMain(m *testing.M) {
	if os.Getenv("CAELIS_RUNTIME_NATIVE_FIXTURE") == "1" && len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-test.") {
		if os.Getenv("CAELIS_RUNTIME_FIXTURE_RESTART") == "1" {
			fmt.Fprintln(os.Stderr, "PRIVATE_CONFIG_SENTINEL CAELIS_HOST_RESTART_REQUIRED PRIVATE_CONVERSATION_SENTINEL")
			os.Exit(1)
		}
		if os.Args[1] == "worker-tool" {
			fmt.Print(os.Getenv("RUNTIME_TOOL_SETTING"))
			os.Exit(0)
		}
		if os.Getenv("CAELIS_CONTROL_TOKEN") != "" || os.Getenv("CAELIS_CONTROL_URL") != "" || os.Getenv("CODEX_THREAD_ID") != "" {
			os.Exit(97)
		}
		child := exec.Command("worker-tool", "worker-tool")
		out, err := child.Output()
		if err != nil || string(out) != "native-export" {
			os.Exit(95)
		}
		log, err := os.OpenFile(os.Getenv("CAELIS_RUNTIME_TEST_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(94)
		}
		_, err = fmt.Fprintln(log, strings.Join(os.Args[1:], " "))
		if err = errors.Join(err, log.Close()); err != nil {
			os.Exit(93)
		}
		switch os.Args[1] {
		case "version":
			fmt.Println(`{"version":"0.fixture"}`)
		case "update":
			fmt.Println("caelis is up to date (0.fixture)")
		case "service":
			fmt.Println(`{"state":"running"}`)
		default:
			os.Exit(98)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func nativeCommandFixture(t *testing.T, dir, name string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAELIS_RUNTIME_NATIVE_FIXTURE", "1")
	return path
}
