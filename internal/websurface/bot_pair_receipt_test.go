package websurface

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/machines"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type auditPairOwner struct{ api.WorkRuntime }

func (*auditPairOwner) Connect(context.Context) error       { return nil }
func (*auditPairOwner) Close(context.Context) error         { return nil }
func (*auditPairOwner) WorkAdmission(context.Context) error { return nil }
func (*auditPairOwner) WorkCapabilities(context.Context, string) (api.WorkCapabilities, error) {
	return api.WorkCapabilities{Contract: api.WorkerContract, Owner: "original-native", Build: "fixture", Runtime: "codex", History: true, Observe: true, Interactions: true, Files: true, Reconcile: true}, nil
}
func TestAuditWebUnknownPairRecoversOriginalProof(t *testing.T) {
	owner := &auditPairOwner{}
	peer, err := workerpeer.Open(t.TempDir(), func(context.Context, string, string, string) (workerpeer.Owner, string, *api.WorkExecutionSettings, error) {
		return owner, "codex", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = peer.Enable(true); err != nil {
		t.Fatal(err)
	}
	code, err := peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var dropped atomic.Bool
	var reachable atomic.Bool
	var pairWrites atomic.Int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"call" && !reachable.Load() {
			http.Error(w, "isolated link offline", 503)
			return
		}
		if r.URL.Path == workerpeer.Prefix+"pair" {
			pairWrites.Add(1)
		}
		if r.URL.Path == workerpeer.Prefix+"pair" && dropped.CompareAndSwap(false, true) {
			rec := httptest.NewRecorder()
			peer.ServeHTTP(rec, r)
			if rec.Code != 200 {
				t.Errorf("pair not accepted %d", rec.Code)
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		peer.ServeHTTP(w, r)
	}))
	defer b.Close()
	m, err := machines.Open(t.TempDir(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewUnstartedServer(nil)
	origin := "http://" + h.Listener.Addr().String()
	s := testServerConfig(t, func(c *Config) { c.Origins = []string{origin} })
	s.services.Backend.ConfigureMachines(m)
	h.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audit/original-proof-recovery" {
			reachable.Store(true)
			ms := m.Machines()
			if len(ms) != 1 {
				t.Errorf("retained profiles %d", len(ms))
				http.Error(w, "missing", 500)
				return
			}
			v, e := m.ConnectBot(t.Context(), api.BotConnectionInput{ID: ms[0].ID, Address: b.URL, Name: "Original B", Reconnect: true})
			if e != nil {
				t.Error(e)
			}
			json.NewEncoder(w).Encode(v)
			return
		}
		s.ServeHTTP(w, r)
	})
	h.Start()
	defer h.Close()
	saved, err := os.ReadFile(filepath.Join(s.config.Directory, "pairing-code"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"code": strings.TrimSpace(string(saved))})
	r := httptest.NewRequest("POST", origin+"/api/v1/pair", bytes.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Caelis-Request", "1")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	var auth struct{ CSRF string }
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &auth) != nil {
		t.Fatal("web auth", rec.Code)
	}
	cookie := rec.Result().Cookies()[0]
	cfg, _ := json.Marshal(map[string]string{"origin": origin, "cookie": cookie.Name + "=" + cookie.Value, "csrf": auth.CSRF, "peer": b.URL, "code": code.Code})
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), node, "testdata/bot-pair-receipt-client.mjs")
	cmd.Stdin = bytes.NewReader(cfg)
	out, err := cmd.CombinedOutput()
	if pairWrites.Load() != 1 {
		t.Errorf("original pair effects %d", pairWrites.Load())
	}
	if err != nil {
		t.Fatalf("actual client + handlers + machines/peer failed: %v\n%s", err, out)
	}
}
