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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

var (
	ErrClosed           = errors.New("app server connection closed")
	ErrProtocol         = errors.New("invalid app server message")
	ErrEventOverflow    = errors.New("app server notification consumer fell behind")
	ErrFrameTooLarge    = errors.New("app server frame too large")
	ErrJSONDecode       = errors.New("app server JSON decode failed")
	ErrDuplicateRequest = errors.New("duplicate app server request ID")
	ErrIO               = errors.New("app server I/O failure")
	ErrWebSocketClose   = errors.New("app server WebSocket closed")
	ErrWebSocketReset   = errors.New("app server WebSocket reset")
)

const maxWireFrame = 8 * 1024 * 1024
const maxQueuedEvents = 512
const maxQueuedEventBytes = 16 * 1024 * 1024

var nextTransportGeneration atomic.Uint64

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
	ReceivedAt  time.Time
	EmittedAtMS int64
	Method      string
	Params      json.RawMessage
	RequestID   json.RawMessage // Present only for a server request, in wire order.
	Sequence    uint64          // Connection-local generation; native IDs may be reused.
}
type wireMessage struct {
	EmittedAtMS int64           `json:"emittedAtMs,omitempty"`
	ID          json.RawMessage `json:"id,omitempty"`
	Method      string          `json:"method,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       *NativeError    `json:"error,omitempty"`
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
	pumpDone       chan struct{}
	writeToken     chan struct{}
	events         chan Notification
	queueMu        sync.Mutex
	queue          []Notification
	queueBytes     int
	queueHighWater int
	eventKinds     [4]uint64
	queueWake      chan struct{}
	stop           func()
	stopOnce       sync.Once
	handleRequests bool
	serverPending  map[string]serverRequestState
	serverSequence uint64
	generation     uint64
	sessionEpoch   atomic.Uint64
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
		stopped: make(chan struct{}), readDone: make(chan struct{}), pumpDone: make(chan struct{}), writeToken: make(chan struct{}, 1), events: make(chan Notification), queueWake: make(chan struct{}, 1), stop: stop, diagnostics: diagnostics}
	t.handleRequests = requests
	t.generation = nextTransportGeneration.Add(1)
	t.serverPending = make(map[string]serverRequestState)
	t.writeToken <- struct{}{}
	go t.pump()
	go t.read()
	return t
}
func (t *transport) fail(err error) {
	t.failWith(err, "operation", 0)
}
func (t *transport) failWith(err error, phase string, size int) {
	t.mu.Lock()
	if t.terminal != nil {
		t.mu.Unlock()
		return
	}
	t.terminal = err
	close(t.done)
	t.mu.Unlock()
	_ = t.conn.Close() // Release the wire before any diagnostic disk I/O.
	if phase == "read" || !errors.Is(err, ErrClosed) {
		depth, queued := t.queueStats()
		t.diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: transportCode(err),
			Reason: "pending outcomes require original-request reconciliation", Generation: t.generation, TransportGeneration: t.generation,
			SessionEpoch: t.sessionEpoch.Load(), Phase: phase, Bytes: size, Limit: maxWireFrame, QueueDepth: depth, QueueBytes: queued})
	}
}
func transportCode(err error) string {
	switch {
	case resourceExhausted(err):
		return "resource_exhausted"
	case errors.Is(err, ErrFrameTooLarge):
		return "frame_too_large"
	case errors.Is(err, ErrJSONDecode):
		return "json_decode_failed"
	case errors.Is(err, ErrDuplicateRequest):
		return "duplicate_server_request"
	case errors.Is(err, ErrProtocol):
		return "invalid_envelope"
	case errors.Is(err, ErrWebSocketReset):
		return "websocket_reset"
	case errors.Is(err, ErrWebSocketClose):
		return "websocket_close"
	case errors.Is(err, ErrIO):
		return "io_failure"
	case errors.Is(err, ErrEventOverflow):
		return "event_overflow"
	case errors.Is(err, ErrClosed):
		return "closed_eof"
	default:
		return "disconnected"
	}
}
func classifyReadError(err error) error {
	switch {
	case errors.Is(err, ErrFrameTooLarge), errors.Is(err, ErrProtocol), errors.Is(err, ErrWebSocketClose), errors.Is(err, ErrWebSocketReset), errors.Is(err, ErrIO), errors.Is(err, ErrClosed):
		return err
	case errors.Is(err, syscall.ECONNRESET):
		return ErrWebSocketReset
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrClosedPipe):
		return ErrClosed
	default:
		return errors.Join(ErrIO, err)
	}
}
func (t *transport) close()  { t.shutdown(true) }
func (t *transport) detach() { t.shutdown(false) }
func (t *transport) shutdown(stopOwner bool) {
	t.fail(ErrClosed)
	if stopOwner {
		t.stopOnce.Do(func() {
			if t.stop != nil {
				t.stop()
			}
			close(t.stopped)
		})
	}
	<-t.readDone
	<-t.pumpDone
}
func (t *transport) cause() error { t.mu.Lock(); defer t.mu.Unlock(); return t.terminal }

func (t *transport) queueStats() (int, int) {
	t.queueMu.Lock()
	defer t.queueMu.Unlock()
	return len(t.queue), t.queueBytes
}

func eventKind(n Notification) int {
	if len(n.RequestID) != 0 {
		return 0
	} // Native approval or input request.
	if strings.HasPrefix(n.Method, "turn/") || strings.HasPrefix(n.Method, "serverRequest/") || strings.HasPrefix(n.Method, "thread/status/") {
		return 1
	}
	if strings.Contains(n.Method, "/delta") || strings.Contains(n.Method, "tokenUsage") {
		return 2
	}
	return 3
}

var eventKindNames = [4]string{"server_request", "lifecycle", "delta", "other"}

func (t *transport) eventStats() (int, [4]uint64) {
	t.queueMu.Lock()
	defer t.queueMu.Unlock()
	return t.queueHighWater, t.eventKinds
}

func (t *transport) enqueue(n Notification) bool {
	if n.ReceivedAt.IsZero() {
		n.ReceivedAt = time.Now().Round(0)
	}
	size := len(n.Params) + len(n.Method) + len(n.RequestID)
	t.queueMu.Lock()
	if len(t.queue) >= maxQueuedEvents || t.queueBytes+size > maxQueuedEventBytes {
		t.queueMu.Unlock()
		return false
	}
	t.queue = append(t.queue, n)
	t.queueBytes += size
	t.eventKinds[eventKind(n)]++
	if len(t.queue) > t.queueHighWater {
		t.queueHighWater = len(t.queue)
	}
	t.queueMu.Unlock()
	select {
	case t.queueWake <- struct{}{}:
	default:
	}
	return true
}

// The wire reader never waits for the projection. A separate pump preserves
// order and holds at most one additional event while the consumer is slow.
func (t *transport) pump() {
	defer close(t.pumpDone)
	defer close(t.events)
	for {
		t.queueMu.Lock()
		var n Notification
		has := len(t.queue) > 0
		if has {
			n = t.queue[0]
		}
		t.queueMu.Unlock()
		if !has {
			select {
			case <-t.done:
				return
			case <-t.readDone:
				return
			case <-t.queueWake:
				continue
			}
		}
		select {
		case <-t.done:
			return
		case t.events <- n:
			t.queueMu.Lock()
			t.queue[0] = Notification{}
			t.queue = t.queue[1:]
			t.queueBytes -= len(n.Params) + len(n.Method) + len(n.RequestID)
			t.queueMu.Unlock()
		}
	}
}

// send serializes JSONL writes. A context can cancel a blocked pipe write without
// leaving a goroutine holding the writer lock or interleaving another message.
func (t *transport) send(ctx context.Context, message wireMessage) (bool, error) {
	return t.sendFrame(ctx, message, false)
}

func (t *transport) sendFrame(ctx context.Context, message wireMessage, finishFrame bool) (bool, error) {
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
	cancel := func() bool { return true }
	if !finishFrame {
		cancel = context.AfterFunc(ctx, func() { _ = t.conn.SetWriteDeadline(time.Now()); close(cancelDone) })
	}
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
			err = errors.Join(ctx.Err(), err)
		}
	}
	return true, err
}
func (t *transport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return t.request(ctx, method, params, false)
}

// observe cancels admission and response observation, but lets an admitted frame
// finish under its original write deadline. Shutdown can then reuse the writer
// for cleanup instead of destroying it by cancelling a background read request.
// An actual write failure still makes the transport unusable, even at zero bytes:
// a framed connection may have written bytes below this adapter boundary.
func (t *transport) observe(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return t.request(ctx, method, params, true)
}

func (t *transport) request(ctx context.Context, method string, params any, finishFrame bool) (json.RawMessage, error) {
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
	sent, err := t.sendFrame(ctx, wireMessage{ID: json.RawMessage(id), Method: method, Params: b}, finishFrame)
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
	scan := bufio.NewScanner(t.conn)
	scan.Buffer(make([]byte, 64*1024), maxWireFrame)
	for scan.Scan() {
		var m wireMessage
		if err := json.Unmarshal(scan.Bytes(), &m); err != nil {
			t.failWith(errors.Join(ErrProtocol, ErrJSONDecode), "read", len(scan.Bytes()))
			return
		}
		if m.Method != "" {
			if len(m.Result) > 0 || m.Error != nil {
				t.failWith(ErrProtocol, "read", len(scan.Bytes()))
				return
			}
			if len(m.ID) > 0 {
				// Plain transport clients reject requests using the ORIGINAL ID.
				// Session clients opt into ordered, generation-checked handling.
				if !validID(m.ID) {
					t.failWith(ErrProtocol, "read", len(scan.Bytes()))
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
						t.failWith(errors.Join(ErrProtocol, ErrDuplicateRequest), "read", len(scan.Bytes()))
						return
					}
					if !t.enqueue(Notification{Method: m.Method, Params: m.Params, RequestID: m.ID, Sequence: sequence}) {
						t.failWith(ErrEventOverflow, "read", len(scan.Bytes()))
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
				if !t.enqueue(Notification{Method: m.Method, Params: m.Params, EmittedAtMS: m.EmittedAtMS}) {
					t.failWith(ErrEventOverflow, "read", len(scan.Bytes()))
					return
				}
			}
			continue
		}
		if !validID(m.ID) || (len(m.Result) == 0) == (m.Error == nil) {
			t.failWith(ErrProtocol, "read", len(scan.Bytes()))
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
	if err := scan.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			t.failWith(ErrFrameTooLarge, "read", maxWireFrame)
		} else {
			t.failWith(classifyReadError(err), "read", 0)
		}
	} else {
		t.failWith(ErrClosed, "read", 0)
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
