package websurface

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestUnknownNoGrantExplicitFreshAuthorizationRecoversWeb(t *testing.T) {
	for _, mode := range []string{"normal", "before", "after"} {
		t.Run(mode, func(t *testing.T) { testUnknownNoGrantAuthorization(t, mode) })
	}
}
func testUnknownNoGrantAuthorization(t *testing.T, mode string) {
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
	var pairWrites atomic.Int32
	var nextFault atomic.Int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"pair" {
			pairWrites.Add(1)
		}
		if r.URL.Path == workerpeer.Prefix+"pair" && dropped.CompareAndSwap(false, true) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		fault := int32(0)
		if r.URL.Path == workerpeer.Prefix+"pair" {
			fault = nextFault.Swap(0)
		}
		if fault == 2 {
			rec := httptest.NewRecorder()
			peer.ServeHTTP(rec, r)
		}
		if fault != 0 {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
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
			if r.URL.Query().Get("arm") == "before" {
				nextFault.Store(1)
			}
			if r.URL.Query().Get("arm") == "after" {
				nextFault.Store(2)
			}
			fresh, e := peer.Pairing(t.Context())
			if e != nil {
				t.Fatal(e)
			}
			ms := m.Machines()
			json.NewEncoder(w).Encode(map[string]any{"code": fresh.Code, "id": ms[0].ID, "grants": len(peer.Status().Sources)})
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
	cfg, _ := json.Marshal(map[string]string{"origin": origin, "cookie": cookie.Name + "=" + cookie.Value, "csrf": auth.CSRF, "peer": b.URL, "code": code.Code, "mode": mode})
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), node, "testdata/bot-pair-authorization-client.mjs")
	cmd.Stdin = bytes.NewReader(cfg)
	out, err := cmd.CombinedOutput()
	expected := int32(4)
	if mode == "before" {
		expected = 5
	}
	if pairWrites.Load() != expected || len(peer.Status().Sources) != 1 {
		t.Errorf("original pair effects %d", pairWrites.Load())
	}
	if err != nil {
		t.Fatalf("actual client + handlers + machines/peer failed: %v\n%s", err, out)
	}
	if !s.UpdateQuiescent() {
		t.Fatal("confirmed fresh authorization still blocks managed restart")
	}
	s.mu.Lock()
	var rootID, webOwner string
	for id, op := range s.operations {
		if op.BotConnection != nil && op.AuthorizationParent == "" && op.Error == "operation_unconfirmed" {
			rootID = id
			webOwner = op.Owner
			break
		}
	}
	id, child, ok := s.botAuthorizationTailLocked(rootID, webOwner)
	if !ok || !botAuthorizationConfirmed(child) {
		s.mu.Unlock()
		t.Fatal("missing confirmed recovery chain")
	}
	child.At = 0
	s.operations[id] = child
	for n := 0; n < 300; n++ {
		s.operations[fmt.Sprintf("retirement-padding-%04d", n)] = operation{Owner: webOwner, State: "complete", At: 1}
	}
	e := s.saveOperations()
	if e == nil && s.retired != nil {
		e = s.retired.Close()
		s.retired = nil
	}
	if e == nil {
		s.operations = nil
		e = s.loadOperations()
	}
	_, tail, valid := s.botAuthorizationTailLocked(rootID, webOwner)
	s.mu.Unlock()
	if e != nil || !valid || !tail.ArchivedAuthorization || !s.UpdateQuiescent() {
		t.Fatal("retired confirmed authorization did not survive restart", e, valid, tail.ArchivedAuthorization)
	}

}

