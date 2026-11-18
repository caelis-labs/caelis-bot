//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/coder/websocket"
)

func recoveryDirectory(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "cb-recover-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeDaemonRecord(t *testing.T, dir, name, record string) {
	t.Helper()
	path := filepath.Join(dir, "app-server-daemon")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, name), []byte(record), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateSocketCannotStartSharedDaemon(t *testing.T) {
	if sharedHome("/tmp/private.sock") != "" || sharedHome("relative/app-server-control/app-server-control.sock") != "" {
		t.Fatal("private owner admitted for daemon recovery")
	}
}

func serveRecoverySocket(t *testing.T, path string, f *sessionFixture, home string) func() {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "initialize" {
			return map[string]string{"userAgent": "synthetic", "codexHome": home}, true
		}
		if m.Method == "turn/start" || m.Method == "thread/start" || m.Method == "turn/steer" || m.Method == "turn/interrupt" {
			t.Errorf("recovery replayed a native mutation: %s", m.Method)
		}
		return nil, false
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		defer ws.CloseNow()
		peer := websocket.NetConn(ctx, ws, websocket.MessageText)
		f.mu.Lock()
		f.peer = peer
		f.mu.Unlock()
		f.serve(peer)
	})}
	go func() { _ = server.Serve(l) }()
	return func() { _ = server.Close(); _ = l.Close() }
}

func TestSharedReconnectStartsDaemonAndReconcilesOriginalReceipts(t *testing.T) {
	for _, scenario := range []string{"automatic after disconnect", "manual without records", "startup with reused updater PID", "unhealthy listener"} {
		t.Run(scenario, func(t *testing.T) { checkSharedReconnect(t, scenario) })
	}
}

