package websurface

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

type authClock struct{ seconds atomic.Int64 }

func newAuthClock() *authClock {
	c := &authClock{}
	c.seconds.Store(time.Now().Add(-time.Hour).Unix())
	return c
}
func (c *authClock) now() time.Time          { return time.Unix(c.seconds.Load(), 0) }
func (c *authClock) advance(d time.Duration) { c.seconds.Add(int64(d / time.Second)) }
func clockServer(t *testing.T, c *authClock) *Server {
	return testServerConfig(t, func(config *Config) { config.now = c.now })
}
func restartAuthServer(t *testing.T, old *Server) *Server {
	t.Helper()
	old.Close()
	s, err := New(old.config, old.services, fstest.MapFS{"web.html": {Data: []byte("CaelisBot")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestRemotePairingHasNoBrowserCountLimit(t *testing.T) {
	clock := newAuthClock()
	s := clockServer(t, clock)
	var clients []*client
	for range 24 {
		clients = append(clients, pairClient(t, s))
		clock.advance(61 * time.Second) // Real entry rate limit still applies.
	}
	if len(s.auth.Sessions) != 24 {
		t.Fatal("pairing still has a device cap")
	}
	for _, c := range clients {
		if got := c.request("/api/v1/bootstrap", nil).Code; got != 200 {
			t.Fatal(got)
		}
	}
	// A valid one-time code is still required; an attacker cannot create records
	// just by sending bootstrap requests or guessing codes.
	for range 10 {
		(&client{s: s}).request("/api/v1/pair", map[string]string{"code": "wrong"})
	}
	if got := (&client{s: s}).request("/api/v1/pair", map[string]string{"code": "wrong"}).Code; got != 429 {
		t.Fatal("pairing rate limit lost", got)
	}
	if len(s.auth.Sessions) != 24 {
		t.Fatal("bad codes allocated grants")
	}
}

func TestRemoteIdleRenewalPersistsAndExpires(t *testing.T) {
	clock := newAuthClock()
	s := clockServer(t, clock)
	c := pairClient(t, s)
	key := digest(c.cookie.Value)
	originalExpiry := s.auth.Sessions[key].Expires
	path := filepath.Join(s.config.Directory, "sessions.json")
	before, _ := os.ReadFile(path)
	for range 9 {
		clock.advance(24 * time.Hour)
		w := c.request("/api/v1/bootstrap", nil)
		if w.Code != 200 {
			t.Fatal("active grant expired", w.Code)
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Value != c.cookie.Value || cookies[0].MaxAge != int(sessionIdleTTL/time.Second) {
			t.Fatal("cookie not renewed in place")
		}
	}
	if clock.now().Unix() <= originalExpiry {
		t.Fatal("test did not cross original deadline")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("requests wrote authentication state directly")
	}
	if !s.authDirty {
		t.Fatal("renewal not scheduled for persistence")
	}
	c.s = restartAuthServer(t, s) // Graceful close flushes the coalesced use.
	if got := c.request("/api/v1/bootstrap", nil).Code; got != 200 {
		t.Fatal("restart lost renewed grant", got)
	}
	if c.s.auth.Sessions[key].CSRF != c.csrf {
		t.Fatal("restart changed CSRF/identity")
	}
	clock.advance(sessionIdleTTL)
	if got := c.request("/api/v1/bootstrap", nil).Code; got != 401 {
		t.Fatal("idle grant survived", got)
	}
	c.s.flushAuth()
	if len(c.s.auth.Sessions) != 0 {
		t.Fatal("expired records not collected")
	}
	c.s = restartAuthServer(t, c.s)
	if len(c.s.auth.Sessions) != 0 {
		t.Fatal("expired records returned after restart")
	}
}

func TestDeniedRequestDoesNotRenewGrant(t *testing.T) {
	clock := newAuthClock()
	s := clockServer(t, clock)
	c := pairClient(t, s)
	key := digest(c.cookie.Value)
	expires := s.auth.Sessions[key].Expires
	clock.advance(time.Hour)
	c.csrf = "bad"
	if c.request("/api/v1/composer/claim", nil).Code != 403 {
		t.Fatal("CSRF accepted")
	}
	if s.auth.Sessions[key].Expires != expires || s.authDirty {
		t.Fatal("failed request extended idle deadline")
	}
}

func localRequest(t *testing.T, s *Server, method, path, csrf string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, testOrigin+path, bytes.NewBufferString(`{}`))
	r.RemoteAddr = "127.0.0.1:12345"
	if method != "GET" {
		r.Header.Set("Origin", testOrigin)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Caelis-Request", "1")
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func localCSRF(t *testing.T, s *Server) string {
	t.Helper()
	w := localRequest(t, s, "POST", "/api/v1/bootstrap", "", nil)
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatal("local bootstrap needs authentication", w.Code)
	}
	var v struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || v.CSRF == "" {
		t.Fatal("missing local CSRF")
	}
	return v.CSRF
}
func TestLocalRequestsNeedNoGrantAndRetainReceiptsAcrossRestart(t *testing.T) {
	s := testServerConfig(t, func(config *Config) { config.DirectLoopback = true })
	csrf := localCSRF(t, s)
	s.register(action{Path: "/api/v1/test/local-owner", Function: func(ctx context.Context) string { return sessionIDFromContext(ctx) }})
	// Exercise a real write and persisted operation; the operation's owner is
	// stable across restart, not tied to an ephemeral browser auth grant.
	r := httptest.NewRequest("POST", testOrigin+"/api/v1/test/local-owner", bytes.NewBufferString(`{"id":"local-receipt-identity","args":[]}`))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-Caelis-Request", "1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(s.auth.Sessions) != 0 {
		t.Fatal("local write allocated grant")
	}
	s = restartAuthServer(t, s)
	if localCSRF(t, s) != csrf {
		t.Fatal("restart broke open tab CSRF")
	}
	if got := localRequest(t, s, "GET", "/api/v1/operations/local-receipt-identity", "", nil).Code; got != 200 {
		t.Fatal("receipt identity lost", got)
	}
	if got := localRequest(t, s, "POST", "/api/v1/composer/claim", "bad", nil).Code; got != 403 {
		t.Fatal("local CSRF bypass", got)
	}
	if len(s.auth.Sessions) != 0 {
		t.Fatal("restart allocated grant")
	}
}
func sessionIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(sessionKey{}).(string)
	return v
}

func TestLocalMigrationRetainsOriginalRemoteReceiptOwner(t *testing.T) {
	clock := newAuthClock()
	s := clockServer(t, clock)
	c := pairClient(t, s)
	originalKey := digest(c.cookie.Value)
	s.operations["old-unknown"] = operation{Owner: originalKey, State: "pending", At: clock.now().Unix()}
	s.config.DirectLoopback = true
	// Existing clients retain CSRF and exact owner; no operation is resubmitted.
	if w := localRequest(t, s, "POST", "/api/v1/bootstrap", "", c.cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if len(s.auth.Sessions) != 1 {
		t.Fatal("migration allocated grant")
	}
	clock.advance(sessionIdleTTL)
	s.flushAuth()
	if len(s.auth.Sessions) != 0 {
		t.Fatal("legacy idle record not pruned")
	}
	if w := localRequest(t, s, "GET", "/api/v1/operations/old-unknown", "", c.cookie); w.Code != 200 {
		t.Fatal("local original owner lost", w.Code)
	}
	// The same expired cookie and a copied local CSRF grant no remote access.
	if w := c.request("/api/v1/bootstrap", nil); w.Code != 401 {
		t.Fatal("expired cookie authorized non-local peer", w.Code)
	}
}

func TestLocalBoundaryForReadsAndWrites(t *testing.T) {
	s := testServerConfig(t, func(c *Config) { c.DirectLoopback = true })
	for _, tc := range []struct {
		name, peer, host, origin, forwarded, site string
		want                                      int
	}{
		{"same origin GET without Origin", "127.0.0.1:1", "127.0.0.1:9317", "", "", "same-origin", 200},
		{"private peer spoofing Host", "10.0.0.2:1", "127.0.0.1:9317", testOrigin, "", "", 401},
		{"proxy loopback with forwarded metadata", "127.0.0.1:1", "127.0.0.1:9317", testOrigin, "198.51.100.1", "", 401},
		{"remote XFF loopback", "10.0.0.2:1", "127.0.0.1:9317", testOrigin, "127.0.0.1", "", 401},
		{"cross site", "127.0.0.1:1", "127.0.0.1:9317", "", "", "cross-site", 403},
		{"foreign Origin", "127.0.0.1:1", "127.0.0.1:9317", "https://evil.invalid", "", "", 403},
		{"foreign Host", "127.0.0.1:1", "evil.invalid", testOrigin, "", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", testOrigin+"/api/v1/bootstrap", nil)
			r.RemoteAddr = tc.peer
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, tc.want)
			}
		})
	}
	if len(s.auth.Sessions) != 0 {
		t.Fatal("boundary test allocated grants")
	}
}

func TestOpenEventStreamRenewsRemoteGrant(t *testing.T) {
	clock := newAuthClock()
	s := clockServer(t, clock)
	c := pairClient(t, s)
	h := httptest.NewServer(s)
	defer h.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", h.URL+"/api/v1/events", nil)
	r.Host = "127.0.0.1:9317"
	r.AddCookie(c.cookie)
	response, err := h.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	for day := range 9 {
		clock.advance(24 * time.Hour)
		s.eventMu.Lock()
		for stream := range s.subscribers {
			select {
			case <-stream:
			default:
			}
			stream <- []byte(`{"idleTest":true}`)
		}
		s.eventMu.Unlock()
		for scanner.Scan() {
			if scanner.Text() == `data: {"idleTest":true}` {
				break
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for {
			s.mu.Lock()
			expires := s.auth.Sessions[digest(c.cookie.Value)].Expires
			s.mu.Unlock()
			if expires == clock.now().Add(sessionIdleTTL).Unix() {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("stream did not renew", day)
			}
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
}

func TestExpiredLegacyLocalCSRFRefreshAdmitsOriginalWriteOnlyOnce(t *testing.T) {
	clock := newAuthClock()
	s := clockServer(t, clock)
	legacy := pairClient(t, s)
	s.config.DirectLoopback = true
	var writes atomic.Int32
	s.register(action{Path: "/api/v1/test/csrf-migration", Function: func() string { writes.Add(1); return "saved" }})
	post := func(csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", testOrigin+"/api/v1/test/csrf-migration", bytes.NewBufferString(`{"id":"original-before-csrf-refresh","args":[]}`))
		r.RemoteAddr = "127.0.0.1:1234" // Browser has removed the expired cookie.
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("X-Caelis-Request", "1")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	clock.advance(8 * 24 * time.Hour)
	s.flushAuth()
	for range 2 {
		w := post(legacy.csrf)
		if w.Code != 403 || !bytes.Contains(w.Body.Bytes(), []byte("csrf_denied")) {
			t.Fatal("stale CSRF bypassed", w.Code, w.Body.String())
		}
	}
	if writes.Load() != 0 || len(s.operations) != 0 {
		t.Fatal("CSRF denial admitted an effect")
	}
	csrf := localCSRF(t, s)
	if csrf == legacy.csrf {
		t.Fatal("test did not cross identity migration")
	}
	for range 2 {
		w := post(csrf)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if writes.Load() != 1 || len(s.operations) != 1 || len(s.auth.Sessions) != 0 {
		t.Fatal("refresh replaced identity, replayed, or allocated grant", writes.Load(), len(s.operations))
	}
	if w := legacy.request("/api/v1/bootstrap", nil); w.Code != 401 {
		t.Fatal("expired remote grant revived", w.Code)
	}
}
