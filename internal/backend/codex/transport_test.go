package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func pair(t *testing.T) (*transport, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	rpc := newTransport(a, nil)
	t.Cleanup(func() { b.Close(); rpc.close() })
	return rpc, b
}
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func readWire(t *testing.T, d *json.Decoder) wireMessage {
	t.Helper()
	var m wireMessage
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}
func writeWire(t *testing.T, w net.Conn, m wireMessage) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(m); err != nil {
		t.Fatal(err)
	}
}

func TestInterleavedNotificationsAndResponsesKeepRequestIdentity(t *testing.T) {
	rpc, server := pair(t)
	ctx := testContext(t)
	results := make(chan string, 2)
	for _, method := range []string{"first", "second"} {
		go func() {
			result, err := rpc.call(ctx, method, map[string]string{"value": method})
			results <- fmt.Sprintf("%s:%s:%v", method, result, err)
		}()
	}
	d := json.NewDecoder(server)
	first, second := readWire(t, d), readWire(t, d)
	writeWire(t, server, wireMessage{Method: "item/event", Params: json.RawMessage(`{"runId":"run-a","itemId":"item-b"}`)})
	writeWire(t, server, wireMessage{ID: second.ID, Result: json.RawMessage(strconvQuote(second.Method))})
	writeWire(t, server, wireMessage{ID: first.ID, Result: json.RawMessage(strconvQuote(first.Method))})
	seen := map[string]bool{}
	for range 2 {
		select {
		case result := <-results:
			seen[result] = true
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	for _, method := range []string{"first", "second"} {
		if !seen[method+":"+strconvQuote(method)+":<nil>"] {
			t.Fatal(seen)
		}
	}
	event := <-rpc.events
	if event.Method != "item/event" || string(event.Params) != `{"runId":"run-a","itemId":"item-b"}` {
		t.Fatal(event)
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestCancellationBeforeAndAfterDispatchDoesNotReplay(t *testing.T) {
	rpc, server := pair(t)
	ctx, cancel := context.WithCancel(testContext(t))
	cancel()
	_, err := rpc.call(ctx, "before", nil)
	var request *RequestError
	if !errors.As(err, &request) || request.OutcomeUnknown || !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(testContext(t))
	done := make(chan error, 1)
	go func() { _, err := rpc.call(ctx, "mutation", nil); done <- err }()
	d := json.NewDecoder(server)
	sent := readWire(t, d)
	cancel()
	err = <-done
	if !errors.As(err, &request) || !request.OutcomeUnknown {
		t.Fatal(err)
	}
	// A late success is ignored; a subsequent request gets its own ID/result.
	writeWire(t, server, wireMessage{ID: sent.ID, Result: json.RawMessage(`"late"`)})
	nextCtx := testContext(t)
	go func() {
		b, err := rpc.call(nextCtx, "next", nil)
		if err == nil && string(b) != `"current"` {
			err = errors.New("late response crossed request identity")
		}
		done <- err
	}()
	next := readWire(t, d)
	if string(next.ID) == string(sent.ID) || next.Method != "next" {
		t.Fatal("request replay or ID reuse")
	}
	writeWire(t, server, wireMessage{ID: next.ID, Result: json.RawMessage(`"current"`)})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type writeObserved struct {
	net.Conn
	entered chan struct{}
}

func (c *writeObserved) Write(b []byte) (int, error) { close(c.entered); return c.Conn.Write(b) }
func TestBlockedWriteCancellationReleasesTransport(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	conn := &writeObserved{a, make(chan struct{})}
	rpc := newTransport(conn, nil)
	defer rpc.close()
	ctx, cancel := context.WithCancel(testContext(t))
	done := make(chan error, 1)
	go func() { _, err := rpc.call(ctx, "blocked", nil); done <- err }()
	<-conn.entered
	cancel() // Peer never reads; no timing-based retry/sleep.
	select {
	case err := <-done:
		var request *RequestError
		if !errors.As(err, &request) || !request.OutcomeUnknown || !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-testContext(t).Done():
		t.Fatal("writer leaked after cancellation")
	}
}
func TestNativeErrorsAndUnsupportedServerRequestsPreserveTargets(t *testing.T) {
	rpc, server := pair(t)
	ctx := testContext(t)
	d := json.NewDecoder(server)
	done := make(chan error, 1)
	go func() { _, err := rpc.call(ctx, "read", nil); done <- err }()
	req := readWire(t, d)
	writeWire(t, server, wireMessage{ID: json.RawMessage(`"approval-native-42"`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"itemId":"native-item"}`)})
	rejection := readWire(t, d)
	if string(rejection.ID) != `"approval-native-42"` || rejection.Error == nil || rejection.Error.Code != -32601 || len(rejection.Result) != 0 {
		t.Fatal(rejection)
	}
	writeWire(t, server, wireMessage{ID: req.ID, Error: &NativeError{Code: 123, Message: "private backend explanation", Data: json.RawMessage(`{"native":"details"}`)}})
	err := <-done
	var native *NativeError
	if !errors.As(err, &native) || native.Code != 123 || native.Message != "private backend explanation" || string(native.Data) != `{"native":"details"}` {
		t.Fatal("native error lost")
	}
	if strings.Contains(err.Error(), "private") {
		t.Fatal("private message leaked through ordinary error logging")
	}
}
func TestEOFAndMalformedMessagesDoNotReportSuccess(t *testing.T) {
	for _, message := range []string{"", `not json`, `{"id":1,"result":{},"error":{"code":1,"message":"both"}}`, `{"id":null,"result":{}}`, `{"id":1,"error":{}}`} {
		t.Run(message, func(t *testing.T) {
			rpc, server := pair(t)
			ctx := testContext(t)
			done := make(chan error, 1)
			go func() { _, err := rpc.call(ctx, "pending", nil); done <- err }()
			readWire(t, json.NewDecoder(server))
			if message == "" {
				server.Close()
			} else {
				fmt.Fprintln(server, message)
			}
			err := <-done
			var request *RequestError
			if !errors.As(err, &request) || !request.OutcomeUnknown {
				t.Fatal(err)
			}
		})
	}
}
func TestNotificationOverflowFailsInsteadOfSilentlyDropping(t *testing.T) {
	rpc, server := pair(t)
	for i := 0; i < 65; i++ {
		writeWire(t, server, wireMessage{Method: "item/event"})
	}
	select {
	case <-rpc.done:
		if !errors.Is(rpc.cause(), ErrEventOverflow) {
			t.Fatal(rpc.cause())
		}
	case <-testContext(t).Done():
		t.Fatal("overflow not observed")
	}
}
func TestAuthProjectionDoesNotExposePrivateIdentityOrInventReadiness(t *testing.T) {
	for _, body := range []string{
		`{"requiresOpenaiAuth":true}`,
		`{"account":null,"requiresOpenaiAuth":true}`,
		`{"account":null,"requiresOpenaiAuth":false}`,
		`{"account":{"type":"chatgpt","email":"private@example.test","planType":"pro"},"requiresOpenaiAuth":true}`,
		`{"account":{"type":"apiKey"},"requiresOpenaiAuth":true}`,
		`{"account":{"type":"amazonBedrock","usesCodexManagedCredentials":false},"requiresOpenaiAuth":false}`,
	} {
		status, err := projectAuth([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(status)
		if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "planType") {
			t.Fatal("private identity leaked")
		}
		if status.AccountPresent != strings.Contains(body, `"type"`) {
			t.Fatal(status)
		}
	}
	for _, body := range []string{`{}`, `{"account":null}`, `{"account":{"type":"newMode"},"requiresOpenaiAuth":true}`} {
		if _, err := projectAuth([]byte(body)); err == nil {
			t.Fatal("unknown auth state accepted")
		}
	}
}
