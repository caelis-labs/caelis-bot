package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// The real native runtime launches this same isolated fixture binary as its
// ordinary Bot MCP helper. No production application entry point is replaced.
func TestMain(m *testing.M) {
	if os.Getenv("CAELIS_BOT_NATIVE_WORKER_FIXTURE") == "1" && len(os.Args) > 1 && os.Args[1] == "--bot-tools" {
		if bot.RunStdio(os.Stdin, os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type nativePrimaryFixture struct {
	app    *Application
	source api.WorkSourceProvider
	pair   workerwire.Pair
}

// The provider is synthetic; the CLI, Bot assembly, native user submission and
// resulting source attestation are real. No static native activation is supplied.
func openNativePrimaryFixture(t *testing.T, binary, nodeID string) *nativePrimaryFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	t.Setenv("CODEX_BIN", binary)
	t.Setenv("CAELIS_CODEX_SOCKET", "")
	t.Setenv("CAELIS_BOT_NATIVE_WORKER_FIXTURE", "1")
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(404)
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 4<<20))
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) {
			b, _ := json.Marshal(v)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": "isolated-primary", "status": "in_progress", "output": []any{}}})
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		item := map[string]any{"id": "isolated-primary-message", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "Primary fixture completed.", "annotations": []any{}}}}
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": "isolated-primary", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	t.Cleanup(provider.Close)
	// Release before owner close: no pending synthetic network request is left.

	config := fmt.Sprintf("model = \"worker-primary-fixture\"\nmodel_provider = \"worker-primary-fixture\"\n[model_providers.worker-primary-fixture]\nname = \"Isolated native primary\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", provider.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "profile")
	if err := os.Mkdir(profile, 0700); err != nil {
		t.Fatal(err)
	}
	runtimeConfig, _ := json.Marshal(api.RuntimeSettings{Runtime: "codex", CLIPath: binary})
	if err := os.WriteFile(filepath.Join(profile, "runtime.json"), runtimeConfig, 0600); err != nil {
		t.Fatal(err)
	}
	app, err := New(profile, Host{ResolveFiles: func([]string) ([]api.InputFile, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error("isolated primary shutdown failed", err)
		}
	})
	t.Cleanup(func() { close(release) })
	if err = app.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	observer := app.engine.(api.SnapshotObserver)
	for snapshot := app.engine.Snapshot(); snapshot.Connection != "ready"; {
		snapshot, err = observer.WaitSnapshot(ctx, snapshot.Revision)
		if err != nil {
			t.Fatal("native primary connect", err, "connection", app.engine.Snapshot().Connection, "phase", app.engine.Snapshot().Phase)
		}
	}
	receipt, err := app.engine.Submit(ctx, api.Submission{ID: "native-worker-primary", Text: "Authorize this isolated native Worker integration gate while this Bot turn is active."}, nil)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal("actual native primary submission rejected", receipt.Outcome, err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("synthetic primary provider not reached", ctx.Err())
	}
	source := app.engine.(api.WorkSourceProvider)
	actual, err := source.WorkDispatchSource(ctx)
	if err != nil || actual.Validate() != nil || actual.NodeID != api.LocalNodeID || actual.Backend != "codex" || actual.Kind != "native_activation" {
		t.Fatal("actual primary source missing", err)
	}
	// Never expose raw native IDs in test logs or public evidence.
	t.Log("genuine native primary activation attested", "bindingDigest", nativeIDHash(actual.BindingID), "operationDigest", nativeIDHash(actual.OperationID))
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: nodeID, Backend: "codex", Role: api.RoleWorker}, BotID: productrpc.ProfileBotID(app.companion.State().ID), SourceNode: actual.NodeID, SourceBackend: actual.Backend}
	return &nativePrimaryFixture{app: app, source: source, pair: pair}
}

func nativeIDHash(id string) string {
	value := sha256.Sum256([]byte(id))
	return hex.EncodeToString(value[:])
}

func TestNativeCodexPrimarySource(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_NATIVE_PRIMARY")
	if binary == "" {
		t.Skip("set CAELIS_BOT_TEST_NATIVE_PRIMARY to the installed CLI; synthetic loopback only")
	}
	primary := openNativePrimaryFixture(t, binary, "isolated-worker")
	if _, err := primary.source.WorkDispatchSource(t.Context()); err != nil {
		t.Fatal(err)
	}
}
