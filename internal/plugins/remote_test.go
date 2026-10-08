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
	"testing"
	"testing/fstest"

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
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fixture","version":"1"}}}`)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"find\"}]}}\n\n")
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