func checkSharedReconnect(t *testing.T, scenario string) {
	t.Helper()
	home := recoveryDirectory(t)
	socket := filepath.Join(home, "app-server-control", "app-server-control.sock")
	log, trigger, ready := filepath.Join(home, "calls"), filepath.Join(home, "trigger"), filepath.Join(home, "ready")
	bin := filepath.Join(home, "codex")
	t.Setenv("CODEX_HOME", filepath.Join(home, "wrong-current-profile"))
	t.Setenv("CODEX_THREAD_ID", "FOREIGN_THREAD")
	t.Setenv("RECOVERY_LOG", log)
	t.Setenv("RECOVERY_TRIGGER", trigger)
	t.Setenv("RECOVERY_READY", ready)
	script := "#!/bin/sh\n[ \"$*\" = 'app-server daemon start' ] || exit 90\n[ -z \"$CODEX_THREAD_ID\" ] || exit 91\nprintf '%s\\n' \"$CODEX_HOME\" >> \"$RECOVERY_LOG\"\ntouch \"$RECOVERY_TRIGGER\"\nwhile [ ! -f \"$RECOVERY_READY\" ]; do sleep 0.01; done\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	f := &sessionFixture{answers: make(chan wireMessage, 8), history: []nativeTurn{{ID: "original-turn", Status: "completed", Items: []nativeItem{{ID: "original-input", Type: "userMessage", ClientID: "original-request"}}}}, workers: map[string]nativeThread{
		"original-worker": {ID: "original-worker", Turns: []nativeTurn{{ID: "worker-turn", Status: "inProgress", Items: []nativeItem{{ID: "worker-input", Type: "userMessage", ClientID: "worker-request"}}}}},
	}}
	worker := f.workers["original-worker"]
	worker.Status.Type = "active"
	f.workers["original-worker"] = worker
	var unhealthy *http.Server
	switch scenario {
	case "startup with reused updater PID":
		writeDaemonRecord(t, home, "daemon-updater.pid", `{"pid":`+strconv.Itoa(os.Getpid())+`}`)
	case "unhealthy listener":
		if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		unhealthy = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})}
		go func() { _ = unhealthy.Serve(listener) }()
		t.Cleanup(func() { _ = unhealthy.Close() })
	}
	serverDone := make(chan func(), 1)
	go func() {
		for {
			if _, err := os.Stat(trigger); err == nil {
				break
			}
			select {
			case <-t.Context().Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
		if unhealthy != nil {
			_ = unhealthy.Close()
		}
		stop := serveRecoverySocket(t, socket, f, home)
		serverDone <- stop
		if err := os.WriteFile(ready, nil, 0600); err != nil {
			t.Error(err)
		}
	}()
	s := NewSession(SessionOptions{Binary: bin, Directory: home, StateFile: filepath.Join(home, "binding.json")})
	s.binding.OwnerEndpoint = "unix://" + socket
	s.binding.ThreadID = "thread-native"
	s.binding.Pending = &pendingSubmission{ID: "original-request"}
	s.binding.LastReceipt = &api.Receipt{ID: "original-request", Outcome: "unknown"}
	s.binding.Tasks = map[string]*taskRecord{"original-task": {
		Thread: "original-worker", Run: "worker-turn", View: api.Task{ID: "original-task", Status: "unknown"},
		Requests: map[string]taskReceipt{"worker-request": {Fingerprint: "original-fingerprint", Outcome: "unknown"}},
	}}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DetachForUpdate(context.Background()) })
	service := backend.NewService(s, nil, nil, nil, nil)
	switch scenario {
	case "automatic after disconnect":
		original := &sessionFixture{answers: make(chan wireMessage, 8)}
		stopOriginal := serveRecoverySocket(t, socket, original, home)
		t.Cleanup(stopOriginal)
		if err := service.Connect(testContext(t)); err != nil {
			t.Fatal(err)
		}
		stopOriginal()
		original.mu.Lock()
		_ = original.peer.Close()
		original.mu.Unlock()
	case "manual without records":
		if err := service.Reconnect(testContext(t)); err != nil {
			t.Fatal(err)
		}
	default:
		if err := service.Connect(testContext(t)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case stop := <-serverDone:
		t.Cleanup(stop)
	case <-testContext(t).Done():
		t.Fatal("shared daemon start did not restore the endpoint")
	}
	v := awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "ready" && v.LastReceipt.Outcome == "accepted" })
	if v.Connection != "ready" || v.LastReceipt.ID != "original-request" || v.LastReceipt.Outcome != "accepted" || !s.client.UsesSharedServer() {
		t.Fatal("original receipt or shared owner lost", v.LastReceipt, v.Connection)
	}
	awaitState(t, s, func(api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.binding.Tasks["original-task"].Requests["worker-request"].Outcome == "accepted"
	})
	if err := s.DetachForUpdate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	// Detach/quit leaves the shared listener alive for other clients.
	c, err := Start(testContext(t), Options{Binary: bin, Socket: socket, RequiredSocket: true, RecoverShared: true})
	if err != nil {
		t.Fatal("detach stopped shared runtime", err)
	}
	c.Close()
	b, err := os.ReadFile(log)
	if err != nil || string(b) != home+"\n" {
		t.Fatalf("wrong home or duplicate startup: %q, %v", b, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts != 0 || !f.resumeExcluded {
		t.Fatal("input replay or full history recovery")
	}
}

// The first public start stays in cooldown while the fast transport probes run.
// A wake or explicit reconnect must be able to start a repaired daemon before
// that cooldown expires, using the original resident binding and receipt.
func TestUnattendedSharedReconnect(t *testing.T) {
	for _, trigger := range []string{"timer", "wake", "button"} {
		t.Run(trigger, func(t *testing.T) {
			home := recoveryDirectory(t)
			socket := filepath.Join(home, "app-server-control", "app-server-control.sock")
			bin := filepath.Join(home, "codex")
			script := "#!/bin/sh\ncd \"$CODEX_HOME\" || exit 1\necho start >> calls\n[ -f repaired ] || exit 1\ntouch starting\nwhile [ ! -S app-server-control/app-server-control.sock ]; do sleep 0.01; done\n"
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			s := NewSession(SessionOptions{Binary: bin, Directory: home, StateFile: filepath.Join(home, "binding.json")})
			s.binding.OwnerEndpoint, s.binding.ThreadID = "unix://"+socket, "thread-native"
			s.binding.Pending = &pendingSubmission{ID: "request"}
			s.binding.LastReceipt = &api.Receipt{ID: "request", Outcome: "unknown"}
			s.reconnectDelay = func(attempt int) time.Duration {
				if attempt <= maxAutoReconnectAttempts {
					return time.Millisecond
				}
				if trigger == "timer" {
					return 100 * time.Millisecond
				}
				return time.Hour
			}
			if err := s.save(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.DetachForUpdate(context.Background()) })
			service := backend.NewService(s, nil, nil, nil, nil)
			if err := service.Connect(testContext(t)); err == nil {
				t.Fatal("unavailable endpoint reported ready")
			}
			awaitState(t, s, func(v api.Snapshot) bool {
				r := service.RecoveryState()
				return r.Supervised && r.Manual && v.ConnectionIssue == "shared_retry_wait"
			})
			// A caller that cannot take admission cannot abandon the only
			// observer. The timer/wake below must still recover this episode.
			s.op.Lock()
			blocked, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			err := s.Connect(blocked)
			cancel()
			s.op.Unlock()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("blocked manual admission: %v", err)
			}
			f := &sessionFixture{answers: make(chan wireMessage, 8), history: []nativeTurn{{ID: "original", Status: "completed", Items: []nativeItem{{ID: "item", Type: "userMessage", ClientID: "request"}}}}}
			if trigger == "timer" {
				stop := serveRecoverySocket(t, socket, f, home)
				t.Cleanup(stop)
			} else {
				if err := os.WriteFile(filepath.Join(home, "repaired"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				reconnected := make(chan error, 1)
				if trigger == "button" {
					go func() { reconnected <- service.Reconnect(testContext(t)) }()
				} else {
					s.RequestReconnectProbe()
				}
				deadline := testContext(t)
				for {
					if _, err := os.Stat(filepath.Join(home, "starting")); err == nil {
						break
					}
					select {
					case <-deadline.Done():
						t.Fatal("accelerated probe failed to start repaired daemon")
					case <-time.After(time.Millisecond):
					}
				}
				stop := serveRecoverySocket(t, socket, f, home)
				t.Cleanup(stop)
				if trigger == "button" {
					if err := <-reconnected; err != nil {
						t.Fatal(err)
					}
				}
			}
			awaitState(t, s, func(v api.Snapshot) bool {
				return v.Connection == "ready" && v.LastReceipt.ID == "request" && v.LastReceipt.Outcome == "accepted"
			})
			log, err := os.ReadFile(filepath.Join(home, "calls"))
			want := "start\nstart\n"
			if trigger == "timer" {
				want = "start\n"
			}
			if string(log) != want || err != nil {
				t.Fatalf("native starts: %q, %v", log, err)
			}
		})
	}
}

func TestCanceledSharedHandshakeHandsOffToResident(t *testing.T) {
	home := recoveryDirectory(t)
	s := NewSession(SessionOptions{Directory: home, StateFile: filepath.Join(home, "binding.json")})
	s.binding.OwnerEndpoint = "unix://" + filepath.Join(home, "app-server-control", "app-server-control.sock")
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
	entered := make(chan struct{})
	var attempts atomic.Int32
	s.start = func(ctx context.Context, _ Options) (*Client, error) {
		if attempts.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, ErrProtocol // New probe finds an actionable problem.
	}
	t.Cleanup(func() { _ = s.DetachForUpdate(context.Background()) })
	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Connect(ctx) }()
	select {
	case <-entered:
		cancel()
	case <-ctx.Done():
		t.Fatal("handshake did not start")
	}
	if err := <-done; err == nil {
		t.Fatal("canceled caller reported a connection")
	}
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.ConnectionIssue == "runtime_protocol" && !s.RecoveryState().Automatic
	})
	if attempts.Load() != 2 {
		t.Fatal("foreground cancellation lost or duplicated the unattended probe")
	}
}

