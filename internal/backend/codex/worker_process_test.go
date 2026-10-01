//go:build darwin || linux

package codex

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/coder/websocket"
)

// This is a real child process and standard private Unix/WebSocket App Server
// endpoint. No account secrets, external connection or model request are used.
func TestWorkerProcessHelper(t *testing.T) {
	sep := -1
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return
	}
	args := os.Args[sep+1:]
	if len(args) != 4 || args[1] != "app-server" || args[2] != "--listen" || !strings.HasPrefix(args[3], "unix://") {
		os.Exit(2)
	}
	if os.WriteFile(args[0], []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
		os.Exit(3)
	}
	listener, err := net.Listen("unix", strings.TrimPrefix(args[3], "unix://"))
	if err != nil {
		os.Exit(4)
	}
	server := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		for {
			_, b, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var m wireMessage
			if json.Unmarshal(b, &m) != nil {
				return
			}
			var result any
			switch m.Method {
			case "initialize":
				result = map[string]string{"userAgent": "owned-worker-fixture"}
			case "initialized":
				continue
			case "account/read":
				result = map[string]any{"account": map[string]string{"type": "chatgpt"}, "requiresOpenaiAuth": true}
			default:
				return
			}
			b, _ = json.Marshal(wireMessage{ID: m.ID, Result: raw(result)})
			if ws.Write(r.Context(), websocket.MessageText, b) != nil {
				return
			}
		}
	})}
	_ = server.Serve(listener)
	os.Exit(0)
}

func TestWorkerOwnedProcessSurvivesSocketLossAndStopsExactly(t *testing.T) {
	for _, removeEndpoint := range []bool{false, true} {
		t.Run(strconv.FormatBool(removeEndpoint), func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			binary, pidFile := filepath.Join(dir, "codex"), filepath.Join(dir, "pid")
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
			body := "#!/bin/sh\nexec " + quote(executable) + " -test.run='^TestWorkerProcessHelper$' -- " + quote(pidFile) + " \"$@\"\n"
			if err = os.WriteFile(binary, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			other := exec.Command("/bin/sleep", "60")
			if err = other.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = other.Process.Kill(); _ = other.Wait() }()
			source := &workerSourceFixture{value: api.WorkDispatchSource{NodeID: "host", Backend: "codex", BindingID: "native-binding", OperationID: "native-operation", Kind: "native_activation"}}
			w := NewWorker(WorkerOptions{Target: api.WorkTarget{NodeID: "worker", Backend: "codex", Role: api.RoleWorker}, Directory: filepath.Join(dir, "worker"), Binary: binary, Source: source})
			defer w.Close(testContext(t))
			if err = w.Connect(testContext(t)); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(b))
			if err != nil {
				t.Fatal(err)
			}
			for path, mode := range map[string]os.FileMode{w.endpoint: 0600, filepath.Dir(w.endpoint): 0700} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != mode {
					t.Fatal("private owner endpoint unavailable", err)
				}
			}
			w.engine.mu.Lock()
			c := w.engine.client
			w.engine.mu.Unlock()
			c.Close() // A transport EOF releases its observation socket only.
			if err = syscall.Kill(pid, 0); err != nil {
				t.Fatal("socket loss killed native owner", err)
			}
			if removeEndpoint {
				if err = os.Remove(w.endpoint); err != nil {
					t.Fatal(err)
				}
			}
			err = w.Connect(testContext(t))
			if removeEndpoint && err == nil {
				t.Fatal("missing original endpoint silently respawned")
			}
			if !removeEndpoint && err != nil {
				t.Fatal("original endpoint did not reconnect", err)
			}
			b, err = os.ReadFile(pidFile)
			if err != nil || string(b) != strconv.Itoa(pid) {
				t.Fatal("native owner was replaced", err)
			}
			_ = w.Close(testContext(t))
			_ = w.Close(testContext(t))
			if err = syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatal("original native child not reaped", err)
			}
			if err = syscall.Kill(other.Process.Pid, 0); err != nil {
				t.Fatal("unrelated process affected", err)
			}
			if _, err = os.Stat(filepath.Dir(w.endpoint)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("private endpoint directory leaked", err)
			}
		})
	}
}
