package websurface

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/managedupdate"
)

type updateOwner struct{ calls int }

func (o *updateOwner) Status() managedupdate.Status {
	return managedupdate.Status{Protocol: 1, Mode: "managed", State: "waiting_idle"}
}
func (o *updateOwner) Receipt(id string) (managedupdate.Receipt, error) {
	return managedupdate.Receipt{ID: id, State: "accepted"}, nil
}
func (o *updateOwner) Command(c managedupdate.Command) (managedupdate.Receipt, error) {
	o.calls++
	return o.Receipt(c.ID)
}

func TestManagedUpdateRoutesKeepAuthenticationAndStrictContract(t *testing.T) {
	s := testServer(t)
	owner := &updateOwner{}
	s.RegisterManagedUpdates(owner)
	c := pairClient(t, s)
	command := map[string]any{"id": "original_update_123456", "action": "check"}
	if w := c.request("/api/v1/updates/command", command); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	c.csrf = "wrong"
	if w := c.request("/api/v1/updates/command", command); w.Code != 403 {
		t.Fatal("missing CSRF gate", w.Code)
	}
	c = pairClient(t, s)
	command["url"] = "http://attacker.invalid/payload"
	if w := c.request("/api/v1/updates/command", command); w.Code != 400 {
		t.Fatal("arbitrary URL accepted", w.Code)
	}
	for _, path := range []string{"/api/v1/updates/status", "/api/v1/updates/receipts/original_update_123456"} {
		r := httptest.NewRequest("GET", testOrigin+path, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("unauthenticated read", w.Code)
		}
		r.AddCookie(c.cookie)
		w = httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest("POST", testOrigin+"/api/v1/updates/command", strings.NewReader(`{"id":"original_update_123456","action":"install"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.invalid")
	r.Header.Set("X-Caelis-Request", "1")
	r.Header.Set("X-CSRF-Token", c.csrf)
	r.AddCookie(c.cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || owner.calls != 1 {
		t.Fatal("cross-origin mutation reached owner", w.Code, owner.calls)
	}
}

func TestManagedUpdatePreservesUnknownOperations(t *testing.T) {
	s := testServer(t)
	for _, op := range []operation{
		{State: "complete", Value: json.RawMessage(`{"id":"original","outcome":"unknown"}`)},
		{State: "pending", Digest: "original-digest", Owner: "original-session"},
		{State: "complete", Error: "operation_unconfirmed", Digest: "original-digest", Owner: "original-session"},
	} {
		s.operations["original"] = op
		if s.UpdateQuiescent() {
			t.Fatal("unknown operation admitted restart")
		}
		if !reflect.DeepEqual(s.operations["original"], op) {
			t.Fatal("operation identity rewritten")
		}
	}
	s.operations["original"] = operation{State: "complete"}
	if !s.UpdateQuiescent() {
		t.Fatal("completed operation blocked")
	}
}