func TestSharedRetryStopsForUserIntervention(t *testing.T) {
	for _, cause := range []error{ErrProtocol, errRuntimeMissing, os.ErrPermission} {
		t.Run(cause.Error(), func(t *testing.T) {
			home := recoveryDirectory(t)
			s := NewSession(SessionOptions{Directory: home, StateFile: filepath.Join(home, "binding.json")})
			s.binding.OwnerEndpoint = "unix://" + filepath.Join(home, "app-server-control", "app-server-control.sock")
			var attempts atomic.Int32
			s.start = func(context.Context, Options) (*Client, error) {
				attempts.Add(1)
				return nil, errors.Join(errExistingServer, cause)
			}
			s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
			t.Cleanup(func() { _ = s.DetachForUpdate(context.Background()) })
			if err := s.Connect(testContext(t)); err == nil {
				t.Fatal("unexpected ready state")
			}
			state := s.RecoveryState()
			if !state.Supervised || state.Automatic || !state.Manual {
				t.Fatal("actionable failure admitted to background retry", state)
			}
			s.RequestReconnectProbe()
			if attempts.Load() != 1 {
				t.Fatal("wake retried actionable failure")
			}
		})
	}
}

func TestSharedReconnectIntervals(t *testing.T) {
	s := &Session{}
	for attempt, want := range []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
		if got := s.retryDelay(attempt + 1); got != want {
			t.Fatalf("attempt %d delay %v, want %v", attempt+1, got, want)
		}
	}
}

