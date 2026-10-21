// Package websurface is a network surface for the existing Bot owner. It owns
// authentication and ingress receipts, never Runtime execution or recovery.
package websurface

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktop"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type TaskReader interface {
	SurfaceTasks() []api.Task
	SurfaceTaskConversation(context.Context, string) (api.TaskConversation, error)
}
type Services struct {
	App     TaskReader
	Backend *backend.Service
	Surface *desktop.Service
}
type Config struct {
	Directory string
	Origins   []string
	Version   string
	// DirectLoopback is set only when the listener itself is loopback. A remote
	// reverse proxy must use the authenticated listener, never this local surface.
	DirectLoopback bool
	LocalProxyKey  string
}
type session struct {
	CSRF    string `json:"csrf"`
	Expires int64  `json:"expires"`
}
type authState struct {
	Sessions map[string]session `json:"sessions"`
}
type pairing struct {
	Hash    string `json:"hash"`
	Expires int64  `json:"expires"`
}
type Server struct {
	config      Config
	services    Services
	mux         *http.ServeMux
	origins     map[string]bool
	hosts       map[string]bool
	secure      bool
	cookieName  string
	mu          sync.Mutex
	auth        authState
	pair        pairing
	attempts    []time.Time
	operations  map[string]operation
	retired     *sql.DB
	composerMu  sync.Mutex
	writer      string
	writerEpoch string
	writerUntil time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	workers     sync.WaitGroup
	eventMu     sync.Mutex
	latest      []byte
	subscribers map[chan []byte]struct{}
}

func token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func New(config Config, services Services, assets fs.FS) (*Server, error) {
	if !filepath.IsAbs(config.Directory) || len(config.Origins) == 0 {
		return nil, errors.New("private absolute directory and explicit browser origins required")
	}
	if err := os.MkdirAll(config.Directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(config.Directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("web directory must be private (0700), not a symlink")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{config: config, services: services, mux: http.NewServeMux(), origins: map[string]bool{}, hosts: map[string]bool{}, auth: authState{Sessions: map[string]session{}}, operations: map[string]operation{}, subscribers: map[chan []byte]struct{}{}, ctx: ctx, cancel: cancel}
	// Cookies are host-scoped, not port-scoped. Independent local profiles must
	// not log each other out during lease renewal. Keep the namespace stable
	// across restarts without exposing the private directory.
	s.cookieName = "caelisbot_session_" + digest(filepath.Clean(config.Directory))[:16]
	for _, origin := range config.Origins {
		u, e := url.Parse(origin)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			cancel()
			return nil, errors.New("origin must be an exact http(s) origin without a path")
		}
		if len(s.origins) > 0 && s.secure != (u.Scheme == "https") {
			cancel()
			return nil, errors.New("do not mix HTTP and HTTPS browser origins")
		}
		s.origins[origin] = true
		s.hosts[strings.ToLower(u.Host)] = true
		s.secure = u.Scheme == "https"
	}
	if err = readState(filepath.Join(config.Directory, "sessions.json"), &s.auth); err != nil {
		cancel()
		return nil, err
	}
	if s.auth.Sessions == nil {
		s.auth.Sessions = map[string]session{}
	}
	if err = s.loadOperations(); err != nil {
		cancel()
		return nil, err
	}
	if s.operations == nil {
		s.operations = map[string]operation{}
	}
	if err = s.Pair(); err != nil {
		cancel()
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(config.Directory, "uploads"), 0700); err != nil {
		cancel()
		return nil, err
	}
	s.mux.HandleFunc("POST /api/v1/pair", s.exchange)
	s.mux.HandleFunc("GET /api/v1/bootstrap", s.bootstrap)
	s.mux.HandleFunc("POST /api/v1/bootstrap", s.bootstrap)
	s.mux.HandleFunc("POST /api/v1/logout", s.logout)
	s.mux.HandleFunc("POST /api/v1/composer/claim", s.claim)
	s.mux.HandleFunc("GET /api/v1/events", s.events)
	s.mux.HandleFunc("GET /api/v1/operations/{id}", s.operationStatus)
	s.mux.HandleFunc("POST /api/v1/attachments", s.upload)
	s.mux.HandleFunc("POST /api/v1/content/import", s.importContent)
	s.mux.HandleFunc("GET /api/v1/artifacts/{id}", s.artifact)
	s.registerActions()
	files := services.Surface.WebContentHandler(http.FileServerFS(assets))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			fail(w, 404, "unknown_action")
			return
		}
		if r.URL.Path == "/" {
			body, e := fs.ReadFile(assets, "web.html")
			if e != nil {
				http.Error(w, "Web assets unavailable", 503)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(body)
			return
		}
		files.ServeHTTP(w, r)
	})
	s.workers.Add(1)
	go s.observe()
	return s, nil
}

