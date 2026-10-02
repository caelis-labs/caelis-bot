//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
	"github.com/coder/websocket"
)

// The native child fixture only initializes an App Server connection. Any
// thread/turn/model command is a failure unless the contained effect marker is present.
// It uses no account store or external network.
func TestWorkerCLINativeHelper(t *testing.T) {
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
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(b, &request) != nil {
				return
			}
			methods, err := os.OpenFile(args[0]+".methods", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				os.Exit(5)
			}
			_, _ = methods.WriteString(request.Method + "\n")
			_ = methods.Close()
			var result any
			switch request.Method {
			case "initialize":
				result = map[string]string{"userAgent": "worker-cli-synthetic"}
			case "initialized":
				continue
			case "config/read":
				result = map[string]any{"config": map[string]any{"model": "fixture-model", "model_reasoning_effort": "medium"}}
			case "thread/start", "thread/read", "thread/resume":
				if _, err := os.Stat(args[0] + ".allow-effects"); err != nil {
					os.Exit(6)
				}
				result = map[string]any{"thread": map[string]any{"id": "fixture-worker-thread", "status": map[string]string{"type": "idle"}}, "model": "fixture-model", "reasoningEffort": "medium"}
			case "turn/start":
				if _, err := os.Stat(args[0] + ".allow-effects"); err != nil {
					os.Exit(6)
				}
				result = map[string]any{"turn": map[string]any{"id": "fixture-worker-turn", "status": "inProgress", "items": []any{}}}
			case "account/read":
				result = map[string]any{"account": map[string]string{"type": "chatgpt"}, "requiresOpenaiAuth": true}
			default:
				os.Exit(6)
			}
			response, _ := json.Marshal(struct {
				ID     json.RawMessage `json:"id"`
				Result any             `json:"result"`
			}{request.ID, result})
			if ws.Write(r.Context(), websocket.MessageText, response) != nil {
				return
			}
		}
	})}
	_ = server.Serve(listener)
	os.Exit(0)
}

