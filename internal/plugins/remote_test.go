package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/secretstore"
)

func TestRemoteMCPProtocolBridgeListsAndCallsWithoutCredentialLeak(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer SYNTHETIC_PRIVATE_TOKEN" {
			t.Error("credential missing")
		}
		if len(methods) > 0 && r.Header.Get("Mcp-Session-Id") != "fixture-session" {
			t.Error("session not retained")
		}
		var body struct {
			Method string `json:"method"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
			return
		}
		methods = append(methods, body.Method)
		switch body.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "fixture-session")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "{\n  \"jsonrpc\": \"2.0\",\n  \"id\": 1,\n  \"result\": {\"protocolVersion\":\"2025-06-18\",\"capabilities\":{\"tools\":{}},\"serverInfo\":{\"name\":\"fixture\",\"version\":\"1\"}}\n}")
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\ndata: \"id\":2,\"result\":{\"tools\":[{\"name\":\"find\"}]}}\n\n")
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"fixture result"}]}}`)
		default:
			t.Errorf("unexpected method %s", body.Method)
		}
	}))
	defer server.Close()
	input := strings.Join([]string{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"find","arguments":{}}}`}, "\n") + "\n"
	var out bytes.Buffer
	if err := relayRemote(Server{Type: "streamable-http", URL: server.URL}, &ConnectionSpec{Placement: "header", Name: "Authorization", Prefix: "Bearer "}, "SYNTHETIC_PRIVATE_TOKEN", "", strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "SYNTHETIC_PRIVATE_TOKEN") || !strings.Contains(out.String(), `"name":"find"`) || !strings.Contains(out.String(), "fixture result") || len(methods) != 4 {
		t.Fatal("protocol bridge mismatch", out.String(), methods)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatal("stdio bridge split a JSON-RPC response across lines", len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal("stdio bridge emitted a non-JSON line")
		}
	}
}

func TestReviewedPackageConnectionLaunchesHTTPDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "SYNTHETIC_PRIVATE_TOKEN" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"list_docs"}]}}`)
	}))
	defer server.Close()
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"fixture","version":"1.0.0","description":"Fixture","author":{"name":"Fixture"}}`)
	mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"docs":{"type":"streamable-http","url":"` + server.URL + `"}}}`)
	hash := func(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
	entry := Entry{ID: "fixture", Title: "Fixture", Version: "1.0.0", Description: "Fixture", Source: "https://example.test/source", Files: map[string]string{"plugin.json": hash(manifest), "mcp.json": hash(mcp)}, Connection: &ConnectionSpec{Server: "docs", Kind: "token", Placement: "header", Name: "Authorization"}}
	m, err := openCatalog(t.TempDir(), []Entry{entry}, map[string]fs.FS{"fixture": fstest.MapFS{"plugin.json": &fstest.MapFile{Data: manifest}, "mcp.json": &fstest.MapFile{Data: mcp}}})
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	m.secrets = secretstore.Functions{SaveFunc: func(id, v string) error { secrets[id] = v; return nil }, LoadFunc: func(id string) (string, error) { return secrets[id], nil }, DeleteFunc: func(id string) error { delete(secrets, id); return nil }}
	if _, err = m.Mutate(context.Background(), "fixture", "install", nil); err != nil {
		t.Fatal(err)
	}
	if len(m.Selection().Servers) != 0 {
		t.Fatal("unconfigured service projected")
	}
	if _, err = m.ConfigureConnection(context.Background(), "fixture", "SYNTHETIC_PRIVATE_TOKEN", "", false, nil); err != nil {
		t.Fatal(err)
	}
	selected := m.Selection().Servers[0]
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	err = runStdio(context.Background(), m, "fixture", "docs", strings.NewReader(input), &out, io.Discard, filepath.Base(selected.Root), strconv.FormatUint(selected.ConnectionRevision, 10))
	if err != nil || !strings.Contains(out.String(), "list_docs") || strings.Contains(out.String(), "SYNTHETIC_PRIVATE_TOKEN") {
		t.Fatal("package-to-HTTP chain failed", err, out.String())
	}
}

