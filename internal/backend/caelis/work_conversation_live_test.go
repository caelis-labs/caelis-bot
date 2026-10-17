package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Explicit, read-only acceptance against original completed workers. No model
// requests, receipts, live bindings or transcript content are written or logged.
func TestExistingWorkerConversations(t *testing.T) {
	profile := os.Getenv("CAELIS_BOT_WORKER_HISTORY_PROFILE")
	if profile == "" {
		t.Skip("set CAELIS_BOT_WORKER_HISTORY_PROFILE for read-only native acceptance")
	}
	b, err := loadBinding(filepath.Join(profile, "providers", "caelis", "application.json"))
	if err != nil {
		t.Fatal("cannot read original binding")
	}
	d, token, err := Discover(api.RuntimeSettings{CaelisStore: b.StoreDirectory})
	if err != nil || d.InstanceID != b.InstanceID || d.PrincipalID != b.PrincipalID {
		t.Fatal("original Host identity unavailable")
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.http.CloseIdleConnections()
	s := New(Options{Directory: t.TempDir()})
	s.client, s.connected, s.state = c, true, b
	count := 0
	for id, w := range b.Workers {
		if w.Binding.SessionId == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		started := time.Now()
		messages, readErr := s.ReadWorkConversation(ctx, id)
		if readErr != nil {
			// Sanitized delivery shape makes a protocol mismatch diagnosable
			// without printing the user's prompts, messages or native IDs.
			e := c.stream(ctx, "/sessions/"+idPath(w.Binding.SessionId)+"/reconnect?history_turns=64", "", func(f frame) error {
				var delivery wire.SessionFeedDelivery
				_ = json.Unmarshal(f.data, &delivery)
				t.Logf("event=%s kind=%s source=%s cursor=%t history=%t page=%d events=%d", f.event, delivery.Kind, delivery.Source, value(delivery.NextCursor) != "", value(delivery.HistoryBefore) != "", value(delivery.Page), len(delivery.Events))
				if delivery.Kind == "sync" {
					return historyComplete
				}
				return nil
			})
			if !errors.Is(e, historyComplete) {
				t.Logf("observation stopped (%T)", e)
			}
			t.Error(readErr)
		} else {
			roles := map[string]int{}
			for _, message := range messages {
				roles[message.Role]++
			}
			if roles["user"] == 0 || roles["assistant"] == 0 {
				t.Error("original worker has no completed exchange")
			}
			t.Logf("worker %d: user=%d assistant=%d elapsed=%s", count+1, roles["user"], roles["assistant"], time.Since(started).Round(time.Millisecond))
		}
		cancel()
		count++
	}
	if count == 0 {
		t.Fatal("no original workers to verify")
	}
}
