package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type deviceFixture struct {
	starts, cancels atomic.Int32
	logged          atomic.Bool
	reject          *NativeError
	malformed       bool
	lost            bool
}

func deviceSetup(t *testing.T, f *deviceFixture) *Setup {
	return setupFixture(t, func(_ *Setup, m wireMessage, c net.Conn) {
		result := `{}`
		switch m.Method {
		case "account/read":
			result = `{"account":null,"requiresOpenaiAuth":true}`
			if f.logged.Load() {
				result = `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`
			}
		case "account/login/start":
			if !strings.Contains(string(m.Params), `"chatgptDeviceCode"`) {
				t.Error("not native device-code login")
			}
			n := f.starts.Add(1)
			if f.reject != nil {
				writeWire(t, c, wireMessage{ID: m.ID, Error: f.reject})
				return
			}
			if f.lost {
				return
			}
			target := "https://auth.openai.com/codex/device"
			if f.malformed {
				target = "https://attacker.invalid/?secret=PRIVATE_SENTINEL"
			}
			result = fmt.Sprintf(`{"type":"chatgptDeviceCode","loginId":"native-%d","userCode":"TEST-1234","verificationUrl":%q,"device_code":"PRIVATE_SENTINEL","access_token":"PRIVATE_SENTINEL"}`, n, target)
		case "account/login/cancel":
			var args struct {
				LoginID string `json:"loginId"`
			}
			_ = json.Unmarshal(m.Params, &args)
			if args.LoginID != fmt.Sprintf("native-%d", f.starts.Load()) {
				t.Error("cancel lost native identity")
			}
			f.cancels.Add(1)
			result = `{"status":"canceled"}`
		case "model/list":
			result = `{"data":[{"model":"fixture","displayName":"Fixture"}]}`
		}
		writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(result)})
	})
}
func owner(t *testing.T, id string) context.Context {
	return api.WithSetupAccess(t.Context(), "web:"+id)
}
func deviceView(t *testing.T, s *Setup, ctx context.Context) api.SetupState {
	t.Helper()
	v, e := s.Inspect(ctx, "/fixture")
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestDeviceLoginOwnerDedupReadbackCancelAndRetry(t *testing.T) {
	f := &deviceFixture{}
	s := deviceSetup(t, f)
	a, b := owner(t, "a"), owner(t, "b")
	for range 2 {
		if _, e := s.Apply(a, "/fixture", "login", ""); e != nil {
			t.Fatal(e)
		}
	}
	v := deviceView(t, s, a)
	if !v.LoginPending || v.Login == nil || v.Login.UserCode != "TEST-1234" || v.Login.ExpiresAt == "" || f.starts.Load() != 1 {
		t.Fatal("missing owned challenge or duplicate start")
	}
	encoded, _ := json.Marshal(v)
	if strings.Contains(string(encoded), "PRIVATE_SENTINEL") || strings.Contains(string(encoded), "native-1") {
		t.Fatal("private native authority leaked")
	}
	foreign := deviceView(t, s, b)
	if foreign.Login.State != "other-session" || foreign.Login.UserCode != "" || foreign.Login.ID != "" {
		t.Fatal("challenge crossed authorization session")
	}
	if _, e := s.Apply(b, "/fixture", "login", ""); e == nil {
		t.Fatal("foreign start replaced login")
	}
	for _, ctx := range []context.Context{a, b} {
		if _, e := s.Apply(ctx, "/fixture", "cancel-login", "", "stale"); e == nil {
			t.Fatal("stale cancellation accepted")
		}
	}
	if _, e := s.Apply(b, "/fixture", "cancel-login", "", v.Login.ID); e == nil {
		t.Fatal("foreign cancellation accepted")
	}
	if _, e := s.Apply(a, "/fixture", "cancel-login", "", v.Login.ID); e != nil {
		t.Fatal(e)
	}
	v2 := deviceView(t, s, a)
	if v2.LoginPending || v2.Login.State != "cancelled" || v2.Login.UserCode != "" || f.cancels.Load() != 1 {
		t.Fatal("cancel retained challenge")
	}
	if _, e := s.Apply(a, "/fixture", "login", ""); e != nil {
		t.Fatal(e)
	}
	newer := deviceView(t, s, a)
	if newer.Login.ID == v.Login.ID || f.starts.Load() != 2 {
		t.Fatal("explicit retry did not create fresh attempt")
	}
	if _, e := s.Apply(a, "/fixture", "cancel-login", "", v.Login.ID); e == nil {
		t.Fatal("old tab cancelled new attempt")
	}
}
func TestDeviceCompletionFailureExpiryAndExistingLogin(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			f := &deviceFixture{}
			s := deviceSetup(t, f)
			ctx := owner(t, "a")
			_, _ = s.Apply(ctx, "/fixture", "login", "")
			s.mu.Lock()
			s.completed["unrelated"] = true
			s.mu.Unlock()
			if !deviceView(t, s, ctx).LoginPending {
				t.Fatal("unrelated completion adopted")
			}
			f.logged.Store(success)
			s.mu.Lock()
			s.completed["native-1"] = success
			s.mu.Unlock()
			v := deviceView(t, s, ctx)
			if v.LoginPending || v.Login.UserCode != "" {
				t.Fatal("terminal code retained")
			}
			if success && v.State != "ready" || !success && v.Login.State != "failed" {
				t.Fatal("wrong completion projection")
			}
			// A timer already queued before completion must not replace its result.
			s.expireDevice(s.epoch, v.Login.ID)
			if got := deviceView(t, s, ctx); got.Login.State != v.Login.State || f.cancels.Load() != 0 {
				t.Fatal("late expiry replaced a terminal result")
			}
		})
	}
	f := &deviceFixture{}
	s := deviceSetup(t, f)
	ctx := owner(t, "a")
	_, _ = s.Apply(ctx, "/fixture", "login", "")
	v := deviceView(t, s, ctx)
	s.expireDevice(s.epoch, v.Login.ID)
	if got := deviceView(t, s, ctx); got.LoginPending || got.Login.State != "expired" || got.Login.UserCode != "" || f.cancels.Load() != 1 {
		t.Fatal("expiry did not cancel original login")
	}
	f.logged.Store(true)
	if _, e := s.Apply(ctx, "/fixture", "login", ""); e != nil {
		t.Fatal(e)
	}
	if f.starts.Load() != 1 {
		t.Fatal("existing account replaced")
	}
	if _, e := s.Apply(ctx, "/fixture", "api-key", "PRIVATE_KEY"); e == nil {
		t.Fatal("existing account overwritten by key")
	}
}

