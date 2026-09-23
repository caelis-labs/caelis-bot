package codex

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func setupFixture(t *testing.T, handler func(*Setup, wireMessage, net.Conn)) *Setup {
	t.Helper()
	s := &Setup{}
	s.start = func(ctx context.Context, _ Options) (*Client, error) {
		rpc, conn := pair(t)
		go func() {
			defer conn.Close()
			d := json.NewDecoder(conn)
			for {
				var m wireMessage
				if d.Decode(&m) != nil {
					return
				}
				handler(s, m, conn)
			}
		}()
		return &Client{rpc}, nil
	}
	t.Cleanup(s.Close)
	return s
}
func TestSetupLoginWaitsForMatchingCompletion(t *testing.T) {
	const id = "setup-login"
	logged := false
	s := setupFixture(t, func(_ *Setup, m wireMessage, c net.Conn) {
		switch m.Method {
		case "account/login/start":
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(`{"type":"chatgpt","loginId":"setup-login","authUrl":"https://auth.openai.com/authorize"}`)})
		case "account/read":
			logged = true
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(`{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`)})
		case "model/list":
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(`{"data":[{"model":"fixture","displayName":"Fixture"}]}`)})
		default:
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(`{}`)})
		}
	})
	u, e := s.Apply(t.Context(), "/fixture", "login", "")
	if e != nil || u == "" {
		t.Fatal(e)
	}
	v, e := s.Inspect(t.Context(), "/fixture")
	if e != nil || !v.LoginPending || v.State != "login" || !logged {
		t.Fatal(v, e)
	}
	// An unrelated login's completion cannot satisfy this attempt.
	s.mu.Lock()
	s.completed["other"] = true
	s.mu.Unlock()
	v, _ = s.Inspect(t.Context(), "/fixture")
	if !v.LoginPending {
		t.Fatal("wrong login completed setup")
	}
	s.mu.Lock()
	s.completed[id] = true
	s.mu.Unlock()
	v, e = s.Inspect(t.Context(), "/fixture")
	if e != nil || v.LoginPending || v.State != "ready" || len(v.Models) != 1 {
		t.Fatal(v, e)
	}
}
func TestSetupEarlyCompletionAndSecretErrorRedaction(t *testing.T) {
	s := setupFixture(t, func(s *Setup, m wireMessage, c net.Conn) {
		switch m.Method {
		case "account/login/start":
			if strings.Contains(string(m.Params), "apiKey") {
				writeWire(t, c, wireMessage{ID: m.ID, Error: &NativeError{Code: -32000, Message: "PRIVATE_SYNTHETIC_KEY"}})
				return
			}
			// Completion may arrive before the login/start response.
			s.mu.Lock()
			s.completed["early"] = true
			s.mu.Unlock()
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(`{"type":"chatgpt","loginId":"early","authUrl":"https://auth.openai.com/authorize"}`)})
		case "account/read":
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(`{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`)})
		case "model/list":
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(`{"data":[]}`)})
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, e := s.Apply(ctx, "/fixture", "login", ""); e != nil {
		t.Fatal(e)
	}
	if v, e := s.Inspect(ctx, "/fixture"); e != nil || v.LoginPending {
		t.Fatal(v, e)
	}
	_, e := s.Apply(ctx, "/fixture", "api-key", "PRIVATE_SYNTHETIC_KEY")
	if e == nil || strings.Contains(e.Error(), "PRIVATE_SYNTHETIC_KEY") {
		t.Fatal("secret-bearing error escaped")
	}
}
