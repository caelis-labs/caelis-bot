package codex

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"net"
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// Opt-in native standard handshake, no login changes, prompts or model calls.
func TestWindowsOwnedAppServerHandshake(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX_BINARY")
	if binary == "" {
		t.Skip("explicit native Codex acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	client, err := Start(ctx, Options{Binary: binary, Directory: t.TempDir(), CLIOnly: true, Attachable: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.retainedSocket() == "" {
		t.Fatal("native owner endpoint missing")
	}
	if id := client.privateOwnerIdentity(); id == nil || privateOwnerLifecycle(*id) != ownerAlive {
		t.Fatal("native owner birth identity missing")
	}
}

func TestWindowsOwnedAppServerSurvivesObserverReplacement(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX_BINARY")
	if binary == "" {
		t.Skip("explicit native Codex acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	old, err := Start(ctx, Options{Binary: binary, Directory: t.TempDir(), CLIOnly: true, Attachable: true})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	id := old.privateOwnerIdentity()
	if id == nil {
		t.Fatal("owner identity missing")
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(id.PID))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	defer windows.TerminateProcess(process, 1) // synthetic owner only; cleanup even if recovery fails.
	endpoint := old.retainedSocket()
	old.detachForReconnect()
	// Closing the old observer's last Job handle models application exit. The
	// replacement opens a fresh handle; no retained Go owner is handed across.
	owner := old.owner.(*windowsSocketConnection).owner
	if err = windows.CloseHandle(owner.job); err != nil {
		t.Fatal(err)
	}
	owner.once.Do(func() {}) // old.Close must not touch a recycled native handle.
	next, err := Start(ctx, Options{Socket: strings.TrimPrefix(endpoint, "unix://"), RequiredSocket: true})
	if err != nil {
		t.Fatal("replacement handshake", err)
	}
	defer next.Close()
	wrong := *id
	wrong.Birth++
	if next.reattachPrivateOwner(nil, endpoint, wrong) {
		t.Fatal("wrong process birth accepted")
	}
	if !next.reattachPrivateOwner(nil, endpoint, *id) {
		name, _ := ownerJobName(*id)
		job, _, jobErr := kernelOwner.NewProc("OpenJobObjectW").Call(0x0004|0x0008, 0, uintptr(unsafe.Pointer(name)))
		if job != 0 {
			windows.CloseHandle(windows.Handle(job))
		}
		t.Fatalf("original owner could not be recovered: peer=%+v expected=%+v life=%v job=%v error=%v", next.rpc.conn.(*socketConnection).peer, *id, privateOwnerLifecycle(*id), job != 0, jobErr)
	}
	if next.UsesSharedServer() {
		t.Fatal("private owner lost ownership")
	}
	next.Close()
	deadline := time.Now().Add(5 * time.Second)
	for privateOwnerLifecycle(*id) == ownerAlive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if privateOwnerLifecycle(*id) != ownerGone {
		t.Fatal("recovered owner did not terminate")
	}
}

func TestWindowsStdioDeadlineAndClose(t *testing.T) {
	conn, child, err := windowsStdioPipe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer child.Close()
	if err = conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Write(make([]byte, 1<<20)) // No child reader: force an actual blocked write.
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("blocked write did not time out: %v", err)
	}
	readDone := make(chan error, 1)
	go func() { _, err := conn.Read(make([]byte, 1)); readDone <- err }()
	conn.Close()
	select {
	case err = <-readDone:
		if err == nil {
			t.Fatal("closed pipe remained readable")
		}
	case <-time.After(time.Second):
		t.Fatal("close did not unblock read")
	}
}

// Settings uses the same standard handshake over stdio, then reads account and
// model metadata without initiating login or making a model call.
func TestWindowsInstalledSetup(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX_BINARY")
	if binary == "" {
		t.Skip("explicit native Codex acceptance")
	}
	client, err := Start(t.Context(), Options{Binary: binary, CLIOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	s := &Setup{}
	defer s.Close()
	v, err := s.Inspect(t.Context(), binary)
	if err != nil || (v.State != "ready" && v.State != "login") {
		t.Fatalf("setup state=%s: %v", v.State, err)
	}
}
