package websurface

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestReadAdmissionReleasesOnlyAuthenticatedFixedReads(t *testing.T) {
	for _, tc := range []struct {
		name                string
		read, authenticated bool
	}{{"read", true, true}, {"write", false, true}, {"unauthenticated", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t)
			c := pairClient(t, s)
			released := false
			called := false
			s.register(action{Path: "/api/v1/test-held-observation", Read: tc.read, Function: func() error {
				called = true
				if released != tc.read {
					t.Error("wrong drain boundary")
				}
				return nil
			}})
			data, _ := json.Marshal(map[string]any{"id": "isolated-observation-call", "args": []any{}})
			r := httptest.NewRequest("POST", testOrigin+"/api/v1/test-held-observation", bytes.NewReader(data))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", testOrigin)
			r.Header.Set("X-Caelis-Request", "1")
			r.Header.Set("X-CSRF-Token", c.csrf)
			if tc.authenticated {
				r.AddCookie(c.cookie)
			}
			r = r.WithContext(WithReadAdmissionRelease(r.Context(), func() { released = true }))
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if called != tc.authenticated || released != (tc.authenticated && tc.read) {
				t.Fatal("untrusted method/path released host write drain", called, released, w.Code)
			}
		})
	}
}
