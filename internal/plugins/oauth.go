package plugins

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// OAuthGrant is stored as one native secret item. Runtime profiles only carry
// the connection revision; no client identity or token enters a profile.
type OAuthGrant struct {
	ClientID      string    `json:"client_id"`
	ClientSecret  string    `json:"client_secret,omitempty"`
	AuthMethod    string    `json:"auth_method"`
	TokenURL      string    `json:"token_url"`
	Issuer        string    `json:"issuer"`
	Resource      string    `json:"resource"`
	RedirectURI   string    `json:"redirect_uri"`
	Scope         string    `json:"scope,omitempty"`
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
	TokenRevision uint64    `json:"token_revision"`
}

type oauthFlow struct {
	state    string
	userCode string
	claimed  bool
	cancel   context.CancelFunc
	listener net.Listener
}

type resourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
	RequiredScope        string   `json:"-"`
}
type authorizationMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RegistrationEndpoint                       string   `json:"registration_endpoint"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	AuthorizationResponseISSParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
}
type registeredClient struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret,omitempty"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`
}

func oauthURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return nil, errors.New("invalid OAuth endpoint")
	}
	return u, nil
}
func oauthHTTP(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.Timeout = 15 * time.Second
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &copyClient
}
func oauthJSON(ctx context.Context, client *http.Client, method, endpoint string, body io.Reader, target any) (*http.Response, error) {
	if _, err := oauthURL(endpoint); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := oauthHTTP(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res, fmt.Errorf("OAuth endpoint returned HTTP %d", res.StatusCode)
	}
	limited := io.LimitReader(res.Body, 64<<10+1)
	data, err := io.ReadAll(limited)
	if err != nil || len(data) > 64<<10 {
		return res, errors.New("OAuth metadata too large")
	}
	if err = json.Unmarshal(data, target); err != nil {
		return res, errors.New("invalid OAuth response")
	}
	return res, nil
}
func challengeParam(header, key string) string {
	if !strings.Contains(strings.ToLower(header), "bearer") {
		return ""
	}
	pattern := regexp.MustCompile(`(?i)(?:^|[,\s])` + regexp.QuoteMeta(key) + `\s*=\s*(?:"([^"]*)"|([^,\s]+))`)
	match := pattern.FindStringSubmatch(header)
	if len(match) == 0 {
		return ""
	}
	if match[1] != "" {
		return match[1]
	}
	return match[2]
}
func discoverOAuth(ctx context.Context, client *http.Client, resource string) (resourceMetadata, authorizationMetadata, error) {
	var protected resourceMetadata
	var auth authorizationMetadata
	resourceURL, err := oauthURL(resource)
	if err != nil {
		return protected, auth, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
	if err != nil {
		return protected, auth, err
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := oauthHTTP(client).Do(req)
	if err != nil {
		return protected, auth, err
	}
	challenge := res.Header.Get("WWW-Authenticate")
	metadataURL := challengeParam(challenge, "resource_metadata")
	protected.RequiredScope = challengeParam(challenge, "scope")
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	res.Body.Close()
	if metadataURL == "" {
		// RFC 9728 well-known path insertion for a resource with a path.
		metadataURL = resourceURL.Scheme + "://" + resourceURL.Host + "/.well-known/oauth-protected-resource" + resourceURL.EscapedPath()
	}
	metadataParsed, err := oauthURL(metadataURL)
	if err != nil {
		return protected, auth, err
	}
	if metadataParsed.Scheme != resourceURL.Scheme {
		return protected, auth, errors.New("OAuth metadata transport mismatch")
	}
	if _, err = oauthJSON(ctx, client, http.MethodGet, metadataURL, nil, &protected); err != nil {
		return protected, auth, err
	}
	if protected.Resource != resource || len(protected.AuthorizationServers) == 0 {
		return protected, auth, errors.New("OAuth resource metadata mismatch")
	}
	issuer, err := oauthURL(protected.AuthorizationServers[0])
	if err != nil {
		return protected, auth, err
	}
	if issuer.Scheme != resourceURL.Scheme {
		return protected, auth, errors.New("OAuth issuer transport mismatch")
	}
	issuerPath := strings.TrimSuffix(issuer.EscapedPath(), "/")
	metadata := issuer.Scheme + "://" + issuer.Host + "/.well-known/oauth-authorization-server" + issuerPath
	if _, err = oauthJSON(ctx, client, http.MethodGet, metadata, nil, &auth); err != nil {
		return protected, auth, err
	}
	if auth.Issuer != strings.TrimSuffix(protected.AuthorizationServers[0], "/") || auth.RegistrationEndpoint == "" {
		return protected, auth, errors.New("OAuth authorization server mismatch")
	}
	for _, endpoint := range []string{auth.AuthorizationEndpoint, auth.TokenEndpoint, auth.RegistrationEndpoint} {
		u, e := oauthURL(endpoint)
		if e != nil || u.Scheme != issuer.Scheme || u.Host != issuer.Host {
			return protected, auth, errors.New("OAuth endpoint origin mismatch")
		}
	}
	s256 := false
	for _, m := range auth.CodeChallengeMethodsSupported {
		if m == "S256" {
			s256 = true
		}
	}
	if !s256 {
		return protected, auth, errors.New("OAuth server does not support PKCE S256")
	}
	return protected, auth, nil
}
func randomOAuthValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func registerOAuth(ctx context.Context, client *http.Client, endpoint, redirect string) (registeredClient, error) {
	var result registeredClient
	body := map[string]any{"client_name": "Caelis Bot", "redirect_uris": []string{redirect}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"}
	data, _ := json.Marshal(body)
	_, err := oauthJSON(ctx, client, http.MethodPost, endpoint, strings.NewReader(string(data)), &result)
	if err != nil {
		return result, err
	}
	if result.ClientID == "" || (result.TokenEndpointAuthMethod != "" && result.TokenEndpointAuthMethod != "none" && result.TokenEndpointAuthMethod != "client_secret_post" && result.TokenEndpointAuthMethod != "client_secret_basic") {
		return result, errors.New("unsupported OAuth client registration")
	}
	if result.TokenEndpointAuthMethod == "" {
		if result.ClientSecret != "" {
			result.TokenEndpointAuthMethod = "client_secret_basic"
		} else {
			result.TokenEndpointAuthMethod = "none"
		}
	}
	return result, nil
}
func oauthConfig(grant OAuthGrant) oauth2.Config {
	style := oauth2.AuthStyleInParams
	if grant.AuthMethod == "client_secret_basic" {
		style = oauth2.AuthStyleInHeader
	}
	return oauth2.Config{ClientID: grant.ClientID, ClientSecret: grant.ClientSecret, RedirectURL: grant.RedirectURI, Scopes: strings.Fields(grant.Scope), Endpoint: oauth2.Endpoint{AuthURL: grant.Issuer, TokenURL: grant.TokenURL, AuthStyle: style}}
}

// BeginOAuth performs public metadata discovery and DCR, then opens the
// browser. The callback is single-use and never reads a workspace itself.
func (m *Manager) BeginOAuth(ctx context.Context, id string, open func(string) error, complete func(context.Context, string, string, OAuthGrant) error) (Snapshot, error) {
	if !oauthNativeSupported {
		return m.Snapshot(), errors.New("OAuth is unsupported on this platform")
	}
	m.mu.Lock()
	e, ok := m.entry(id)
	if ok && e.Connection != nil && e.Connection.DeviceOAuth != nil {
		m.mu.Unlock()
		return m.beginDeviceOAuth(ctx, id, open, complete)
	}
	_, installed := m.state.Installed[id]
	if !ok || e.Connection == nil || e.Connection.Kind != "oauth" || !installed {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, errors.New("OAuth plugin is not installed")
	}
	if m.oauthFlows[id] != nil {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, errors.New("OAuth authorization already pending")
	}
	rootName := m.state.Installed[id].Root
	m.mu.Unlock()
	p, err := m.readInstalledAt(e, rootName)
	if err != nil {
		return m.Snapshot(), err
	}
	var resource string
	for _, server := range p.Servers {
		if server.Name == e.Connection.Server && server.Type == "streamable-http" {
			resource = server.URL
		}
	}
	if resource == "" {
		return m.Snapshot(), errors.New("OAuth MCP resource unavailable")
	}
	protected, auth, err := discoverOAuth(ctx, m.oauthClient, resource)
	if err != nil {
		return m.Snapshot(), err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return m.Snapshot(), err
	}
	redirect := "http://" + listener.Addr().String() + "/oauth/callback"
	client, err := registerOAuth(ctx, m.oauthClient, auth.RegistrationEndpoint, redirect)
	if err != nil {
		listener.Close()
		return m.Snapshot(), err
	}
	state, err := randomOAuthValue()
	if err != nil {
		listener.Close()
		return m.Snapshot(), err
	}
	verifier := oauth2.GenerateVerifier()
	scope := ""
	if protected.RequiredScope != "" {
		scope = protected.RequiredScope
	} else if len(protected.ScopesSupported) > 0 {
		scope = protected.ScopesSupported[0]
	}
	grant := OAuthGrant{ClientID: client.ClientID, ClientSecret: client.ClientSecret, AuthMethod: client.TokenEndpointAuthMethod, TokenURL: auth.TokenEndpoint, Issuer: auth.Issuer, Resource: resource, RedirectURI: redirect, Scope: scope}
	config := oauthConfig(grant)
	config.Endpoint.AuthURL = auth.AuthorizationEndpoint
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("resource", resource)}
	authorizationURL := config.AuthCodeURL(state, opts...)
	flowCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	flow := &oauthFlow{state: state, cancel: cancel, listener: listener}
	m.mu.Lock()
	if m.oauthFlows == nil {
		m.oauthFlows = map[string]*oauthFlow{}
	}
	if m.oauthErrors == nil {
		m.oauthErrors = map[string]bool{}
	}
	if m.oauthFlows[id] != nil || m.state.Installed[id].Root != rootName {
		m.mu.Unlock()
		cancel()
		listener.Close()
		return m.Snapshot(), errors.New("plugin changed during authorization")
	}
	m.oauthFlows[id] = flow
	delete(m.oauthErrors, id)
	snapshot := m.snapshotLocked()
	m.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Host != listener.Addr().String() || r.URL.Path != "/oauth/callback" || r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid authorization response", http.StatusBadRequest)
			return
		}
		if auth.AuthorizationResponseISSParameterSupported && r.URL.Query().Get("iss") != auth.Issuer {
			http.Error(w, "Invalid issuer", http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		active := m.oauthFlows[id] == flow && !flow.claimed
		if active {
			flow.claimed = true
		}
		m.mu.Unlock()
		if !active {
			http.Error(w, "Authorization no longer active", http.StatusGone)
			return
		}
		defer func() { cancel(); listener.Close() }()
		if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
			m.failOAuth(id)
			http.Error(w, "Authorization was not completed", http.StatusBadRequest)
			return
		}
		exchangeCtx := context.WithValue(flowCtx, oauth2.HTTPClient, oauthHTTP(m.oauthClient))
		token, exchangeErr := config.Exchange(exchangeCtx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier), oauth2.SetAuthURLParam("resource", resource))
		if exchangeErr != nil || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") {
			m.failOAuth(id)
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}
		grant.AccessToken = token.AccessToken
		grant.RefreshToken = token.RefreshToken
		grant.ExpiresAt = token.Expiry
		grant.TokenRevision = 1
		if err := complete(flowCtx, id, state, grant); err != nil {
			m.failOAuth(id)
			http.Error(w, "Connection could not be saved", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "Caelis Bot is connected. You can return to the app.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-flowCtx.Done()
		m.mu.Lock()
		if m.oauthFlows[id] == flow {
			delete(m.oauthFlows, id)
			m.oauthErrors[id] = true
		}
		m.mu.Unlock()
		listener.Close()
	}()
	if open == nil || open(authorizationURL) != nil {
		m.CancelOAuth(id)
		return m.Snapshot(), errors.New("browser could not be opened")
	}
	return snapshot, nil
}
func (m *Manager) failOAuth(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelOAuthLocked(id)
	if m.oauthErrors == nil {
		m.oauthErrors = map[string]bool{}
	}
	m.oauthErrors[id] = true
}
func (m *Manager) cancelOAuthLocked(id string) {
	if flow := m.oauthFlows[id]; flow != nil {
		delete(m.oauthFlows, id)
		if flow.cancel != nil {
			flow.cancel()
		}
		if flow.listener != nil {
			flow.listener.Close()
		}
	}
	delete(m.oauthErrors, id)
}
func (m *Manager) CancelOAuth(id string) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelOAuthLocked(id)
	return m.snapshotLocked()
}

