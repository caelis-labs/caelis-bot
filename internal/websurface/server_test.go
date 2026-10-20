package websurface

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktop"
)

type testEngine struct{ api.Engine }

func (testEngine) Snapshot() api.Snapshot {
	return api.Snapshot{Revision: 1, Connection: "ready", CanSend: true}
}

const testOrigin = "http://127.0.0.1:9317"

type client struct {
	s                *Server
	cookie           *http.Cookie
	csrf, tab, epoch string
}

func testServer(t *testing.T, engines ...api.Engine) *Server {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Web service requires Unix private filesystem; Windows uses WSL")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("XDG_DATA_HOME", root)
	core, surface, err := desktop.NewHeadlessProduct(filepath.Join(root, "product"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Close() })
	var engine api.Engine = testEngine{}
	if len(engines) > 0 {
		engine = engines[0]
	}
	s, err := New(Config{Directory: filepath.Join(root, "Web"), Origins: []string{testOrigin}, Version: "test"}, Services{Backend: backend.NewService(engine, func([]string) ([]api.InputFile, error) { return nil, nil }, nil, nil, nil), Surface: surface}, fstest.MapFS{"web.html": {Data: []byte("CaelisBot")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func (c *client) request(path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", testOrigin+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-Caelis-Request", "1")
	r.Header.Set("X-CSRF-Token", c.csrf)
	r.Header.Set("X-Caelis-Tab", c.tab)
	r.Header.Set("X-Caelis-Edit-Epoch", c.epoch)
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.s.ServeHTTP(w, r)
	return w
}
func pairClient(t *testing.T, s *Server) *client {
	t.Helper()
	if err := s.Pair(); err != nil {
		t.Fatal(err)
	}
	code, err := os.ReadFile(filepath.Join(s.config.Directory, "pairing-code"))
	if err != nil {
		t.Fatal(err)
	}
	c := &client{s: s, tab: token()}
	w := c.request("/api/v1/pair", map[string]string{"code": strings.TrimSpace(string(code))})
	if w.Code != 200 {
		t.Fatalf("pair status %d", w.Code)
	}
	c.cookie = w.Result().Cookies()[0]
	var v struct {
		CSRF string `json:"csrf"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	c.csrf = v.CSRF
	return c
}
func (c *client) claim(t *testing.T, take bool) bool {
	t.Helper()
	w := c.request("/api/v1/composer/claim", map[string]bool{"takeOver": take})
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var v struct {
		Editing bool   `json:"editing"`
		Epoch   string `json:"epoch"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	c.epoch = v.Epoch
	return v.Editing
}
func TestAuthenticationAndNetworkBoundary(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	if !c.cookie.HttpOnly || c.cookie.SameSite != http.SameSiteStrictMode || c.cookie.Secure {
		t.Fatal("loopback cookie policy")
	}
	for _, tc := range []struct {
		name, host, origin, csrf string
		cookie                   bool
		want                     int
	}{
		{"no cookie", "127.0.0.1:9317", testOrigin, c.csrf, false, 401},
		{"csrf", "127.0.0.1:9317", testOrigin, "bad", true, 403},
		{"foreign origin", "127.0.0.1:9317", "https://evil.invalid", c.csrf, true, 403},
		{"no origin", "127.0.0.1:9317", "", c.csrf, true, 403},
		{"DNS rebinding", "evil.invalid", testOrigin, c.csrf, true, 403},
		{"paired", "127.0.0.1:9317", testOrigin, c.csrf, true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", testOrigin+"/api/v1/conversation/snapshot", strings.NewReader(`{"args":[]}`))
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Caelis-Request", "1")
			r.Header.Set("X-CSRF-Token", tc.csrf)
			if tc.cookie {
				r.AddCookie(c.cookie)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code)
			}
		})
	}
	if c.request("/api/v1/WailsRPC", map[string]any{"method": "RunShell"}).Code != 404 {
		t.Fatal("generic RPC exposed")
	}
	// Pairing material is consumed and login does not grant cross-origin access.
	if _, err := os.Stat(filepath.Join(s.config.Directory, "pairing-code")); !os.IsNotExist(err) {
		t.Fatal("pair code retained")
	}
	if c.request("/api/v1/logout", nil).Code != 200 {
		t.Fatal("logout")
	}
	if c.request("/api/v1/conversation/snapshot", map[string]any{"args": []any{}}).Code != 401 {
		t.Fatal("revoked session survived")
	}
}
func TestWriteIdentitySurvivesLostResponseAndRestart(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	var calls atomic.Int32
	s.register(action{Path: "/api/test/effect", Function: func(ctx context.Context, v string) (string, error) {
		if ctx.Err() != nil {
			t.Error("browser context owns effect")
		}
		calls.Add(1)
		return "accepted", nil
	}})
	id := token()
	body := map[string]any{"id": id, "args": []string{"value"}}
	if c.request("/api/test/effect", body).Code != 200 || c.request("/api/test/effect", body).Code != 200 || calls.Load() != 1 {
		t.Fatal("same write replayed")
	}
	if c.request("/api/test/effect", map[string]any{"id": id, "args": []string{"changed"}}).Code != 409 {
		t.Fatal("identity payload changed")
	}
	other := pairClient(t, s)
	if other.request("/api/test/effect", body).Code != 409 {
		t.Fatal("receipt crossed browser session")
	}
	// A persisted pending record after a crash must never dispatch again.
	s.mu.Lock()
	s.operations[id] = operation{Owner: sessionIDFor(c), Digest: digest("/api/test/effect" + `["value"]`), State: "pending"}
	if err := s.saveOperations(); err != nil {
		t.Fatal(err)
	}
	s.operations = map[string]operation{}
	if err := readState(filepath.Join(s.config.Directory, "operations.json"), &s.operations); err != nil {
		t.Fatal(err)
	}
	s.mu.Unlock()
	if c.request("/api/test/effect", body).Code != 409 || calls.Load() != 1 {
		t.Fatal("uncertain operation replayed")
	}
	data, _ := os.ReadFile(filepath.Join(s.config.Directory, "operations.json"))
	if bytes.Contains(data, []byte(`"value"`)) {
		t.Fatal("request body persisted")
	}
}
func sessionIDFor(c *client) string { return digest(c.cookie.Value) }
func TestComposerEpochAndOwnedUpload(t *testing.T) {
	s := testServer(t)
	first := pairClient(t, s)
	if !first.claim(t, false) {
		t.Fatal("first claim")
	}
	oldEpoch := first.epoch
	second := &client{s: s, cookie: first.cookie, csrf: first.csrf, tab: token()}
	if second.claim(t, false) {
		t.Fatal("implicit takeover")
	}
	if !second.claim(t, true) {
		t.Fatal("explicit takeover")
	}
	s.register(action{Path: "/api/test/edit", Function: func() {}, Composer: true, Local: true})
	if first.request("/api/test/edit", map[string]any{"args": []any{}}).Code != 409 {
		t.Fatal("stale tab wrote")
	}
	if !first.claim(t, true) || first.epoch == oldEpoch {
		t.Fatal("lease did not fence prior epoch")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "notes.txt")
	_, _ = io.WriteString(part, "fixture attachment")
	_ = form.Close()
	r := httptest.NewRequest("POST", testOrigin+"/api/v1/attachments", &body)
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-Caelis-Request", "1")
	r.Header.Set("X-CSRF-Token", first.csrf)
	r.Header.Set("X-Caelis-Tab", first.tab)
	r.Header.Set("X-Caelis-Edit-Epoch", first.epoch)
	r.AddCookie(first.cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	files := s.services.Surface.DraftFiles()
	if len(files) != 1 || files[0].Name != "notes.txt" {
		t.Fatal("upload not staged")
	}
	if _, err := s.services.Surface.RemoveFile(files[0].ID); err != nil {
		t.Fatal(err)
	}
	var regular int
	_ = filepath.WalkDir(filepath.Join(s.config.Directory, "uploads"), func(_ string, e os.DirEntry, err error) error {
		if err == nil && e.Type().IsRegular() {
			regular++
		}
		return err
	})
	if regular != 0 {
		t.Fatal("owned upload not cleaned")
	}
	if _, err := s.services.Surface.StageWebUpload(filepath.Join(s.config.Directory, "uploads"), "../escape", strings.NewReader("bad")); err == nil {
		t.Fatal("path traversal accepted")
	}
}
func TestNoReadOnlyModeCanDispatchArbitraryMethod(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	if c.request("/api/v1/conversation/snapshot", map[string]any{"args": []any{}, "function": "shell"}).Code != 400 {
		t.Fatal("unrecognized request authority accepted")
	}
}

func TestPrivateHTTPSProxyStillRequiresPairing(t *testing.T) {
	original := testServer(t)
	origin := "https://bot.example.ts.net"
	s, err := New(Config{Directory: filepath.Join(t.TempDir(), "Web"), Origins: []string{origin}}, original.services, fstest.MapFS{"web.html": {Data: []byte("test")}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := httptest.NewRequest("GET", origin+"/api/v1/bootstrap", nil)
	r.Header.Set("Tailscale-User-Login", "trusted@example.invalid")
	r.Header.Set("X-Forwarded-Host", "bot.example.ts.net")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("private proxy identity bypassed pairing")
	}
	code, _ := os.ReadFile(filepath.Join(s.config.Directory, "pairing-code"))
	body, _ := json.Marshal(map[string]string{"code": strings.TrimSpace(string(code))})
	r = httptest.NewRequest("POST", origin+"/api/v1/pair", bytes.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Caelis-Request", "1")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !w.Result().Cookies()[0].Secure {
		t.Fatal("HTTPS proxy cookie boundary")
	}
	if _, err := New(Config{Directory: filepath.Join(t.TempDir(), "Web"), Origins: []string{origin, testOrigin}}, original.services, fstest.MapFS{}); err == nil {
		t.Fatal("mixed secure/insecure origins accepted")
	}
}
func TestBrowserCancellationDoesNotOwnAdmittedAction(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	called := false
	s.register(action{Path: "/api/test/independent", Function: func(ctx context.Context) error { called = true; return ctx.Err() }})
	body, _ := json.Marshal(map[string]any{"id": token(), "args": []any{}})
	r := httptest.NewRequest("POST", testOrigin+"/api/test/independent", bytes.NewReader(body))
	r.AddCookie(c.cookie)
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Caelis-Request", "1")
	r.Header.Set("X-CSRF-Token", c.csrf)
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !called || w.Code != 200 {
		t.Fatal("browser close canceled owner operation")
	}
}

func TestLocalBootstrapUsesConnectionBoundary(t *testing.T) {
	s := testServer(t)
	s.config.DirectLoopback = true
	s.config.LocalProxyKey = strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, peer, origin, forwarded, proof string
		direct                               bool
		want                                 int
	}{
		{"direct localhost", "127.0.0.1:1234", testOrigin, "", "", true, 200},
		{"ipv6 localhost", "[::1]:1234", testOrigin, "", "", true, 200},
		{"private client", "192.168.1.8:1234", testOrigin, "", "", true, 401},
		{"spoofed forwarded", "192.168.1.8:1234", testOrigin, "127.0.0.1", "", true, 401},
		{"loopback proxy metadata", "127.0.0.1:1234", testOrigin, "198.51.100.9", "", true, 401},
		{"proxy listener", "127.0.0.1:1234", testOrigin, "", "", false, 401},
		{"NAT gateway", "192.168.1.1:1234", testOrigin, "", "", false, 401},
		{"bad local proof", "192.168.1.1:1234", testOrigin, "", strings.Repeat("b", 64), false, 401},
		{"verified host relay", "192.168.1.1:1234", testOrigin, "", strings.Repeat("a", 64), false, 200},
		{"cross site", "127.0.0.1:1234", "https://evil.invalid", "", "", true, 403},
		{"missing origin", "127.0.0.1:1234", "", "", "", true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.config.DirectLoopback = tc.direct
			r := httptest.NewRequest("POST", testOrigin+"/api/v1/bootstrap", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Caelis-Request", "1")
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			r.Header.Set("X-Caelis-Local-Proof", tc.proof)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
			if tc.want == 200 {
				cookies := w.Result().Cookies()
				if len(cookies) != 1 || !cookies[0].HttpOnly {
					t.Fatal("missing local session")
				}
				var body struct {
					CSRF string `json:"csrf"`
				}
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if body.CSRF == "" {
					t.Fatal("missing CSRF")
				}
			}
		})
	}
}

func TestCompletionPersistenceFailureRetainsPendingReceipt(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	var calls int
	s.register(action{Path: "/api/v1/test/durable-completion", Function: func() string {
		calls++
		path := filepath.Join(s.config.Directory, "operations.json")
		if err := os.Rename(path, path+".saved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		return "effect happened"
	}})
	body := map[string]any{"id": "persist-completion-fixture", "args": []any{}}
	if got := c.request("/api/v1/test/durable-completion", body).Code; got != 409 {
		t.Fatal(got)
	}
	s.mu.Lock()
	record := s.operations["persist-completion-fixture"]
	s.mu.Unlock()
	if record.State != "pending" || len(record.Value) != 0 {
		t.Fatal("unpersisted completion became authoritative")
	}
	if got := c.request("/api/v1/test/durable-completion", body).Code; got != 409 || calls != 1 {
		t.Fatal("uncertain operation repeated", got, calls)
	}
}
