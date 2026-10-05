package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

type readFailureConn struct{ err error }

func (c *readFailureConn) Read([]byte) (int, error)         { return 0, c.err }
func (c *readFailureConn) Write(b []byte) (int, error)      { return len(b), nil }
func (c *readFailureConn) Close() error                     { return nil }
func (c *readFailureConn) SetWriteDeadline(time.Time) error { return nil }

func diagnosticFailure(t *testing.T, c connection, send func(), requests bool) (error, diagnosticlog.Record) {
	t.Helper()
	dir := t.TempDir()
	rpc := newTransportLogged(c, nil, requests, diagnosticlog.New(dir))
	if send != nil {
		send()
	}
	select {
	case <-rpc.readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("reader stalled")
	}
	got := rpc.cause()
	raw, err := os.ReadFile(filepath.Join(dir, "error.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE_SENTINEL") {
		t.Fatal("private wire bytes logged")
	}
	var record diagnosticlog.Record
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &record); err != nil {
		t.Fatal(err)
	}
	if record.Generation == 0 || record.Phase != "read" || record.Limit != maxWireFrame {
		t.Fatalf("missing safe connection facts: %+v", record)
	}
	rpc.close()
	return got, record
}

func TestReadFailureClassesPreserveUnknownOutcomeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name           string
		input, errorIs error
		code           string
	}{
		{"eof", io.EOF, ErrClosed, "closed_eof"},
		{"reset", syscall.ECONNRESET, ErrWebSocketReset, "websocket_reset"},
		{"io", errors.New("PRIVATE_SENTINEL /private/path"), ErrIO, "io_failure"},
		{"websocket close", ErrWebSocketClose, ErrWebSocketClose, "websocket_close"},
		{"frame limit", ErrFrameTooLarge, ErrFrameTooLarge, "frame_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, record := diagnosticFailure(t, &readFailureConn{err: tc.input}, nil, false)
			if !errors.Is(got, tc.errorIs) || record.Code != tc.code {
				t.Fatal(got, record)
			}
		})
	}
}

func TestWireFailuresAreJSONEnvelopeDuplicateOrFrameLimit(t *testing.T) {
	for _, tc := range []struct {
		name, wire, code string
		want             error
		requests         bool
	}{
		{"json", `{"id":1,"result":`, "json_decode_failed", ErrJSONDecode, false},
		{"envelope", `{"id":1,"result":1,"error":{"code":1,"message":"PRIVATE_SENTINEL"}}`, "invalid_envelope", ErrProtocol, false},
		{"duplicate", `{"id":7,"method":"approval","params":{}}` + "\n" + `{"id":7,"method":"approval","params":{}}`, "duplicate_server_request", ErrDuplicateRequest, true},
		{"oversize", strings.Repeat("x", maxWireFrame), "frame_too_large", ErrFrameTooLarge, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			got, record := diagnosticFailure(t, client, func() {
				go func() { _, _ = fmt.Fprintln(server, tc.wire) }()
			}, tc.requests)
			if !errors.Is(got, tc.want) || record.Code != tc.code {
				t.Fatal(got, record)
			}
			if tc.name == "oversize" && record.Bytes != maxWireFrame {
				t.Fatal(record)
			}
		})
	}
}
