// Package codex owns the native App Server connection. It has no desktop/UI imports.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

var (
	ErrClosed        = errors.New("app server connection closed")
	ErrProtocol      = errors.New("invalid app server message")
	ErrEventOverflow = errors.New("app server notification consumer fell behind")
)

// NativeError preserves the original error without putting private backend text
// into ordinary logs. Callers must deliberately project Message/Data for the UI.
type NativeError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *NativeError) Error() string { return fmt.Sprintf("app server error %d", e.Code) }

func (e *NativeError) UnmarshalJSON(b []byte) error {
	var native struct {
		Code    *int            `json:"code"`
		Message *string         `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &native) != nil || native.Code == nil || native.Message == nil {
		return ErrProtocol
	}
	e.Code, e.Message, e.Data = *native.Code, *native.Message, native.Data
	return nil
}

// RequestError distinguishes failure before dispatch from an unobserved outcome.
// Cancellation of observation never means that the backend cancelled its work.
type RequestError struct {
	Method         string
	OutcomeUnknown bool
	Cause          error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("%s: %v (outcome unknown: %t)", e.Method, e.Cause, e.OutcomeUnknown)
}
func (e *RequestError) Unwrap() error { return e.Cause }

type Notification struct {
	Method    string
	Params    json.RawMessage
	RequestID json.RawMessage // Present only for a server request, in wire order.
	Sequence  uint64          // Connection-local generation; native IDs may be reused.
}
type wireMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *NativeError    `json:"error,omitempty"`
}
type response struct {
	result json.RawMessage
	err    error
}
type connection interface {
	io.ReadWriteCloser
	SetWriteDeadline(time.Time) error
}

type transport struct {
	diagnostics    *diagnosticlog.Logger
	conn           connection
	mu             sync.Mutex
	next           uint64
	pending        map[string]chan response
	terminal       error
	done           chan struct{}
	stopped        chan struct{}
	readDone       chan struct{}
	writeToken     chan struct{}
	events         chan Notification
	stop           func()
	handleRequests bool
	serverPending  map[string]serverRequestState
	serverSequence uint64
}
type serverRequestState struct {
	sequence uint64
	answered bool
}

func newTransport(conn connection, stop func()) *transport {
	return newTransportOptions(conn, stop, false)
}
func newTransportOptions(conn connection, stop func(), requests bool) *transport {
	return newTransportLogged(conn, stop, requests, nil)
}
func newTransportLogged(conn connection, stop func(), requests bool, diagnostics *diagnosticlog.Logger) *transport {
	t := &transport{conn: conn, pending: make(map[string]chan response), done: make(chan struct{}),
		stopped: make(chan struct{}), readDone: make(chan struct{}), writeToken: make(chan struct{}, 1), events: make(chan Notification, 64), stop: stop, diagnostics: diagnostics}
	t.handleRequests = requests
	t.serverPending = make(map[string]serverRequestState)
	t.writeToken <- struct{}{}
	go t.read()
	return t
}
func (t *transport) fail(err error) {
	t.mu.Lock()
	if t.terminal != nil {
		t.mu.Unlock()
		return
	}
	t.terminal = err
	close(t.done)
	t.mu.Unlock()
	if !errors.Is(err, ErrClosed) {
		reason := "transport disconnected; pending outcomes require reconciliation"
		if errors.Is(err, ErrEventOverflow) {
			reason = "notification queue overflow; execution observation incomplete"
		}
		if errors.Is(err, ErrProtocol) {
			reason = "invalid native wire envelope"
		}
		t.diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: "transport_failed", Reason: reason})
	}
	_ = t.conn.Close() // Releases a blocked writer/reader, including cancellation.
	go func() {
		if t.stop != nil {
			t.stop()
		}
		close(t.stopped)
	}()
}
func (t *transport) close()       { t.fail(ErrClosed); <-t.stopped; <-t.readDone }
func (t *transport) cause() error { t.mu.Lock(); defer t.mu.Unlock(); return t.terminal }

// send serializes JSONL writes. A context can cancel a blocked pipe write without
// leaving a goroutine holding the writer lock or interleaving another message.
func (t *transport) send(ctx context.Context, message wireMessage) (bool, error) {
	b, err := json.Marshal(message)
	if err != nil {
		return false, err
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-t.done:
		return false, t.cause()
	case <-t.writeToken:
	}
	defer func() { t.writeToken <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := t.cause(); err != nil {
		return false, err
	}
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := t.conn.SetWriteDeadline(deadline); err != nil {
		return false, err
	}
	cancelDone := make(chan struct{})
	cancel := context.AfterFunc(ctx, func() { _ = t.conn.SetWriteDeadline(time.Now()); close(cancelDone) })
	n, err := t.conn.Write(append(b, '\n'))
	if !cancel() {
		<-cancelDone
	}
	_ = t.conn.SetWriteDeadline(time.Time{})
	if err == nil && n != len(b)+1 {
		err = io.ErrShortWrite
	}
	if err != nil {
		t.fail(ErrClosed) // A partial frame cannot be safely reused or retried.
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}
	return true, err
}
func (t *transport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return nil, &RequestError{method, false, err}
	}
	t.mu.Lock()
	if t.terminal != nil {
		err = t.terminal
		t.mu.Unlock()
		return nil, &RequestError{method, false, err}
	}
	t.next++
	id := strconv.FormatUint(t.next, 10)
	ch := make(chan response, 1)
	t.pending[id] = ch
	t.mu.Unlock()
	defer func() { t.mu.Lock(); delete(t.pending, id); t.mu.Unlock() }()
	sent, err := t.send(ctx, wireMessage{ID: json.RawMessage(id), Method: method, Params: b})
	if err != nil {
		return nil, &RequestError{method, sent, err}
	}
	select {
	case reply := <-ch:
		return reply.result, reply.err
	case <-ctx.Done():
		err = ctx.Err()
	case <-t.done:
		err = t.cause()
	}
	// A response delivered before cancellation/EOF wins, even if select picked
	// the cancellation first. Late responses after removal cannot match a new call.
	t.mu.Lock()
	delete(t.pending, id)
	select {
	case reply := <-ch:
		t.mu.Unlock()
		return reply.result, reply.err
	default:
		t.mu.Unlock()
		return nil, &RequestError{method, true, err}
	}
}
func (t *transport) read() {
	defer close(t.readDone)
	defer close(t.events)
	scan := bufio.NewScanner(t.conn)
	scan.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scan.Scan() {
		var m wireMessage
		if err := json.Unmarshal(scan.Bytes(), &m); err != nil {
			t.diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: "wire_decode_failed", Reason: diagnosticlog.DecodeReason(err), Fingerprint: diagnosticlog.Fingerprint(scan.Bytes()), Bytes: len(scan.Bytes())})
			t.fail(ErrProtocol)
			return
		}
		if m.Method != "" {
			if len(m.Result) > 0 || m.Error != nil {
				t.fail(ErrProtocol)
				return
			}
			if len(m.ID) > 0 {
				// Plain transport clients reject requests using the ORIGINAL ID.
				// Session clients opt into ordered, generation-checked handling.
				if !validID(m.ID) {
					t.fail(ErrProtocol)
					return
				}
				if t.handleRequests {
					t.mu.Lock()
					_, duplicate := t.serverPending[string(m.ID)]
					t.serverSequence++
					sequence := t.serverSequence
					t.serverPending[string(m.ID)] = serverRequestState{sequence: sequence}
					t.mu.Unlock()
					if duplicate {
						t.fail(ErrProtocol)
						return
					}
					select {
					case t.events <- Notification{Method: m.Method, Params: m.Params, RequestID: m.ID, Sequence: sequence}:
					default:
						t.fail(ErrEventOverflow)
						return
					}
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := t.send(ctx, wireMessage{ID: m.ID, Error: &NativeError{Code: -32601, Message: "Client method is not implemented"}})
				cancel()
				if err != nil {
					t.fail(ErrClosed)
					return
				}
			} else {
				if m.Method == "serverRequest/resolved" {
					var resolved struct {
						RequestID json.RawMessage `json:"requestId"`
					}
					if json.Unmarshal(m.Params, &resolved) == nil {
						t.mu.Lock()
						delete(t.serverPending, string(resolved.RequestID))
						t.mu.Unlock()
					}
				}
				select {
				case t.events <- Notification{Method: m.Method, Params: m.Params}:
				default:
					t.fail(ErrEventOverflow)
					return
				}
			}
			continue
		}
		if !validID(m.ID) || (len(m.Result) == 0) == (m.Error == nil) {
			t.fail(ErrProtocol)
			return
		}
		t.mu.Lock()
		if ch := t.pending[string(m.ID)]; ch != nil {
			delete(t.pending, string(m.ID))
			var err error
			if m.Error != nil {
				err = m.Error
			}
			ch <- response{m.Result, err}
		}
		t.mu.Unlock()
	}
	if scan.Err() != nil {
		t.fail(ErrProtocol)
	} else {
		t.fail(ErrClosed)
	}
}

// Claim an exact native request once. A failed write is never retried as approval.
func (t *transport) respond(ctx context.Context, id json.RawMessage, sequence uint64, result any, nativeErr *NativeError) error {
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	t.mu.Lock()
	request, pending := t.serverPending[string(id)]
	if !pending || request.answered || request.sequence != sequence {
		t.mu.Unlock()
		return errors.New("request is no longer pending")
	}
	request.answered = true
	t.serverPending[string(id)] = request
	t.mu.Unlock()
	m := wireMessage{ID: id, Result: b, Error: nativeErr}
	if nativeErr != nil {
		m.Result = nil
	}
	sent, err := t.send(ctx, m)
	if err != nil {
		return &RequestError{"server response", sent, err}
	}
	return nil
}
func validID(id json.RawMessage) bool {
	var s string
	if len(id) > 0 && id[0] == '"' {
		return json.Unmarshal(id, &s) == nil
	}
	var n int64
	return len(id) > 0 && string(id) != "null" && json.Unmarshal(id, &n) == nil
}
