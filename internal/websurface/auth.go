package websurface

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const sessionIdleTTL = 7 * 24 * time.Hour
const authFlushInterval = time.Minute

func (s *Server) loadAuth() error {
	// This private, server-written file has no browser-count admission limit.
	// Keep the unrelated ingress receipt size guard in readState unchanged.
	f, err := os.Open(filepath.Join(s.config.Directory, "sessions.json"))
	if err == nil {
		defer f.Close()
		d := json.NewDecoder(f)
		if err = d.Decode(&s.auth); err != nil {
			return err
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return errors.New("extra authentication state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if s.auth.Sessions == nil {
		s.auth.Sessions = map[string]session{}
	}
	if s.auth.LocalCSRF == "" {
		// Local authority comes from the connection, never this token. Keeping one
		// profile CSRF token allows open local tabs to survive a service restart.
		s.auth.LocalCSRF = token()
		s.authDirty = true
	}
	s.pruneSessionsLocked(s.now())
	if s.authDirty {
		return s.writeAuthLocked()
	}
	return nil
}

func (s *Server) authenticate(r *http.Request) (string, session, bool) {
	c, err := s.sessionCookie(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		key := digest(c.Value)
		if entry, ok := s.auth.Sessions[key]; ok && entry.Expires > s.now().Unix() {
			// Existing grants retain their CSRF and receipt owner, including clients
			// created by older loopback bootstrap. No replacement grant is allocated.
			return key, entry, true
		}
	}
	if !s.localRequest(r) {
		return "", session{}, false
	}
	key := "local"
	if err == nil && c.Name == s.cookieName {
		// On a trusted local connection only, an old profile cookie can still
		// select its original receipts after the grant expires. It grants no
		// remote access. Never adopt another profile's unscoped legacy cookie.
		key = digest(c.Value)
	}
	return key, session{CSRF: s.auth.LocalCSRF}, true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, raw string) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: raw, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionIdleTTL / time.Second)})
}

func (s *Server) useSession(w http.ResponseWriter, r *http.Request, key string) {
	if s.touchSession(key) && w != nil {
		if c, err := s.sessionCookie(r); err == nil {
			s.setSessionCookie(w, c.Value)
		}
	}
}

func (s *Server) touchSession(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.auth.Sessions[key]
	now := s.now()
	if !ok || entry.Expires <= now.Unix() {
		return false
	}
	expires := now.Add(sessionIdleTTL).Unix()
	if expires > entry.Expires {
		entry.Expires = expires
		s.auth.Sessions[key] = entry
		s.authDirty = true
	}
	return true
}

func (s *Server) pruneSessionsLocked(now time.Time) {
	for key, entry := range s.auth.Sessions {
		if entry.Expires <= now.Unix() {
			delete(s.auth.Sessions, key)
			s.authDirty = true
		}
	}
}
func (s *Server) writeAuthLocked() error {
	if err := localstate.Write(filepath.Join(s.config.Directory, "sessions.json"), s.auth); err != nil {
		return err
	}
	s.authDirty = false
	return nil
}
func (s *Server) flushAuth() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneSessionsLocked(s.now())
	if s.authDirty {
		if err := s.writeAuthLocked(); err != nil {
			// Do not log tokens, CSRF, client identities, or private paths. Dirty
			// renewals stay in memory and are retried on the next tick/shutdown.
			log.Print("web session idle renewal could not be saved; will retry")
		}
	}
}
func (s *Server) maintainAuth() {
	defer s.workers.Done()
	ticker := time.NewTicker(authFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.flushAuth()
		case <-s.ctx.Done():
			s.flushAuth()
			return
		}
	}
}