func TestDeviceCodeHiddenWhileExpiryWaitsForManagementLock(t *testing.T) {
	s := deviceSetup(t, &deviceFixture{})
	ctx := owner(t, "a")
	_, _ = s.Apply(ctx, "/fixture", "login", "")
	s.mu.Lock()
	s.device.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
	s.mu.Unlock()
	v := deviceView(t, s, ctx)
	if !v.LoginPending || v.Login.State != "unknown" || v.Login.UserCode != "" || v.Login.VerificationURL != "" {
		t.Fatal("elapsed challenge exposed before cancellation confirmation")
	}
}
func TestDeviceUnsupportedAndMalformedNeverFallback(t *testing.T) {
	for _, code := range []int{-32601, -32602, -32600, -32000} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			f := &deviceFixture{reject: &NativeError{Code: code, Message: "PRIVATE_SENTINEL"}}
			s := deviceSetup(t, f)
			ctx := owner(t, "a")
			_, e := s.Apply(ctx, "/fixture", "login", "")
			if e != nil {
				t.Fatal("raw failure escaped")
			}
			v := deviceView(t, s, ctx)
			want := "unavailable"
			if code == -32601 || code == -32602 {
				want = "unsupported"
			}
			if v.Login.State != want || v.LoginPending || f.starts.Load() != 1 {
				t.Fatal("failure classification or automatic fallback")
			}
		})
	}
	f := &deviceFixture{malformed: true}
	s := deviceSetup(t, f)
	ctx := owner(t, "a")
	_, _ = s.Apply(ctx, "/fixture", "login", "")
	v := deviceView(t, s, ctx)
	if v.Login.State != "unknown" || v.Login.UserCode != "" || v.Login.VerificationURL != "" {
		t.Fatal("untrusted URL/code displayed")
	}
	_, _ = s.Apply(ctx, "/fixture", "login", "")
	if f.starts.Load() != 1 {
		t.Fatal("unknown effect retried")
	}
	_, e := s.Apply(ctx, "/fixture", "cancel-login", "", v.Login.ID)
	if e != nil || f.cancels.Load() != 1 {
		t.Fatal("malformed response did not retain cancel target")
	}
}
func TestLostDeviceStartRetainsAttemptUntilOwnedProcessCancellation(t *testing.T) {
	f := &deviceFixture{lost: true}
	s := deviceSetup(t, f)
	ctx := owner(t, "a")
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	_, _ = s.Apply(short, "/fixture", "login", "")
	v := deviceView(t, s, ctx)
	if v.Login.State != "unknown" || !v.LoginPending {
		t.Fatal("lost start erased uncertainty")
	}
	_, _ = s.Apply(ctx, "/fixture", "login", "")
	if f.starts.Load() != 1 {
		t.Fatal("lost start repeated")
	}
	original := s.client
	if _, e := s.Apply(ctx, "/fixture", "cancel-login", "", v.Login.ID); e != nil {
		t.Fatal(e)
	}
	select {
	case <-original.Done():
	default:
		t.Fatal("owned setup client not stopped")
	}
	if s.client != nil {
		t.Fatal("cancel left unknown owner reusable")
	}
}

func TestDeviceNativeEarlyCompletionNotification(t *testing.T) {
	logged := false
	s := setupFixture(t, func(_ *Setup, m wireMessage, c net.Conn) {
		switch m.Method {
		case "account/read":
			result := `{"account":null,"requiresOpenaiAuth":true}`
			if logged {
				result = `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`
			}
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(result)})
		case "account/login/start":
			logged = true
			writeWire(t, c, wireMessage{Method: "account/login/completed", Params: json.RawMessage(`{"loginId":"native-early","success":true,"error":null}`)})
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(`{"type":"chatgptDeviceCode","loginId":"native-early","verificationUrl":"https://auth.openai.com/codex/device","userCode":"TEST-1234"}`)})
		case "model/list":
			writeWire(t, c, wireMessage{ID: m.ID, Result: json.RawMessage(`{"data":[{"model":"fixture"}]}`)})
		}
	})
	ctx := owner(t, "a")
	_, err := s.Apply(ctx, "/fixture", "login", "")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		v := deviceView(t, s, ctx)
		if v.State == "ready" && v.Login.State == "complete" && v.Login.UserCode == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native completion was lost")
		}
		time.Sleep(time.Millisecond)
	}
}