func TestSharedStartMustRestoreOriginalEndpoint(t *testing.T) {
	home := recoveryDirectory(t)
	bin := filepath.Join(home, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(home, "app-server-control", "app-server-control.sock")
	client, err := Start(testContext(t), Options{Binary: bin, Socket: socket, RequiredSocket: true, RecoverShared: true})
	if client != nil || !errors.Is(err, errExistingServer) {
		t.Fatalf("start exited successfully but original endpoint is missing: client=%v err=%v", client != nil, err)
	}
}

func TestSharedRecoveryFailureBacksOffWithoutPrivateFallback(t *testing.T) {
	dir := recoveryDirectory(t)
	socket := filepath.Join(dir, "app-server-control", "app-server-control.sock")
	log, bin := filepath.Join(dir, "calls"), filepath.Join(dir, "codex")
	t.Setenv("RECOVERY_LOG", log)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$RECOVERY_LOG\"\necho PRIVATE_COMMAND_ERROR >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		_, err := Start(testContext(t), Options{Binary: bin, Directory: dir, Socket: socket, RequiredSocket: true, RecoverShared: true})
		if !errors.Is(err, errSharedStartFailed) || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(log)
	if err != nil || string(b) != "app-server daemon start\n" {
		t.Fatal("unbounded start or private fallback", string(b), err)
	}
}

func TestHealthyOrIncompatibleSharedSocketNeverStartsDaemon(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(strconv.FormatBool(valid), func(t *testing.T) {
			home := recoveryDirectory(t)
			socket := filepath.Join(home, "app-server-control", "app-server-control.sock")
			f := &sessionFixture{answers: make(chan wireMessage, 8)}
			stop := serveRecoverySocket(t, socket, f, home)
			defer stop()
			if !valid {
				f.mu.Lock()
				f.handle = func(m wireMessage) (any, bool) {
					if m.Method == "initialize" {
						return map[string]string{"userAgent": ""}, true
					}
					return nil, false
				}
				f.mu.Unlock()
			}
			bin, log := filepath.Join(home, "codex"), filepath.Join(home, "calls")
			t.Setenv("RECOVERY_LOG", log)
			if err := os.WriteFile(bin, []byte("#!/bin/sh\necho WRONG_START >> \"$RECOVERY_LOG\"\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			c, err := Start(testContext(t), Options{Binary: bin, Socket: socket, RequiredSocket: true, RecoverShared: true})
			if valid {
				if err != nil {
					t.Fatal(err)
				}
				c.Close()
			} else if !errors.Is(err, ErrProtocol) {
				t.Fatal(err)
			}
			if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("live or incompatible shared service was restarted", err)
			}
		})
	}
}

func TestOrdinarySharedClosePreservesEndpointAcrossBotRestart(t *testing.T) {
	home := recoveryDirectory(t)
	socket := filepath.Join(home, "app-server-control", "app-server-control.sock")
	f := &sessionFixture{answers: make(chan wireMessage, 8)}
	stop := serveRecoverySocket(t, socket, f, home)
	defer stop()
	opts := SessionOptions{Binary: filepath.Join(home, "no-private-cli"), Directory: home, StateFile: filepath.Join(home, "binding.json")}
	s := NewSession(opts)
	s.binding.OwnerEndpoint, s.binding.ThreadID = "unix://"+socket, "thread-native"
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
	loaded := NewSession(opts)
	if loaded.binding.OwnerEndpoint != "unix://"+socket {
		t.Fatal("ordinary exit lost the shared owner and could fall back to a private Runtime")
	}
	defer func() { _ = loaded.DetachForUpdate(context.Background()) }()
	if err := loaded.Connect(testContext(t)); err != nil {
		t.Fatal("Bot restart stopped or replaced shared Runtime", err)
	}
	if !loaded.client.UsesSharedServer() {
		t.Fatal("Bot restart owns a private service")
	}
}
