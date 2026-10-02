//go:build darwin || linux

package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/coder/websocket"
)

// The retained owner is a real launched child and private native endpoint.
// The fixture implements metadata/thread creation only, never a model turn.
func TestLocalCodexHealthProjectsActualOwnedSession(t *testing.T) {
	if !codex.OwnedRuntimeSupported() {
		t.Skip("this build cannot prove native owned runtime fencing")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex-health-fixture")
	body := "#!/bin/sh\nexec " + nodeShellQuote(executable) + " -test.run='^TestLocalCodexHealthOwnedHelper$' -- \"$@\"\n"
	if err = os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	session := codex.NewSession(codex.SessionOptions{Binary: binary, Socket: filepath.Join(dir, "absent-shared.sock"), Directory: dir, StateFile: filepath.Join(dir, "conversation.json")})
	closed := false
	t.Cleanup(func() {
		if !closed {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := session.Close(ctx); err != nil {
				t.Error("owned metadata fixture cleanup", err)
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err = session.Connect(ctx); err != nil || !session.OwnsLiveRuntime() {
		t.Fatal("fixture did not establish real owned native handles", err)
	}
	a := localCodexHealthApplication(t, session, session)
	health, err := nodeLocalHealth(ctx, a, api.NodeCodex)
	supported := codex.OwnedRuntimeSupported()
	if err != nil || !health.ManagedOwner || health.Fenceable != supported || health.BotEligible != supported || !health.WorkerEligible || !health.Healthy || !health.Authenticated {
		t.Fatal("actual active owned Session lost Bot capability projection", health, err)
	}
	// A still-live Codex owner is not authority for another active provider.
	inactive := localCodexHealthApplication(t, session, newTestEngine())
	health, err = nodeLocalHealth(ctx, inactive, api.NodeCodex)
	if err != nil || health.ManagedOwner || health.Fenceable || health.BotEligible {
		t.Fatal("inactive Codex owner was advertised for active provider", health, err)
	}
	if err = session.Close(ctx); err != nil {
		t.Fatal("owned metadata fixture stop was not confirmed", err)
	}
	closed = true
	health, err = nodeLocalHealth(ctx, a, api.NodeCodex)
	if err != nil || health.ManagedOwner || health.Fenceable || health.BotEligible {
		t.Fatal("closed owner retained Bot eligibility", health, err)
	}
}

func TestLocalCodexHealthOwnedHelper(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	if len(args) != 3 || args[0] != "app-server" || args[1] != "--listen" || !strings.HasPrefix(args[2], "unix://") {
		os.Exit(2)
	}
	listener, err := net.Listen("unix", strings.TrimPrefix(args[2], "unix://"))
	if err != nil {
		os.Exit(3)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		for {
			_, body, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(body, &request) != nil {
				os.Exit(4)
			}
			if len(request.ID) == 0 {
				continue
			}
			var result any
			switch request.Method {
			case "initialize":
				result = map[string]string{"userAgent": "owned-local-health-fixture"}
			case "account/read":
				result = map[string]bool{"requiresOpenaiAuth": false}
			case "thread/start":
				result = map[string]any{"thread": map[string]string{"id": "owned-health-thread"}, "model": "metadata-fixture", "modelProvider": "synthetic"}
			case "model/list", "skills/list", "mcpServerStatus/list", "thread/backgroundTerminals/list":
				result = map[string]any{"data": []any{}}
			case "thread/backgroundTerminals/clean":
				result = map[string]any{}
			default:
				// A model/turn or any unreviewed operation fails the fixture.
				os.Exit(5)
			}
			response, _ := json.Marshal(map[string]any{"id": request.ID, "result": result})
			if ws.Write(r.Context(), websocket.MessageText, response) != nil {
				return
			}
		}
	})}
	_ = server.Serve(listener)
	os.Exit(0)
}
