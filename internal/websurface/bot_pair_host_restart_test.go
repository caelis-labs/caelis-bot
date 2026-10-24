package websurface

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/machines"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

type capturePairAdmission struct {
	*machines.Service
	capture func()
}

func (m *capturePairAdmission) ConnectBot(ctx context.Context, in api.BotConnectionInput) (api.Machine, error) {
	m.capture()
	return m.Service.ConnectBot(ctx, in)
}
func (m *capturePairAdmission) ConnectPreparedBot(ctx context.Context, ref api.BotConnectionReference, in api.BotConnectionInput) (api.Machine, api.BotConnectionReference, error) {
	m.capture()
	return m.Service.ConnectPreparedBot(ctx, ref, in)
}

// Actual TS/HTTP restores the durable admission image, not a manufactured final
// receipt. The native peer may have accepted before the old host saved a result.
func TestFirstPairHostExitRestoresOriginalWebReceipt(t *testing.T) {
	for _, mode := range []string{"before", "after", "result"} {
		t.Run(mode, func(t *testing.T) {
			owner := &auditPairOwner{}
			peer, e := workerpeer.Open(t.TempDir(), func(context.Context, string, string, string) (workerpeer.Owner, string, *api.WorkExecutionSettings, error) {
				return owner, "codex", nil, nil
			})
			if e != nil {
				t.Fatal(e)
			}
			if e = peer.Enable(true); e != nil {
				t.Fatal(e)
			}
			code, e := peer.Pairing(t.Context())
			if e != nil {
				t.Fatal(e)
			}
			var pairs atomic.Int32
			b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == workerpeer.Prefix+"pair" && pairs.Add(1) == 1 && mode != "result" {
					if mode == "after" {
						rec := httptest.NewRecorder()
						peer.ServeHTTP(rec, r)
						if rec.Code != 200 {
							t.Error("B did not grant", rec.Code)
						}
					}
					c, _, _ := w.(http.Hijacker).Hijack()
					c.Close()
					return
				}
				peer.ServeHTTP(w, r)
			}))
			defer b.Close()
			machineRoot := t.TempDir()
			m, e := machines.Open(machineRoot, owner, nil)
			if e != nil {
				t.Fatal(e)
			}
			h := httptest.NewUnstartedServer(nil)
			origin := "http://" + h.Listener.Addr().String()
			current := testServerConfig(t, func(c *Config) { c.Origins = []string{origin} })
			var snapshot []byte
			var capture sync.Once
			wrapped := &capturePairAdmission{Service: m, capture: func() {
				capture.Do(func() {
					snapshot, e = os.ReadFile(filepath.Join(current.config.Directory, "operations.json"))
					if e != nil {
						t.Error(e)
					}
				})
			}}
			current.services.Backend.ConfigureMachines(wrapped)
			var first atomic.Bool
			var hostMu sync.Mutex
			h.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hostMu.Lock()
				defer hostMu.Unlock()
				if r.URL.Path == botConnectionPath && first.CompareAndSwap(false, true) {
					rec := httptest.NewRecorder()
					current.ServeHTTP(rec, r)
					// Crash image captured at real controller entry; discard only the old host
					// result, preserve actual B grant and machines proof/intent persisted by it.
					current.Close()
					if e = os.WriteFile(filepath.Join(current.config.Directory, "operations.json"), snapshot, 0600); e != nil {
						t.Error(e)
					}
					restored, err := machines.Open(machineRoot, owner, nil)
					if err != nil {
						t.Error(err)
					}
					current.services.Backend.ConfigureMachines(restored)
					next, err := New(current.config, current.services, fstest.MapFS{"web.html": {Data: []byte("Bot")}})
					if err != nil {
						t.Error(err)
					}
					current = next
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
					return
				}
				if r.URL.Path == "/audit/status" || r.URL.Path == "/audit/revoke" || r.URL.Path == "/audit/reload" {
					if r.URL.Path == "/audit/revoke" {
						sources := peer.Status().Sources
						if len(sources) != 1 {
							t.Error("unexpected grant count")
						}
						if err := peer.Revoke(sources[0].ID); err != nil {
							t.Error(err)
						}
					}
					if r.URL.Path == "/audit/reload" {
						current.mu.Lock()
						current.operations = nil
						e = current.loadOperations()
						current.mu.Unlock()
						if e != nil {
							t.Error(e)
						}
					}
					fresh, err := peer.Pairing(t.Context())
					if err != nil {
						t.Error(err)
					}
					json.NewEncoder(w).Encode(map[string]any{"grants": len(peer.Status().Sources), "quiescent": current.UpdateQuiescent(), "code": fresh.Code, "pairs": pairs.Load()})
					return
				}
				current.ServeHTTP(w, r)
			})
			h.Start()
			defer h.Close()
			defer func() { current.Close() }()
			saved, e := os.ReadFile(filepath.Join(current.config.Directory, "pairing-code"))
			if e != nil {
				t.Fatal(e)
			}
			body, _ := json.Marshal(map[string]string{"code": strings.TrimSpace(string(saved))})
			r := httptest.NewRequest("POST", origin+"/api/v1/pair", bytes.NewReader(body))
			r.Header.Set("Origin", origin)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Caelis-Request", "1")
			rec := httptest.NewRecorder()
			current.ServeHTTP(rec, r)
			var auth struct{ CSRF string }
			if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &auth) != nil {
				t.Fatal("web auth", rec.Code)
			}
			cookie := rec.Result().Cookies()[0]
			cfg, _ := json.Marshal(map[string]string{"origin": origin, "cookie": cookie.Name + "=" + cookie.Value, "csrf": auth.CSRF, "peer": b.URL, "code": code.Code, "mode": mode})
			node, e := exec.LookPath("node")
			if e != nil {
				t.Fatal(e)
			}
			cmd := exec.CommandContext(t.Context(), node, "testdata/bot-pair-host-restart.mjs")
			cmd.Stdin = bytes.NewReader(cfg)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("first-pair restart real client: %v\n%s", err, out)
			}
		})
	}
}