func readState(path string, out any) error {
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if len(b) > 16<<20 {
		return errors.New("web state exceeds limit")
	}
	return json.Unmarshal(b, out)
}

// Pair rotates only the local, short-lived pairing material. It is never
// printed in logs or embedded in a URL. Read pairing-code from the private root.
func (s *Server) Pair() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CreatePairing(s.config.Directory)
}

func CreatePairing(directory string) error {
	if e := os.MkdirAll(directory, 0700); e != nil {
		return e
	}
	info, e := os.Lstat(directory)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("pairing directory must be private")
	}
	code := token()
	p := pairing{digest(code), time.Now().Add(10 * time.Minute).Unix()}
	if e = localstate.Write(filepath.Join(directory, "pairing.json"), p); e != nil {
		return e
	}
	f, err := os.CreateTemp(directory, ".pair-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.WriteString(code + "\n")
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(directory, "pairing-code"))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'")
	if !s.hosts[strings.ToLower(r.Host)] {
		fail(w, http.StatusForbidden, "origin_denied")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !s.origins[origin] {
		fail(w, http.StatusForbidden, "origin_denied")
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, http.StatusForbidden, "origin_denied")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/content/") {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "GET" && (!s.origins[r.Header.Get("Origin")] || r.Header.Get("X-Caelis-Request") != "1") {
			fail(w, 403, "origin_denied")
			return
		}
		if r.URL.Path == "/api/v1/bootstrap" && r.Method == "POST" {
			if _, _, ok := s.authenticate(r); !ok {
				if !s.localRequest(r) {
					fail(w, 401, "pair_required")
					return
				}
				s.localBootstrap(w, r)
				return
			}
			// Bootstrap only returns this already-authenticated session's CSRF token.
			s.bootstrap(w, r)
			return
		}
		if r.URL.Path != "/api/v1/pair" {
			key, entry, ok := s.authenticate(r)
			if !ok {
				fail(w, 401, "pair_required")
				return
			}
			if r.Method != "GET" && r.Header.Get("X-CSRF-Token") != entry.CSRF {
				fail(w, 403, "csrf_denied")
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), sessionKey{}, key))
		}
	}
	s.mux.ServeHTTP(w, r)
}

// Forwarding metadata never establishes local authority. The optional proof is
// supplied only by our host-loopback relay, not by the browser or a public proxy.
func (s *Server) localRequest(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || !loopbackHost(u.Hostname()) {
		return false
	}
	for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP"} {
		if r.Header.Get(name) != "" {
			return false
		}
	}
	if s.config.LocalProxyKey != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Caelis-Local-Proof")), []byte(s.config.LocalProxyKey)) == 1 {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return s.config.DirectLoopback && err == nil && ip != nil && ip.IsLoopback()
}
func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func (s *Server) localBootstrap(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, value := range s.auth.Sessions {
		if value.Expires <= now.Unix() {
			delete(s.auth.Sessions, key)
		}
	}
	if len(s.auth.Sessions) >= 16 {
		fail(w, 429, "device_limit")
		return
	}
	raw := token()
	entry := session{token(), now.Add(7 * 24 * time.Hour).Unix()}
	s.auth.Sessions[digest(raw)] = entry
	if err := localstate.Write(filepath.Join(s.config.Directory, "sessions.json"), s.auth); err != nil {
		delete(s.auth.Sessions, digest(raw))
		fail(w, 503, "session_unavailable")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: raw, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 24 * 3600})
	s.bootstrapResponse(w, entry)
}

type sessionKey struct{}