func TestProbeServerListsPagedToolsWithoutCallingTools(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "SYNTHETIC_PRIVATE_TOKEN" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var request struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		methods = append(methods, request.Method)
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "fixture-session")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}}`, request.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			var params map[string]string
			if len(request.Params) > 0 {
				if err := json.Unmarshal(request.Params, &params); err != nil {
					t.Error(err)
				}
			}
			if r.Header.Get("Mcp-Session-Id") != "fixture-session" {
				t.Error("MCP session lost")
			}
			if params["cursor"] == "" {
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"find","description":"Find documents","annotations":{"readOnlyHint":true}}],"nextCursor":"page-2"}}`, request.ID)
			} else {
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"edit","description":"Edit documents"}]}}`, request.ID)
			}
		default:
			t.Errorf("unexpected MCP method %s", request.Method)
		}
	}))
	defer server.Close()
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"fixture","version":"1.0.0","description":"Fixture","author":{"name":"Fixture"}}`)
	mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"docs":{"type":"streamable-http","url":"` + server.URL + `"}}}`)
	hash := func(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
	entry := Entry{ID: "fixture", Title: "Fixture", Version: "1.0.0", Description: "Fixture", Source: "https://example.test/source", Files: map[string]string{"plugin.json": hash(manifest), "mcp.json": hash(mcp)}, Connection: &ConnectionSpec{Server: "docs", Kind: "token", Placement: "header", Name: "Authorization"}}
	m, err := openCatalog(t.TempDir(), []Entry{entry}, map[string]fs.FS{"fixture": fstest.MapFS{"plugin.json": &fstest.MapFile{Data: manifest}, "mcp.json": &fstest.MapFile{Data: mcp}}})
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	m.secrets = secretstore.Functions{SaveFunc: func(id, value string) error { secrets[id] = value; return nil }, LoadFunc: func(id string) (string, error) { return secrets[id], nil }, DeleteFunc: func(id string) error { delete(secrets, id); return nil }}
	if detail := m.ProbeServer(t.Context(), "fixture", "docs"); detail.State != "not_configured" || len(methods) != 0 {
		t.Fatal("uninstalled package started", detail, methods)
	}
	if _, err := m.Mutate(t.Context(), "fixture", "install", nil); err != nil {
		t.Fatal(err)
	}
	if detail := m.ProbeServer(t.Context(), "fixture", "docs"); detail.State != "not_configured" || len(methods) != 0 {
		t.Fatal("unconfigured service started", detail, methods)
	}
	if _, err := m.ConfigureConnection(t.Context(), "fixture", "SYNTHETIC_PRIVATE_TOKEN", "", false, nil); err != nil {
		t.Fatal(err)
	}
	detail := m.ProbeServer(t.Context(), "fixture", "docs")
	if detail.State != "connected" || len(detail.Tools) != 2 || detail.Tools[0].Name != "edit" || detail.Tools[1].Name != "find" || detail.Tools[1].ReadOnlyHint == nil || !*detail.Tools[1].ReadOnlyHint || strings.Contains(fmt.Sprint(detail), "SYNTHETIC_PRIVATE_TOKEN") {
		t.Fatal("real directory probe failed", detail)
	}
	if fmt.Sprint(methods) != "[initialize notifications/initialized tools/list tools/list]" {
		t.Fatal("probe executed or skipped a method", methods)
	}
	if _, err := m.ConfigureConnection(t.Context(), "fixture", "INVALID_PRIVATE_TOKEN", "", false, nil); err != nil {
		t.Fatal(err)
	}
	if detail := m.ProbeServer(t.Context(), "fixture", "docs"); detail.State != "authentication_required" || len(detail.Tools) != 0 || len(methods) != 4 {
		t.Fatal("authentication failure was not isolated", detail, methods)
	}
	if _, err := m.Mutate(t.Context(), "fixture", "disable", nil); err != nil {
		t.Fatal(err)
	}
	if detail := m.ProbeServer(t.Context(), "fixture", "docs"); detail.State != "not_configured" || len(methods) != 4 {
		t.Fatal("disabled service started", detail, methods)
	}
}

func configuredProbeFixture(t *testing.T, handler http.HandlerFunc) *Manager {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"fixture","version":"1.0.0","description":"Fixture","author":{"name":"Fixture"}}`)
	mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"docs":{"type":"streamable-http","url":"` + server.URL + `"}}}`)
	hash := func(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
	entry := Entry{ID: "fixture", Title: "Fixture", Version: "1.0.0", Description: "Fixture", Source: "https://example.test/source", Files: map[string]string{"plugin.json": hash(manifest), "mcp.json": hash(mcp)}, Connection: &ConnectionSpec{Server: "docs", Kind: "token", Placement: "header", Name: "Authorization"}}
	m, err := openCatalog(t.TempDir(), []Entry{entry}, map[string]fs.FS{"fixture": fstest.MapFS{"plugin.json": &fstest.MapFile{Data: manifest}, "mcp.json": &fstest.MapFile{Data: mcp}}})
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	m.secrets = secretstore.Functions{SaveFunc: func(id, value string) error { secrets[id] = value; return nil }, LoadFunc: func(id string) (string, error) { return secrets[id], nil }, DeleteFunc: func(id string) error { delete(secrets, id); return nil }}
	if _, err := m.Mutate(t.Context(), "fixture", "install", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConfigureConnection(t.Context(), "fixture", "SYNTHETIC_PRIVATE_TOKEN", "", false, nil); err != nil {
		t.Fatal(err)
	}
	return m
}

func probeMCPMethod(t *testing.T, w http.ResponseWriter, r *http.Request) (string, int) {
	t.Helper()
	var request struct {
		Method string `json:"method"`
		ID     int    `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Error(err)
	}
	if request.Method == "initialize" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}}`, request.ID)
	} else if request.Method == "notifications/initialized" {
		w.WriteHeader(http.StatusAccepted)
	}
	return request.Method, request.ID
}

func TestProbeServerSSEEOFAndErrorDoNotWaitForInput(t *testing.T) {
	for _, test := range []struct{ name, event string }{
		{"unrelated response then EOF", "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{}}\n\n"},
		{"SSE error event", "event: error\ndata: upstream unavailable\n\n"},
		{"JSON SSE error event", "event: error\ndata: {\"reason\":\"upstream unavailable\"}\n\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := configuredProbeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if method, _ := probeMCPMethod(t, w, r); method == "tools/list" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, test.event)
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
			defer cancel()
			finished := make(chan ServerDetail, 1)
			go func() { finished <- m.ProbeServer(ctx, "fixture", "docs") }()
			select {
			case detail := <-finished:
				if detail.State != "failed" || len(detail.Tools) != 0 {
					t.Fatal("incomplete SSE response appeared connected", detail)
				}
			case <-time.After(600 * time.Millisecond):
				t.Fatal("probe waited for new input after SSE ended")
			}
		})
	}
}

func TestRemoteMCPUnknownToolCallOutcomeIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Error("unexpected method", r.Method)
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{}}\n\n")
	}))
	defer server.Close()
	var out bytes.Buffer
	err := relayRemote(Server{Type: "streamable-http", URL: server.URL}, nil, "", "", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"write"}}`+"\n"), &out)
	if err != nil || calls.Load() != 1 || !strings.Contains(out.String(), `"id":2`) || !strings.Contains(out.String(), "tool outcome is uncertain") {
		t.Fatal("unknown tool call was lost or replayed", err, calls.Load(), out.String())
	}
}

