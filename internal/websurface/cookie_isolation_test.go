package websurface

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Real HTTP and the standard browser cookie rules: ports do NOT isolate cookies.
// Two empty temporary Bot profiles must coexist on localhost without logging
// each other out on the 10-second composer lease renewal.
func TestLocalProfilesKeepIndependentBrowserSessions(t *testing.T) {
	a, b := testServer(t), testServer(t)
	serve := func(s *Server) *httptest.Server {
		h := httptest.NewServer(s)
		t.Cleanup(h.Close)
		u, _ := url.Parse(h.URL)
		s.origins = map[string]bool{h.URL: true}
		s.hosts = map[string]bool{u.Host: true}
		s.config.DirectLoopback = true
		return h
	}
	ah, bh := serve(a), serve(b)
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	request := func(base, path, csrf string) map[string]any {
		t.Helper()
		r, _ := http.NewRequest("POST", base+path, bytes.NewBufferString(`{}`))
		r.Header.Set("Origin", base)
		r.Header.Set("X-Caelis-Request", "1")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("X-Caelis-Tab", "0123456789abcdef0123456789abcdef")
		resp, err := browser.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d, %s", path, resp.StatusCode, data)
		}
		var v map[string]any
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	av := request(ah.URL, "/api/v1/bootstrap", "")
	bv := request(bh.URL, "/api/v1/bootstrap", "")
	for range 3 {
		request(ah.URL, "/api/v1/composer/claim", av["csrf"].(string))
		request(bh.URL, "/api/v1/composer/claim", bv["csrf"].(string))
	}
	if len(a.auth.Sessions) != 0 || len(b.auth.Sessions) != 0 {
		t.Fatal("local requests created persistent grants")
	}
}

func TestLegacySessionMigratesOnlyItsOwningProfile(t *testing.T) {
	a, b := testServer(t), testServer(t)
	c := pairClient(t, a)
	original := c.cookie.Value
	c.cookie.Name = "caelisbot_session"
	migrated := c.request("/api/v1/bootstrap", nil)
	if migrated.Code != 200 || len(migrated.Result().Cookies()) != 1 {
		t.Fatal("legacy owner could not migrate")
	}
	fresh := migrated.Result().Cookies()[0]
	if fresh.Name != a.cookieName || fresh.Value != original || len(a.auth.Sessions) != 1 {
		t.Fatal("migration replaced the owned session")
	}
	c.s = b
	if got := c.request("/api/v1/bootstrap", nil); got.Code != 401 {
		t.Fatal("another profile accepted legacy authority", got.Code)
	}
	c.s = a
	c.cookie = fresh
	if got := c.request("/api/v1/logout", nil); got.Code != 200 || got.Result().Cookies()[0].Name != a.cookieName {
		t.Fatal("logout did not clear only its namespace")
	}
}
