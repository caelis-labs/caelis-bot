package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run the test executable as a real stdio child, including the version command.
// No model, credentials, network, timing-based readiness polling or mock process.
func fixtureBinary(t *testing.T, mode string) (string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binary, pid := filepath.Join(dir, "codex"), filepath.Join(dir, "pid")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	body := "#!/bin/sh\nexec " + quote(executable) + " -test.run='^TestAppServerHelper$' -- " + quote(mode) + " " + quote(pid) + " \"$@\"\n"
	if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return binary, pid
}

func TestAppServerHelper(t *testing.T) {
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
	mode, pidFile, command := args[0], args[1], args[2:]
	if command[0] == "--version" {
		version := TestedVersion
		switch mode {
		case "older-compatible":
			version = "0.1.0"
		case "newer-compatible":
			version = "99.0.0-next.1"
		case "no-version-command":
			os.Exit(2)
		}
		fmt.Println("codex-cli " + version)
		os.Exit(0)
	}
	if strings.Join(command, " ") != "app-server --listen stdio://" {
		os.Exit(2)
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(3)
	}
	if mode == "ignore-term" {
		signal.Ignore(syscall.SIGTERM)
	}
	if mode == "owned-tool" {
		child := exec.Command("/bin/sleep", "60")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if child.Start() != nil {
			os.Exit(11)
		}
		if os.WriteFile(pidFile+"-tool", []byte(strconv.Itoa(child.Process.Pid)), 0600) != nil {
			os.Exit(12)
		}
		go func() { _ = child.Wait() }()
	}
	d, e := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	var m wireMessage
	if d.Decode(&m) != nil || m.Method != "initialize" {
		os.Exit(4)
	}
	var params struct {
		ClientInfo   struct{ Name string } `json:"clientInfo"`
		Capabilities struct {
			Experimental bool  `json:"experimentalApi"`
			Attestation  *bool `json:"requestAttestation"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(m.Params, &params) != nil || params.ClientInfo.Name != "caelis_bot" || params.Capabilities.Experimental || params.Capabilities.Attestation == nil || *params.Capabilities.Attestation {
		os.Exit(5)
	}
	if mode == "stall-init" {
		for {
			time.Sleep(time.Hour)
		}
	}
	result := json.RawMessage(`{"codexHome":"/fixture","userAgent":"fixture","platformFamily":"unix","platformOs":"macos"}`)
	switch mode {
	case "bad-init":
		result = json.RawMessage(`{}`)
	case "minimal-init":
		result = json.RawMessage(`{"userAgent":"fixture"}`)
	case "additional-init-fields":
		result = json.RawMessage(`{"userAgent":"fixture","futureMetadata":{"extension":true}}`)
	case "wrong-type-init":
		result = json.RawMessage(`{"userAgent":42}`)
	case "unsupported-init":
		_ = e.Encode(wireMessage{ID: m.ID, Error: &NativeError{Code: -32601, Message: "unsupported"}})
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	if e.Encode(wireMessage{ID: m.ID, Result: result}) != nil {
		os.Exit(6)
	}
	m = wireMessage{}
	if d.Decode(&m) != nil || m.Method != "initialized" || len(m.ID) != 0 {
		os.Exit(7)
	}
	for {
		m = wireMessage{}
		err := d.Decode(&m)
		if mode == "ignore-term" && err == io.EOF {
			for {
				time.Sleep(time.Hour)
			}
		}
		if err != nil {
			os.Exit(0)
		}
		if m.Method != "account/read" || string(m.Params) != `{"refreshToken":false}` {
			os.Exit(8)
		}
		if mode == "exit-pending" {
			os.Exit(9)
		}
		if e.Encode(wireMessage{ID: m.ID, Result: json.RawMessage(`{"account":{"type":"chatgpt","email":"private@example.test","planType":"pro"},"requiresOpenaiAuth":true}`)}) != nil {
			os.Exit(10)
		}
		if mode == "final-response" {
			os.Exit(0)
		}
	}
}

func assertReaped(t *testing.T, file string) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned child not reaped: %v", err)
	}
}

func TestOwnedProcessHandshakeAuthAndClose(t *testing.T) {
	for _, mode := range []string{"normal", "final-response", "ignore-term"} {
		t.Run(mode, func(t *testing.T) {
			binary, pid := fixtureBinary(t, mode)
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			client, err := Start(ctx, Options{Binary: binary, Directory: t.TempDir(), CLIOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			cancel() // Startup context does not own the returned connection.
			status, err := client.ReadAuthStatus(testContext(t))
			if err != nil || !status.AccountPresent || status.AccountType != "chatgpt" {
				t.Fatalf("auth: %+v, %v", status, err)
			}
			client.Close()
			client.Close() // Idempotent, including a server that already exited.
			assertReaped(t, pid)
		})
	}
}

func TestCompatibleProtocolDoesNotRequireCLIReleaseMatch(t *testing.T) {
	for _, mode := range []string{"older-compatible", "newer-compatible", "no-version-command", "minimal-init", "additional-init-fields"} {
		t.Run(mode, func(t *testing.T) {
			binary, pid := fixtureBinary(t, mode)
			c, err := Start(testContext(t), Options{Binary: binary, CLIOnly: true})
			if err != nil {
				t.Fatal("compatible protocol rejected", err)
			}
			defer c.Close()
			if _, err := c.ReadAuthStatus(testContext(t)); err != nil {
				t.Fatal(err)
			}
			c.Close()
			assertReaped(t, pid)
		})
	}
}

func TestFailedProtocolHandshakeReapsProcess(t *testing.T) {
	for _, mode := range []string{"bad-init", "wrong-type-init", "unsupported-init", "stall-init"} {
		t.Run(mode, func(t *testing.T) {
			binary, pid := fixtureBinary(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			client, err := Start(ctx, Options{Binary: binary, CLIOnly: true})
			if err == nil {
				client.Close()
				t.Fatal("invalid startup accepted")
			}
			if mode != "stall-init" && !incompatibleProtocol(err) {
				t.Fatal("protocol failure not classified", err)
			}
			assertReaped(t, pid)
		})
	}
}

func TestOwnedProcessExitWhileRequestPending(t *testing.T) {
	binary, pid := fixtureBinary(t, "exit-pending")
	client, err := Start(testContext(t), Options{Binary: binary, CLIOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.ReadAuthStatus(testContext(t))
	var request *RequestError
	if !errors.As(err, &request) || !request.OutcomeUnknown || !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	client.Close()
	assertReaped(t, pid)
}

func TestOwnedProcessCleansDetachedToolButPreservesUnrelatedProcess(t *testing.T) {
	unrelated := exec.Command("/bin/sleep", "60")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
	binary, pid := fixtureBinary(t, "owned-tool")
	c, err := Start(testContext(t), Options{Binary: binary, CLIOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.captureTools()
	owner := c.rpc.conn.(*pipeConnection).tools
	owner.mu.Lock()
	staleBirth := owner.born
	staleBirth.Sec--
	owner.children[unrelated.Process.Pid] = staleBirth // A retained PID whose identity changed.
	owner.mu.Unlock()
	c.Close()
	if err := c.toolCleanupError(); err != nil {
		t.Fatal(err)
	}
	assertReaped(t, pid)
	// The fixture parent may exit before reaping; verify the exact birth identity
	// is no longer executing, rather than treating a short-lived zombie as work.
	b, err := os.ReadFile(pid + "-tool")
	if err != nil {
		t.Fatal(err)
	}
	child, _ := strconv.Atoi(string(b))
	if sameLiveProcess(child, owner.children[child]) {
		t.Fatal("owned detached tool still running")
	}
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("unrelated process was signalled")
	}
}