// ConfigureOAuthGrant confirms the private replacement before Runtime reload.
// Synchronous callers still roll back on apply failure. The old grant remains
// available to an existing relay until the replacement is projected.
func (m *Manager) ConfigureOAuthGrant(ctx context.Context, id, state string, grant OAuthGrant, apply func(context.Context, Selection) error) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entry(id)
	if !ok || e.Connection == nil || (e.Connection.Kind != "oauth" && e.Connection.Kind != "oauth-or-token") {
		return m.snapshotLocked(), errors.New("OAuth plugin unavailable")
	}
	if flow := m.oauthFlows[id]; flow == nil || flow.state != state || !flow.claimed {
		return m.snapshotLocked(), errors.New("OAuth authorization no longer active")
	}
	if _, ok := m.state.Installed[id]; !ok {
		return m.snapshotLocked(), errors.New("OAuth plugin is not installed")
	}
	if grant.ClientID == "" || grant.AccessToken == "" || grant.Resource == "" {
		return m.snapshotLocked(), errors.New("invalid OAuth grant")
	}
	old := m.state
	next := cloneState(old)
	next.Revision++
	current := old.Connections[id]
	replacement := connectionRecord{Revision: next.Revision, Configured: true, Mode: "oauth", RetiredOAuth: append([]uint64(nil), current.RetiredOAuth...)}
	if current.Configured {
		replacement.RetiredOAuth = append(replacement.RetiredOAuth, current.Revision)
	}
	next.Connections[id] = replacement
	key := secretKey(m.root, id, next.Revision)
	data, _ := json.Marshal(grant)
	cleanup := func() error { return m.secrets.Delete(key) }
	if err := m.secrets.Save(key, string(data)); err != nil {
		return m.snapshotLocked(), errors.Join(errors.New("private credential store unavailable"), cleanup())
	}
	if err := m.save(next); err != nil {
		return m.snapshotLocked(), errors.Join(err, m.save(old), cleanup())
	}
	m.state = next
	if apply != nil && m.state.Installed[id].Enabled {
		if err := apply(ctx, m.selectionLocked(next)); err != nil {
			restore := m.save(old)
			m.state = old
			return m.snapshotLocked(), errors.Join(err, restore, cleanup())
		}
	}
	if apply != nil {
		// Cleanup is retryable from the persisted retired list. A failed
		// deletion cannot turn an already confirmed grant into an unknown
		// authorization result.
		_ = m.confirmOAuthProjectionLocked(next.Revision)
	}
	m.cancelOAuthLocked(id)
	delete(m.oauthErrors, id)
	return m.snapshotLocked(), nil
}

// ConfirmOAuthProjection retires old grants only after the Runtime has
// accepted the exact connection generation. A newer management revision
// defers cleanup until its own projection completes.
func (m *Manager) ConfirmOAuthProjection(revision uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.confirmOAuthProjectionLocked(revision)
}

func (m *Manager) confirmOAuthProjectionLocked(revision uint64) error {
	if m.state.Revision != revision {
		return nil
	}
	next := cloneState(m.state)
	changed := false
	for id, record := range next.Connections {
		if len(record.RetiredOAuth) == 0 {
			continue
		}
		for _, old := range record.RetiredOAuth {
			if err := m.revokeOAuthKey(id, old); err != nil {
				return err
			}
		}
		record.RetiredOAuth = nil
		next.Connections[id] = record
		changed = true
	}
	if !changed {
		return nil
	}
	if err := m.save(next); err != nil {
		return err
	}
	m.state = next
	return nil
}