// A real native stdio proxy helper: EOF exits only this observer process.
func TestWorkerCLIProxyHelper(t *testing.T) {
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
	if err := run(context.Background(), os.Args[sep+1:], os.Stdout); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

type workerProxyStream struct {
	in   io.WriteCloser
	out  io.ReadCloser
	cmd  *exec.Cmd
	once sync.Once
}

func (s *workerProxyStream) Read(b []byte) (int, error)  { return s.out.Read(b) }
func (s *workerProxyStream) Write(b []byte) (int, error) { return s.in.Write(b) }
func (s *workerProxyStream) Close() error {
	s.once.Do(func() { _ = s.in.Close(); _ = s.out.Close(); _ = s.cmd.Process.Kill(); _ = s.cmd.Wait() })
	return nil
}

type workerReady struct {
	Version int             `json:"version"`
	Socket  string          `json:"socket"`
	Pair    workerwire.Pair `json:"pair"`
}
type workerReadyWriter struct {
	once  sync.Once
	ready chan workerReady
}

func (w *workerReadyWriter) Write(b []byte) (int, error) {
	var ready workerReady
	if json.Unmarshal(b, &ready) != nil {
		return 0, errors.New("invalid worker metadata")
	}
	w.once.Do(func() { w.ready <- ready })
	return len(b), nil
}

type noActivation struct{}

func (noActivation) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	return api.WorkDispatchSource{}, errors.New("no actual activation")
}
func workerTestBound(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func canonicalWorkerTestRoot(t *testing.T) string {
	t.Helper()
	temporary, err := os.MkdirTemp("/tmp", "caelis-wcli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporary) })
	root, err := filepath.EvalSymlinks(temporary)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}
func workerTestConfig(root, binary string) workerConfiguration {
	return workerConfiguration{Version: 1, Pair: workerwire.Pair{Target: api.WorkTarget{NodeID: "synthetic-node", Backend: "codex", Role: api.RoleWorker}, BotID: "bot-fixture", SourceNode: "local", SourceBackend: "caelis"}, Directory: filepath.Join(root, "owner"), Binary: binary, Socket: filepath.Join(root, "owner", "worker.sock"), Execution: api.WorkExecutionSettings{Model: "synthetic-model", Effort: "medium"}}
}
func saveWorkerTestConfig(t *testing.T, root string, c workerConfiguration) string {
	t.Helper()
	path := filepath.Join(root, "worker.json")
	b, _ := json.Marshal(c)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWorkerCLIConnectDetachDoesNotCreateBotAndSignalReapsOwner(t *testing.T) {
	root := canonicalWorkerTestRoot(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, pidFile := filepath.Join(root, "codex"), filepath.Join(root, "pid")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	if err = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestWorkerCLINativeHelper$' -- "+quote(pidFile)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	config := workerTestConfig(root, binary)
	file := saveWorkerTestConfig(t, root, config)
	unrelated, err := os.StartProcess("/bin/sleep", []string{"sleep", "60"}, &os.ProcAttr{Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Kill(); _, _ = unrelated.Wait() }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	output := &workerReadyWriter{ready: make(chan workerReady, 1)}
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"serve-worker", "--config-file", file}, output) }()
	var metadata workerReady
	select {
	case metadata = <-output.ready:
	case err = <-done:
		t.Fatal("worker owner failed before ready", err)
	case <-workerTestBound(t).Done():
		t.Fatal("worker readiness not observed")
	}
	if metadata.Pair != config.Pair || metadata.Socket != config.Socket || metadata.Version != 1 {
		t.Fatal("different pairing advertised", metadata)
	}
	nativePIDBytes, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(nativePIDBytes))
	if err != nil {
		t.Fatal(err)
	}
	connect := func() *workerwire.Client {
		t.Helper()
		stream, err := (&net.Dialer{}).DialContext(workerTestBound(t), "unix", config.Socket)
		if err != nil {
			t.Fatal(err)
		}
		client, err := workerwire.NewClient(workerTestBound(t), config.Pair, noActivation{}, stream)
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	client := connect()
	client.Close()
	if syscall.Kill(pid, 0) != nil {
		t.Fatal("observer detach reaped native owner")
	}
	// The actual proxy CLI gets only the existing private socket, no pairing or
	// owner options. Closing its pipes cannot construct/stop a runtime.
	proxy := exec.CommandContext(workerTestBound(t), executable, "-test.run=^TestWorkerCLIProxyHelper$", "--", "proxy-worker", "--socket", config.Socket)
	in, err := proxy.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := proxy.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	proxy.Stderr = io.Discard
	if err = proxy.Start(); err != nil {
		t.Fatal(err)
	}
	proxied, err := workerwire.NewClient(workerTestBound(t), config.Pair, noActivation{}, &workerProxyStream{in: in, out: out, cmd: proxy})
	if err != nil {
		_ = proxy.Process.Kill()
		_ = proxy.Wait()
		t.Fatal(err)
	}
	proxied.Close()
	if syscall.Kill(pid, 0) != nil {
		t.Fatal("proxy CLI detach reaped native owner")
	}
	client = connect()
	client.Close()
	info, err := os.Lstat(config.Socket)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("socket not bound privately before ready", err)
	}
	methods, err := os.ReadFile(pidFile + ".methods")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range strings.Fields(string(methods)) {
		if method != "initialize" && method != "initialized" && method != "account/read" {
			t.Fatal("connect created native Bot/task/model action", method)
		}
	}
	for _, name := range []string{"bot.json", "runtime.json", "conversation.json", "Notebook", "Product"} {
		if _, err = os.Lstat(filepath.Join(config.Directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("worker created resident state", name)
		}
	}
	originalPair, err := os.ReadFile(filepath.Join(config.Directory, "worker-pair.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored workerwire.Pair
	if json.Unmarshal(originalPair, &stored) != nil || stored != config.Pair {
		t.Fatal("pair not persisted before listener")
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal("native owner cleanup unconfirmed", err)
		}
	case <-workerTestBound(t).Done():
		t.Fatal("signal failed to reap owner")
	}
	if err = syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("native owner still alive", err)
	}
	if _, err = os.Lstat(config.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wire listener remains", err)
	}
	if syscall.Kill(unrelated.Pid, 0) != nil {
		t.Fatal("unrelated process affected")
	}
}

func TestWorkerCLIRejectsChangedRolePrivateRedirectAndSecretFields(t *testing.T) {
	root := canonicalWorkerTestRoot(t)
	config := workerTestConfig(root, "/bin/sh")
	file := saveWorkerTestConfig(t, root, config)
	if _, err := readWorkerConfiguration(file); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*workerConfiguration){func(c *workerConfiguration) { c.Pair.Target.Role = api.RoleBot }, func(c *workerConfiguration) { c.Pair.Target.Backend = "caelis" }, func(c *workerConfiguration) { c.Pair.BotID = "" }, func(c *workerConfiguration) { c.Socket = "relative" }, func(c *workerConfiguration) { c.Socket = filepath.Join(root, "foreign.sock") }} {
		invalid := config
		mutate(&invalid)
		if _, err := readWorkerConfiguration(saveWorkerTestConfig(t, root, invalid)); err == nil {
			t.Fatal("invalid worker owner config admitted")
		}
	}
	_ = saveWorkerTestConfig(t, root, config)
	b, _ := json.Marshal(config)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	raw["apiKey"] = "SECRET"
	b, _ = json.Marshal(raw)
	_ = os.WriteFile(file, b, 0600)
	if _, err := readWorkerConfiguration(file); err == nil {
		t.Fatal("credential field admitted")
	}
	_ = saveWorkerTestConfig(t, root, config)
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorkerConfiguration(file); err == nil {
		t.Fatal("public pairing config admitted")
	}
	if err := os.Chmod(file, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "config-link")
	_ = os.Symlink(file, link)
	if _, err := readWorkerConfiguration(link); err == nil {
		t.Fatal("config symlink admitted")
	}
	foreign := filepath.Join(root, "foreign")
	_ = os.Mkdir(foreign, 0700)
	_ = os.Symlink(foreign, config.Directory)
	if err := prepareWorkerDirectory(config.Directory); err == nil {
		t.Fatal("owner directory redirect admitted")
	}
	_ = os.Remove(config.Directory)
	_ = os.Mkdir(config.Directory, 0700)
	_ = os.WriteFile(filepath.Join(config.Directory, "bot.json"), []byte(`{"id":"old-resident"}`), 0600)
	if err := prepareWorkerDirectory(config.Directory); err == nil {
		t.Fatal("resident profile adopted as Worker")
	}
	for _, args := range [][]string{{"serve-worker"}, {"proxy-worker"}, {"proxy-worker", "--socket", "relative"}, {"serve-worker", "--config-file", file, "--pair", "untrusted"}} {
		if err := run(t.Context(), args, new(bytes.Buffer)); err == nil {
			t.Fatal("incomplete/authority-supplying CLI admitted")
		}
	}
}