// Recreate the durable admission snapshot at the exact crash boundary: machines
// saved a newer epoch and B accepted, but the Web host never saved its result.
// The original grant cannot be inferred from a newer epoch. Preserve uncertainty
// and the only proof; another deliberate new authorization can still proceed.
func TestInterruptedNewAuthorizationPreservesUnknownAndFreshRecovery(t *testing.T) {
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
	var pairWrites atomic.Int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"pair" && pairWrites.Add(1) == 1 {
			c, _, _ := w.(http.Hijacker).Hijack()
			c.Close()
			return
		}
		peer.ServeHTTP(w, r)
	}))
	defer b.Close()
	m, e := machines.Open(t.TempDir(), owner, nil)
	if e != nil {
		t.Fatal(e)
	}
	first, e := peer.Pairing(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.ConnectBot(t.Context(), api.BotConnectionInput{Name: "B", Address: b.URL, Code: first.Code})
	var unknown *api.BotConnectionUnconfirmedError
	if !errors.As(e, &unknown) {
		t.Fatal(e)
	}
	original := unknown.Reference
	fresh, e := peer.Pairing(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = m.AuthorizeBotConnection(t.Context(), original, api.BotConnectionInput{ID: original.ID, Address: b.URL, Code: fresh.Code}); e != nil {
		t.Fatal(e)
	}
	s := testServer(t)
	s.services.Backend.ConfigureMachines(m)
	c := pairClient(t, s)
	webOwner := sessionIDFor(c)
	s.operations["original-unknown-pair"] = operation{Owner: webOwner, State: "complete", Error: "operation_unconfirmed", BotConnection: &original, AuthorizationBy: "interrupted-new-pair"}
	s.operations["interrupted-new-pair"] = operation{Owner: webOwner, State: "pending", BotAuthorization: true, BotConnection: &original, AuthorizationParent: "original-unknown-pair"}
	if e = s.saveOperations(); e != nil {
		t.Fatal(e)
	}
	s.operations = nil
	if e = s.loadOperations(); e != nil {
		t.Fatal(e)
	}
	if e = s.reconcileBotConnection(t.Context(), "interrupted-new-pair", webOwner); e != nil {
		t.Fatal(e)
	}
	if s.operations["interrupted-new-pair"].Error != "operation_unconfirmed" {
		t.Fatal("crashed unknown inferred as success")
	}
	newer, e := peer.Pairing(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	got := c.request(botAuthorizationPath, map[string]any{"id": "explicit-after-host-crash", "args": []any{api.BotConnectionAuthorization{OriginalOperationID: "interrupted-new-pair", Connection: api.BotConnectionInput{ID: original.ID, Address: b.URL, Code: newer.Code}}}})
	if got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	if len(peer.Status().Sources) != 1 || s.operations["interrupted-new-pair"].Error != "operation_unconfirmed" || s.operations["original-unknown-pair"].Error != "operation_unconfirmed" {
		t.Fatal("original source or uncertainty changed")
	}
	if !s.UpdateQuiescent() {
		t.Fatal("confirmed recovery blocks restart")
	}
	if s.botAuthorizationAllowsRestartLocked("original-unknown-pair", operation{Owner: webOwner, State: "pending", Error: "operation_unconfirmed", BotConnection: &original}) {
		t.Fatal("pending invocation allowed restart")
	}
	blocked := s.operations["original-unknown-pair"]
	blocked.Native = &nativeReceipt{Owner: "native", ID: "original"}
	if s.botAuthorizationAllowsRestartLocked("original-unknown-pair", blocked) {
		t.Fatal("native unknown allowed restart")
	}
	child := s.operations["explicit-after-host-crash"]
	broken := child
	broken.Owner = "other"
	s.operations["explicit-after-host-crash"] = broken
	if s.UpdateQuiescent() {
		t.Fatal("other owner authorization allowed restart")
	}
	s.operations["explicit-after-host-crash"] = child
	// This unlinked route cannot manufacture a following authorization gate.
	unlinked := c.request(botAuthorizationPath, map[string]any{"id": "unlinked-new-authorization", "args": []any{api.BotConnectionAuthorization{Connection: api.BotConnectionInput{ID: original.ID, Address: b.URL, Code: newer.Code}}}})
	if unlinked.Code != 409 || s.operations["unlinked-new-authorization"].Error != "operation_rejected" || pairWrites.Load() != 3 {
		t.Fatal("unlinked Web authorization accepted")
	}
}
