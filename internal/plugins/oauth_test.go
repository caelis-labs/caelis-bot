package plugins

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/secretstore"
)

type oauthFixture struct {
	server            *httptest.Server
	manager           *Manager
	mu                sync.Mutex
	secrets           map[string]string
	tokenCalls        atomic.Int32
	resourceCalls     atomic.Int32
	verifierChallenge string
	tokenError        bool
}

func newOAuthFixture(t *testing.T) *oauthFixture {
	t.Helper()
	if !oauthNativeSupported {
		t.Skip("native Bot secret store unavailable")
	}
	f := &oauthFixture{secrets: map[string]string{}}
	var service string
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			f.resourceCalls.Add(1)
			if r.Header.Get("Authorization") == "Bearer ACCESS" || r.Header.Get("Authorization") == "Bearer ROTATED" {
				var rpc struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&rpc); err != nil {
					t.Error(err)
				}
				if rpc.Method == "notifications/initialized" {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if rpc.Method == "initialize" {
					fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}}`, rpc.ID)
				} else if rpc.Method == "tools/call" {
					fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"fixture result"}]}}`, rpc.ID)
				} else {
					fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"find","description":"Find workspace pages"}]}}`, rpc.ID)
				}
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+service+`/.well-known/oauth-protected-resource/mcp", scope="default"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/.well-known/oauth-protected-resource/mcp":
			json.NewEncoder(w).Encode(resourceMetadata{Resource: service + "/mcp", AuthorizationServers: []string{service}, ScopesSupported: []string{"default"}})
		case "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(authorizationMetadata{Issuer: service, AuthorizationEndpoint: service + "/authorize", TokenEndpoint: service + "/token", RegistrationEndpoint: service + "/register", CodeChallengeMethodsSupported: []string{"S256"}, AuthorizationResponseISSParameterSupported: true})
		case "/register":
			var request struct {
				RedirectURIs []string `json:"redirect_uris"`
				AuthMethod   string   `json:"token_endpoint_auth_method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.RedirectURIs) != 1 || !strings.HasPrefix(request.RedirectURIs[0], "http://127.0.0.1:") || request.AuthMethod != "none" {
				t.Errorf("invalid DCR request: %v %+v", err, request)
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"client_id":"FIXTURE_CLIENT","token_endpoint_auth_method":"none"}`)
		case "/token":
			f.tokenCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("resource") != service+"/mcp" || r.Form.Get("client_id") != "FIXTURE_CLIENT" {
				t.Errorf("token request missing resource/client: %v", r.Form)
			}
			if r.Form.Get("grant_type") == "authorization_code" {
				verifier := r.Form.Get("code_verifier")
				digest := sha256.Sum256([]byte(verifier))
				if base64.RawURLEncoding.EncodeToString(digest[:]) != f.verifierChallenge || r.Form.Get("code") != "FIXTURE_CODE" {
					t.Errorf("invalid PKCE exchange: %v", r.Form)
				}
			} else if r.Form.Get("grant_type") == "refresh_token" {
				if r.Form.Get("refresh_token") != "REFRESH" {
					t.Errorf("stale refresh token: %v", r.Form)
				}
			} else {
				t.Errorf("unknown grant type")
			}
			w.Header().Set("Content-Type", "application/json")
			if f.tokenError {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)
				return
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				fmt.Fprint(w, `{"access_token":"ROTATED","refresh_token":"REFRESH2","token_type":"Bearer","expires_in":3600}`)
			} else {
				fmt.Fprint(w, `{"access_token":"ACCESS","refresh_token":"REFRESH","token_type":"Bearer","expires_in":3600}`)
			}
		default:
			t.Errorf("unexpected OAuth path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.server.Close)
	service = f.server.URL
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"fixture","version":"1.0.0","description":"Fixture","author":{"name":"Fixture"}}`)
	mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"docs":{"type":"streamable-http","url":"` + service + `/mcp"}}}`)
	hash := func(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
	entry := Entry{ID: "fixture", Title: "Fixture", Version: "1.0.0", Description: "Fixture", Source: service, Files: map[string]string{"plugin.json": hash(manifest), "mcp.json": hash(mcp)}, Connection: &ConnectionSpec{Server: "docs", Kind: "oauth"}}
	m, err := openCatalog(t.TempDir(), []Entry{entry}, map[string]fs.FS{"fixture": fstest.MapFS{"plugin.json": &fstest.MapFile{Data: manifest}, "mcp.json": &fstest.MapFile{Data: mcp}}})
	if err != nil {
		t.Fatal(err)
	}
	m.secrets = secretstore.Functions{SaveFunc: func(id, value string) error { f.mu.Lock(); defer f.mu.Unlock(); f.secrets[id] = value; return nil }, LoadFunc: func(id string) (string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		v, ok := f.secrets[id]
		if !ok {
			return "", errors.New("missing")
		}
		return v, nil
	}, DeleteFunc: func(id string) error { f.mu.Lock(); defer f.mu.Unlock(); delete(f.secrets, id); return nil }}
	m.oauthClient = f.server.Client()
	f.manager = m
	if _, err := m.Mutate(t.Context(), "fixture", "install", nil); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestOAuthOldRelaySurvivesUnconfirmedReplacement(t *testing.T) {
	f := newOAuthFixture(t)
	authorizeFixture(t, f)
	selected := f.manager.Selection().Servers[0]
	old := selected.ConnectionRevision
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- runStdio(t.Context(), f.manager, "fixture", "docs", inReader, outWriter, io.Discard, selected.Root[strings.LastIndex(selected.Root, "/")+1:], fmt.Sprint(old))
		outWriter.Close()
	}()
	responses := bufio.NewScanner(outReader)
	call := func(id int) {
		t.Helper()
		if _, err := fmt.Fprintf(inWriter, `{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"find","arguments":{}}}`+"\n", id); err != nil {
			t.Fatal(err)
		}
		if !responses.Scan() || !strings.Contains(responses.Text(), `"id":`+fmt.Sprint(id)) || !strings.Contains(responses.Text(), "fixture result") {
			t.Fatal("old relay lost authenticated call", responses.Text(), responses.Err())
		}
	}
	call(1)
	flow := &oauthFlow{state: "replacement", claimed: true, cancel: func() {}}
	f.manager.mu.Lock()
	f.manager.oauthFlows = map[string]*oauthFlow{"fixture": flow}
	f.manager.mu.Unlock()
	replacement := OAuthGrant{ClientID: "new-client", AccessToken: "ROTATED", Resource: f.server.URL + "/mcp"}
	if _, err := f.manager.ConfigureOAuthGrant(t.Context(), "fixture", flow.state, replacement, nil); err != nil {
		t.Fatal(err)
	}
	if f.manager.state.Connections["fixture"].Revision == old {
		t.Fatal("replacement generation was not committed")
	}
	if err := f.manager.ConfirmOAuthProjection(old); err != nil {
		t.Fatal(err)
	}
	beforeSecondCall := f.resourceCalls.Load()
	call(2)
	if f.resourceCalls.Load() != beforeSecondCall+1 {
		t.Fatal("old relay did not reach the upstream after replacement")
	}
	inWriter.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("old relay did not drain")
	}
	if err := f.manager.ConfirmOAuthProjection(f.manager.state.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.secrets.Load(secretKey(f.manager.root, "fixture", old)); err == nil {
		t.Fatal("old grant remained after confirmed switch")
	}
}

