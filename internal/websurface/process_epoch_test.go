package websurface

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestHTTPAndEventSnapshotsShareProcessEpoch(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	read := func(path string, args []any, target any) {
		t.Helper()
		response := c.request("/api/v1/conversation/"+path, map[string]any{"args": args})
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d: %s", path, response.Code, response.Body.String())
		}
		var envelope struct {
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(envelope.Value, target); err != nil {
			t.Fatal(err)
		}
	}
	var snapshot, pet api.Snapshot
	read("snapshot", []any{}, &snapshot)
	if snapshot.ProcessEpoch == "" {
		t.Fatal("HTTP snapshot omitted processEpoch")
	}
	read("pet-snapshot", []any{}, &pet)
	if pet.ProcessEpoch != snapshot.ProcessEpoch {
		t.Fatal("HTTP pet snapshot epoch differs from full snapshot")
	}
	for _, epoch := range []string{"", "previous-process"} {
		var update api.ChatUpdate
		read("chat-snapshot", []any{snapshot.Revision, snapshot.BotStatus, epoch}, &update)
		if !update.Changed || update.Snapshot.ProcessEpoch != snapshot.ProcessEpoch {
			t.Fatalf("HTTP chat did not refresh mismatched epoch: %+v", update)
		}
	}
	var unchanged api.ChatUpdate
	read("chat-snapshot", []any{snapshot.Revision, snapshot.BotStatus, snapshot.ProcessEpoch}, &unchanged)
	if unchanged.Changed {
		t.Fatal("HTTP chat ignored matching epoch")
	}

	h := httptest.NewServer(s)
	defer h.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "127.0.0.1:9317"
	request.AddCookie(c.cookie)
	response, err := h.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") || line == "data: {}" {
			continue
		}
		var update api.ChatUpdate
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &update); err != nil {
			t.Fatal(err)
		}
		if !update.Changed || update.Snapshot.ProcessEpoch != snapshot.ProcessEpoch {
			t.Fatalf("SSE epoch differs from HTTP snapshots: %+v", update)
		}
		return
	}
	t.Fatalf("SSE ended without a snapshot: %v", scanner.Err())
}
