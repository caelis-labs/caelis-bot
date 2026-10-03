package caelis

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Opt-in read-only native acceptance: existing credentials are used solely for
// GET observations. Projection writes go to t.TempDir, never the live profile.
func TestExistingNativeHistoryPages(t *testing.T) {
	profile := os.Getenv("CAELIS_BOT_HISTORY_PROFILE")
	if profile == "" {
		t.Skip("set CAELIS_BOT_HISTORY_PROFILE for read-only history acceptance")
	}
	directory := filepath.Join(profile, "providers", "caelis")
	b, err := loadBinding(filepath.Join(directory, "application.json"))
	if err != nil {
		t.Fatal("cannot read existing binding")
	}
	raw, err := privateRead(filepath.Join(directory, "application-credential.json"), 65536)
	if err != nil {
		t.Fatal("cannot read existing scoped credential")
	}
	var key credential
	if json.Unmarshal(raw, &key) != nil {
		t.Fatal("invalid scoped credential")
	}
	c, err := newClient(b.Endpoint, key.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.http.CloseIdleConnections()
	s := New(Options{Directory: filepath.Join(t.TempDir(), "projection")})
	s.client, s.connected, s.state = c, true, b
	sid, instance := b.Session.SessionId, b.InstanceID
	s.state.Views = map[string]*view{sid: {Items: []api.Item{}, Seen: map[string]bool{}}}
	s.state.PastSessions = nil
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- s.watch(ctx, c, sid, instance) }()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		s.mu.Lock()
		caughtUp := s.state.Views[sid].CommandCaughtUp
		s.mu.Unlock()
		if caughtUp {
			break
		}
		select {
		case err := <-done:
			t.Fatal("history observation failed before sync", err)
		case <-ctx.Done():
			t.Fatal("history observation did not sync")
		case <-tick.C:
		}
	}
	elapsed := time.Since(started)
	cancel()
	<-done
	s.mu.Lock()
	v := s.state.Views[sid]
	state, cursor, before, items := clone(v.State), v.Cursor, v.HistoryBefore, clone(v.Items)
	s.mu.Unlock()
	if before == "" || len(items) == 0 {
		t.Fatal("selected history has no earlier page")
	}
	started = time.Now()
	if err := s.LoadEarlier(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(v.Items) <= len(items) || v.HistoryBefore == before || v.Cursor != cursor || !reflect.DeepEqual(state, v.State) || !reflect.DeepEqual(items, v.Items[len(v.Items)-len(items):]) {
		t.Fatal("native page did not safely prepend")
	}
	t.Logf("recent recovery: %s, %d display items; earlier page: %s, %d added display items", elapsed.Round(time.Millisecond), len(items), time.Since(started).Round(time.Millisecond), len(v.Items)-len(items))
	if !b.Connection.ExpiresAt.After(time.Now().Add(3 * time.Second)) {
		t.Log("callback wait requires the live Bot's renewed connection lease; observation did not renew it")
		return
	}
	waitCtx, stopWait := context.WithTimeout(t.Context(), 2*time.Second)
	defer stopWait()
	started = time.Now()
	calls, err := waitCalls(waitCtx, c, sid)
	if err != nil {
		t.Fatal("native callback wait failed", err)
	}
	for _, call := range calls {
		if call.State != "pending" {
			t.Fatal("native callback wait returned completed history")
		}
	}
	t.Logf("native callback wait: %s, %d pending calls", time.Since(started).Round(time.Millisecond), len(calls))
}