func sessionID(r *http.Request) string { v, _ := r.Context().Value(sessionKey{}).(string); return v }

// Accept a legacy cookie only through this profile's own session authority.
// Bootstrap migrates it without creating another session or clearing a cookie
// that may still belong to another older instance on this host.
func (s *Server) sessionCookie(r *http.Request) (*http.Cookie, error) {
	c, err := r.Cookie(s.cookieName)
	if err == http.ErrNoCookie {
		return r.Cookie("caelisbot_session")
	}
	return c, err
}
func (s *Server) authenticate(r *http.Request) (string, session, bool) {
	c, e := s.sessionCookie(r)
	if e != nil {
		return "", session{}, false
	}
	key := digest(c.Value)
	s.mu.Lock()
	entry, ok := s.auth.Sessions[key]
	s.mu.Unlock()
	return key, entry, ok && entry.Expires > time.Now().Unix()
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code string) {
	jsonOut(w, status, map[string]string{"error": code})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON required")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("extra input")
	}
	return nil
}

func (s *Server) exchange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if decode(w, r, &in) != nil {
		fail(w, 400, "invalid_request")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	recent := s.attempts[:0]
	for _, t := range s.attempts {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	s.attempts = recent
	if len(recent) >= 10 {
		fail(w, 429, "try_later")
		return
	}
	s.attempts = append(s.attempts, now)
	// A local pair command can rotate the file without restarting the Bot.
	if err := readState(filepath.Join(s.config.Directory, "pairing.json"), &s.pair); err != nil {
		fail(w, 503, "pair_unavailable")
		return
	}
	if s.pair.Hash == "" || s.pair.Expires <= now.Unix() || digest(strings.TrimSpace(in.Code)) != s.pair.Hash {
		fail(w, 403, "invalid_code")
		return
	}
	raw := token()
	entry := session{token(), now.Add(7 * 24 * time.Hour).Unix()}
	for key, value := range s.auth.Sessions {
		if value.Expires <= now.Unix() {
			delete(s.auth.Sessions, key)
		}
	}
	if len(s.auth.Sessions) >= 16 {
		fail(w, 429, "device_limit")
		return
	}
	s.auth.Sessions[digest(raw)] = entry
	if e := localstate.Write(filepath.Join(s.config.Directory, "sessions.json"), s.auth); e != nil {
		delete(s.auth.Sessions, digest(raw))
		fail(w, 503, "pair_unavailable")
		return
	}
	s.pair = pairing{}
	if e := localstate.Write(filepath.Join(s.config.Directory, "pairing.json"), s.pair); e != nil {
		delete(s.auth.Sessions, digest(raw))
		_ = localstate.Write(filepath.Join(s.config.Directory, "sessions.json"), s.auth)
		fail(w, 503, "pair_unavailable")
		return
	}
	_ = os.Remove(filepath.Join(s.config.Directory, "pairing-code"))
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: raw, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 24 * 3600})
	jsonOut(w, 200, map[string]any{"csrf": entry.CSRF})
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	_, entry, _ := s.authenticate(r)
	if c, err := s.sessionCookie(r); err == nil && c.Name != s.cookieName {
		http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: c.Value, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: int(entry.Expires - time.Now().Unix())})
	}
	s.bootstrapResponse(w, entry)
}
func (s *Server) bootstrapResponse(w http.ResponseWriter, entry session) {
	jsonOut(w, 200, map[string]any{"csrf": entry.CSRF, "version": s.config.Version, "capabilities": map[string]any{"web": true, "terminal": false, "computerUse": false, "nativeWindows": false, "reason": "Linux / WSL 暂不支持桌面电脑操作；网页不提供终端窗口。"}})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.auth.Sessions, sessionID(r))
	if e := localstate.Write(filepath.Join(s.config.Directory, "sessions.json"), s.auth); e != nil {
		fail(w, 503, "operation_unconfirmed")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode})
	jsonOut(w, 200, nil)
}

func (s *Server) Close() {
	s.cancel()
	s.workers.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired != nil {
		_ = s.retired.Close()
	}
}
func (s *Server) String() string { return fmt.Sprintf("CaelisBot Web (%s)", s.config.Version) }