func TestProbeServerCancellationReapsRepeatedSSERequests(t *testing.T) {
	var active atomic.Int32
	m := configuredProbeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if method, _ := probeMCPMethod(t, w, r); method == "tools/list" {
			active.Add(1)
			defer active.Add(-1)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, ": waiting for tool result\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	})
	for i := 0; i < 12; i++ {
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
		started := time.Now()
		detail := m.ProbeServer(ctx, "fixture", "docs")
		cancel()
		if detail.State != "failed" || time.Since(started) > 600*time.Millisecond {
			t.Fatal("cancelled probe did not exit promptly", i, detail)
		}
	}
	deadline := time.After(600 * time.Millisecond)
	for active.Load() != 0 {
		select {
		case <-deadline:
			t.Fatal("repeated probes left HTTP streams active", active.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestProbeServerSSEUsesMatchingResponseAfterNotifications(t *testing.T) {
	m := configuredProbeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if method, id := probeMCPMethod(t, w, r); method == "tools/list" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"tools\":[{\"name\":\"lookup\"}]}}\n\n", id)
			w.(http.Flusher).Flush()
			<-r.Context().Done() // A held-open stream must not delay the matching result.
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := time.Now()
	detail := m.ProbeServer(ctx, "fixture", "docs")
	if detail.State != "connected" || len(detail.Tools) != 1 || detail.Tools[0].Name != "lookup" || time.Since(started) > 600*time.Millisecond {
		t.Fatal("matching SSE result was not returned promptly", detail)
	}
}

func TestRemoteMCPTrustsOnlyProvidedLocalCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"vault_search"}]}}`)
	}))
	defer server.Close()
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	var without, with bytes.Buffer
	if err := relayRemote(Server{Type: "streamable-http", URL: server.URL}, nil, "", "", strings.NewReader(input), &without); err != nil {
		t.Fatal(err)
	}
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	if err := relayRemote(Server{Type: "streamable-http", URL: server.URL}, nil, "", ca, strings.NewReader(input), &with); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without.String(), "vault_search") || !strings.Contains(with.String(), "vault_search") {
		t.Fatal("TLS trust was not scoped to supplied CA")
	}
}

func TestRemoteMCPAuthFailureAndRedirectAreRedacted(t *testing.T) {
	var attempts int
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "SYNTHETIC_PRIVATE_TOKEN")
	}))
	defer denied.Close()
	var failed bytes.Buffer
	if err := relayRemote(Server{Type: "streamable-http", URL: denied.URL}, nil, "SYNTHETIC_PRIVATE_TOKEN", "", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"), &failed); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || strings.Contains(failed.String(), "SYNTHETIC_PRIVATE_TOKEN") || !strings.Contains(failed.String(), "MCP authentication failed") {
		t.Fatal("invalid token was retried or exposed", attempts, failed.String())
	}
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "SYNTHETIC_PRIVATE_TOKEN" {
			t.Error("query credential absent")
		}
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	if err := relayRemote(Server{Type: "streamable-http", URL: server.URL}, &ConnectionSpec{Placement: "query", Name: "key"}, "SYNTHETIC_PRIVATE_TOKEN", "", strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if redirected || strings.Contains(out.String(), "SYNTHETIC_PRIVATE_TOKEN") || !strings.Contains(out.String(), "MCP response type unsupported") {
		t.Fatal("redirect or credential disclosure", out.String())
	}
}