func TestOAuthExplicitClearRevokesUnconfirmedOldGenerations(t *testing.T) {
	f := newOAuthFixture(t)
	authorizeFixture(t, f)
	old := f.manager.state.Connections["fixture"].Revision
	authorizeFixture(t, f)
	current := f.manager.state.Connections["fixture"].Revision
	if _, err := f.manager.secrets.Load(secretKey(f.manager.root, "fixture", old)); err != nil {
		t.Fatal("old grant was deleted before projection", err)
	}
	if _, err := f.manager.ConfigureConnection(t.Context(), "fixture", "", "", true, nil); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []uint64{old, current} {
		if _, err := f.manager.secrets.Load(secretKey(f.manager.root, "fixture", revision)); err == nil {
			t.Fatal("explicit clear retained OAuth grant", revision)
		}
	}
}

func TestOAuthUnsupportedPlatform(t *testing.T) {
	if oauthNativeSupported {
		t.Skip("native Bot secret store available")
	}
	m, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range m.Snapshot().Items {
		if item.ID != "notion" {
			continue
		}
		if item.Status != "unavailable" || item.Connection == nil || item.Connection.State != "unsupported" {
			t.Fatal("OAuth did not report platform limit", item)
		}
		if _, err := m.Mutate(t.Context(), "notion", "install", nil); err == nil {
			t.Fatal("unsupported OAuth installed")
		}
		return
	}
	t.Fatal("reviewed Notion entry missing")
}
func (f *oauthFixture) grant(t *testing.T) OAuthGrant {
	t.Helper()
	record := f.manager.state.Connections["fixture"]
	raw, err := f.manager.secrets.Load(secretKey(f.manager.root, "fixture", record.Revision))
	if err != nil {
		t.Fatal(err)
	}
	var grant OAuthGrant
	if err := json.Unmarshal([]byte(raw), &grant); err != nil {
		t.Fatal(err)
	}
	return grant
}
func (f *oauthFixture) saveGrant(t *testing.T, grant OAuthGrant) {
	t.Helper()
	data, _ := json.Marshal(grant)
	record := f.manager.state.Connections["fixture"]
	if err := f.manager.secrets.Save(secretKey(f.manager.root, "fixture", record.Revision), string(data)); err != nil {
		t.Fatal(err)
	}
}
func authorizeFixture(t *testing.T, f *oauthFixture) {
	t.Helper()
	priorRevision := f.manager.state.Connections["fixture"].Revision
	var authURL string
	_, err := f.manager.BeginOAuth(t.Context(), "fixture", func(u string) error { authURL = u; return nil }, func(ctx context.Context, id, state string, grant OAuthGrant) error {
		_, err := f.manager.ConfigureOAuthGrant(ctx, id, state, grant, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("resource") != f.server.URL+"/mcp" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("scope") != "default" {
		t.Fatal("authorization request incorrect", u)
	}
	f.verifierChallenge = u.Query().Get("code_challenge")
	callback := u.Query().Get("redirect_uri")
	if callback == "" {
		t.Fatal("missing callback")
	}
	wrongState := callback + "?code=FIXTURE_CODE&state=wrong&iss=" + url.QueryEscape(f.server.URL)
	response, err := http.Get(wrongState)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 400 || f.manager.state.Connections["fixture"].Revision != priorRevision {
		t.Fatal("wrong state accepted")
	}
	wrongIssuer := callback + "?code=FIXTURE_CODE&state=" + url.QueryEscape(u.Query().Get("state")) + "&iss=" + url.QueryEscape("https://wrong.example")
	response, err = http.Get(wrongIssuer)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 400 || f.manager.state.Connections["fixture"].Revision != priorRevision {
		t.Fatal("wrong issuer accepted")
	}
	valid := callback + "?code=FIXTURE_CODE&state=" + url.QueryEscape(u.Query().Get("state")) + "&iss=" + url.QueryEscape(f.server.URL)
	response, err = http.Get(valid)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !f.manager.state.Connections["fixture"].Configured || f.manager.state.Connections["fixture"].Revision == priorRevision {
		t.Fatalf("OAuth callback failed: %d %s", response.StatusCode, body)
	}
	response, err = http.Get(valid)
	if err == nil {
		response.Body.Close()
	}
	if err == nil && response.StatusCode == 200 {
		t.Fatal("callback replay accepted")
	}
}
func TestOAuthDiscoveryCallbackAndRelay(t *testing.T) {
	f := newOAuthFixture(t)
	authorizeFixture(t, f)
	if len(f.manager.Selection().Servers) != 1 {
		t.Fatal("OAuth service not selected")
	}
	grant := f.grant(t)
	if grant.AccessToken != "ACCESS" || grant.RefreshToken != "REFRESH" || grant.TokenRevision != 1 {
		t.Fatal("grant not privately stored")
	}
	serialized, _ := json.Marshal(f.manager.Snapshot())
	if bytes.Contains(serialized, []byte("ACCESS")) || bytes.Contains(serialized, []byte("FIXTURE_CLIENT")) {
		t.Fatal("credential leaked to snapshot")
	}
	selectedJSON, _ := json.Marshal(f.manager.Selection())
	if bytes.Contains(selectedJSON, []byte("ACCESS")) || bytes.Contains(selectedJSON, []byte("FIXTURE_CLIENT")) {
		t.Fatal("credential leaked to Runtime selection")
	}
	selected := f.manager.Selection().Servers[0]
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	err := runStdio(t.Context(), f.manager, "fixture", "docs", strings.NewReader(input), &out, io.Discard, selected.Root[strings.LastIndex(selected.Root, "/")+1:], fmt.Sprint(selected.ConnectionRevision))
	if err != nil || !strings.Contains(out.String(), "find") || strings.Contains(out.String(), "ACCESS") {
		t.Fatal("OAuth relay failed", err, out.String())
	}
	if detail := f.manager.ProbeServer(t.Context(), "fixture", "docs"); detail.State != "connected" || len(detail.Tools) != 1 || detail.Tools[0].Name != "find" {
		t.Fatal("connected tool directory was not shown", detail)
	}
}
func TestOAuthRefreshRotationAndUnknownCallNoReplay(t *testing.T) {
	f := newOAuthFixture(t)
	authorizeFixture(t, f)
	grant := f.grant(t)
	grant.ExpiresAt = time.Now().Add(-time.Minute)
	f.saveGrant(t, grant)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := f.manager.oauthBearer(t.Context(), "fixture", f.manager.state.Connections["fixture"].Revision, "")
			if err != nil || token != "ROTATED" {
				t.Errorf("refresh failed: %v %q", err, token)
			}
		}()
	}
	wg.Wait()
	if f.tokenCalls.Load() != 2 || f.grant(t).RefreshToken != "REFRESH2" || f.grant(t).TokenRevision != 2 {
		t.Fatal("refresh was not serialized or rotated", f.tokenCalls.Load())
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":77,"method":"tools/call"}` + "\n"
	err := relayRemoteWithOAuth(t.Context(), Server{Type: "streamable-http", URL: server.URL}, &ConnectionSpec{Kind: "oauth"}, "", "", func(context.Context, string) (string, error) { return "ROTATED", nil }, strings.NewReader(input), &out)
	if err != nil || calls.Load() != 1 || !strings.Contains(out.String(), `"id":77`) {
		t.Fatal("unknown tool call replayed", err, calls.Load(), out.String())
	}
}
func TestOAuthCancelAndRollback(t *testing.T) {
	f := newOAuthFixture(t)
	var authURL string
	_, err := f.manager.BeginOAuth(t.Context(), "fixture", func(u string) error { authURL = u; return nil }, func(context.Context, string, string, OAuthGrant) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	callback := u.Query().Get("redirect_uri")
	f.manager.CancelOAuth("fixture")
	if len(f.manager.Selection().Servers) != 0 {
		t.Fatal("cancelled OAuth selected")
	}
	if _, err := http.Get(callback + "?code=FIXTURE_CODE&state=" + url.QueryEscape(u.Query().Get("state"))); err == nil {
		t.Fatal("cancelled callback still listening")
	}
	flow := &oauthFlow{state: "fixture-state", claimed: true}
	f.manager.mu.Lock()
	f.manager.oauthFlows = map[string]*oauthFlow{"fixture": flow}
	f.manager.mu.Unlock()
	grant := OAuthGrant{ClientID: "fixture", AccessToken: "TOKEN", Resource: f.server.URL + "/mcp"}
	original := f.manager.state.Revision
	_, err = f.manager.ConfigureOAuthGrant(t.Context(), "fixture", flow.state, grant, func(context.Context, Selection) error { return errors.New("apply rejected") })
	if err == nil || f.manager.state.Revision != original || len(f.manager.Selection().Servers) != 0 {
		t.Fatal("failed apply activated OAuth", err)
	}
	f.mu.Lock()
	count := len(f.secrets)
	f.mu.Unlock()
	if count != 0 {
		t.Fatal("failed apply retained grant")
	}
}

func TestOAuthInvalidGrantAndReauthorizationRollback(t *testing.T) {
	f := newOAuthFixture(t)
	authorizeFixture(t, f)
	initial := f.manager.state.Connections["fixture"]
	grant := f.grant(t)
	grant.ExpiresAt = time.Now().Add(-time.Minute)
	f.saveGrant(t, grant)
	f.tokenError = true
	if _, err := f.manager.oauthBearer(t.Context(), "fixture", initial.Revision, ""); err == nil {
		t.Fatal("invalid_grant accepted")
	}
	if f.tokenCalls.Load() != 2 || f.grant(t).AccessToken != "" {
		t.Fatal("terminal invalid_grant was retried or retained")
	}
	if state := f.manager.Snapshot().Items[0].Connection.State; state != "authentication_required" {
		t.Fatal("reauthorization not shown", state)
	}
	flow := &oauthFlow{state: "replacement", claimed: true, cancel: func() {}}
	f.manager.mu.Lock()
	f.manager.oauthFlows = map[string]*oauthFlow{"fixture": flow}
	f.manager.mu.Unlock()
	replacement := OAuthGrant{ClientID: "new-client", AccessToken: "NEW", Resource: f.server.URL + "/mcp"}
	_, err := f.manager.ConfigureOAuthGrant(t.Context(), "fixture", flow.state, replacement, func(context.Context, Selection) error { return errors.New("runtime rejected replacement") })
	if err == nil || f.manager.state.Connections["fixture"].Revision != initial.Revision {
		t.Fatal("failed reauthorization lost old connection", err)
	}
	if _, err := f.manager.secrets.Load(secretKey(f.manager.root, "fixture", initial.Revision)); err != nil {
		t.Fatal("failed reauthorization deleted old grant")
	}
	if _, err := f.manager.ConfigureConnection(t.Context(), "fixture", "", "", true, nil); err != nil {
		t.Fatal(err)
	}
	if f.manager.state.Connections["fixture"].Configured || len(f.manager.Selection().Servers) != 0 {
		t.Fatal("disconnect left OAuth selected")
	}
}

func TestOAuthReauthorizationDisableAndUninstall(t *testing.T) {
	f := newOAuthFixture(t)
	authorizeFixture(t, f)
	old := f.manager.state.Connections["fixture"].Revision
	authorizeFixture(t, f)
	if f.manager.state.Connections["fixture"].Revision == old {
		t.Fatal("reauthorization kept old revision")
	}
	if _, err := f.manager.secrets.Load(secretKey(f.manager.root, "fixture", old)); err != nil {
		t.Fatal("old grant was revoked before Runtime switched")
	}
	if err := f.manager.ConfirmOAuthProjection(f.manager.state.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.secrets.Load(secretKey(f.manager.root, "fixture", old)); err == nil {
		t.Fatal("old grant retained after Runtime switch")
	}
	if _, err := f.manager.Mutate(t.Context(), "fixture", "disable", nil); err != nil {
		t.Fatal(err)
	}
	if len(f.manager.Selection().Servers) != 0 {
		t.Fatal("disabled OAuth service selected")
	}
	if _, err := f.manager.Mutate(t.Context(), "fixture", "enable", nil); err != nil {
		t.Fatal(err)
	}
	if len(f.manager.Selection().Servers) != 1 {
		t.Fatal("enabled OAuth service missing")
	}
	current := f.manager.state.Connections["fixture"].Revision
	if _, err := f.manager.Mutate(t.Context(), "fixture", "uninstall", nil); err != nil {
		t.Fatal(err)
	}
	if len(f.manager.Selection().Servers) != 0 {
		t.Fatal("uninstalled OAuth service selected")
	}
	if _, err := f.manager.secrets.Load(secretKey(f.manager.root, "fixture", current)); err == nil {
		t.Fatal("uninstalled grant retained")
	}
}
