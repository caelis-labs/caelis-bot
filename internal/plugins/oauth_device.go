package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var deviceClientIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{8,128}$`)
var githubAppInstallPattern = regexp.MustCompile(`^https://github\.com/apps/[a-z0-9]+(?:-[a-z0-9]+)*/installations/new$`)

func githubAppInstallURL(value string) bool { return githubAppInstallPattern.MatchString(value) }

func deviceClientID(spec *DeviceOAuthSpec) string {
	if spec == nil {
		return ""
	}
	id := spec.ClientID
	if !deviceClientIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func deviceForm(ctx context.Context, client *http.Client, endpoint string, form url.Values, target any) error {
	if _, err := oauthURL(endpoint); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("invalid OAuth request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := oauthHTTP(client).Do(req)
	if err != nil {
		return errors.New("GitHub authorization service unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("GitHub authorization service rejected the request")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 64<<10+1))
	if err != nil || len(data) > 64<<10 || json.Unmarshal(data, target) != nil {
		return errors.New("invalid GitHub authorization response")
	}
	return nil
}

// beginDeviceOAuth uses a registered public client ID. GitHub's remote MCP
// endpoint does not support DCR; the device grant stays in Bot's private store.
func (m *Manager) beginDeviceOAuth(ctx context.Context, id string, open func(string) error, complete func(context.Context, string, string, OAuthGrant) error) (Snapshot, error) {
	m.mu.Lock()
	e, ok := m.entry(id)
	_, installed := m.state.Installed[id]
	if !ok || e.Connection == nil || e.Connection.DeviceOAuth == nil || !installed {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, errors.New("OAuth plugin is not installed")
	}
	if m.oauthFlows[id] != nil {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, errors.New("OAuth authorization already pending")
	}
	spec := *e.Connection.DeviceOAuth
	clientID := deviceClientID(&spec)
	rootName := m.state.Installed[id].Root
	m.mu.Unlock()
	if clientID == "" {
		return m.Snapshot(), errors.New("GitHub App client ID is not configured")
	}
	p, err := m.readInstalledAt(e, rootName)
	if err != nil {
		return m.Snapshot(), err
	}
	resource := ""
	for _, server := range p.Servers {
		if server.Name == e.Connection.Server && server.Type == "streamable-http" {
			resource = server.URL
		}
	}
	if resource == "" {
		return m.Snapshot(), errors.New("OAuth MCP resource unavailable")
	}
	var device struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		VerifyURL  string `json:"verification_uri"`
		ExpiresIn  int    `json:"expires_in"`
		Interval   int    `json:"interval"`
	}
	form := url.Values{"client_id": {clientID}}
	if spec.Scope != "" {
		form.Set("scope", spec.Scope)
	}
	if err := deviceForm(ctx, m.oauthClient, spec.DeviceCodeURL, form, &device); err != nil {
		return m.Snapshot(), err
	}
	if device.DeviceCode == "" || device.UserCode == "" || device.VerifyURL != spec.VerifyURL || device.ExpiresIn < 1 || device.ExpiresIn > 900 || device.Interval < 0 || device.Interval > 120 {
		return m.Snapshot(), errors.New("invalid GitHub device authorization response")
	}
	state, err := randomOAuthValue()
	if err != nil {
		return m.Snapshot(), err
	}
	flowCtx, cancel := context.WithTimeout(context.Background(), time.Duration(device.ExpiresIn)*time.Second)
	flow := &oauthFlow{state: state, userCode: device.UserCode, cancel: cancel}
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
		return m.Snapshot(), errors.New("plugin changed during authorization")
	}
	m.oauthFlows[id] = flow
	delete(m.oauthErrors, id)
	snapshot := m.snapshotLocked()
	m.mu.Unlock()
	if open == nil || open(spec.VerifyURL) != nil {
		m.CancelOAuth(id)
		return m.Snapshot(), errors.New("browser could not be opened")
	}
	interval := device.Interval
	if interval < 5 {
		interval = 5
	}
	go m.pollDeviceOAuth(flowCtx, id, flow, clientID, resource, spec, device.DeviceCode, time.Duration(interval)*time.Second, complete)
	return snapshot, nil
}

func (m *Manager) pollDeviceOAuth(ctx context.Context, id string, flow *oauthFlow, clientID, resource string, spec DeviceOAuthSpec, deviceCode string, interval time.Duration, complete func(context.Context, string, string, OAuthGrant) error) {
	defer func() {
		m.mu.Lock()
		active := m.oauthFlows[id] == flow
		m.mu.Unlock()
		if active {
			m.failOAuth(id)
		}
	}()
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		var result struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			TokenType    string `json:"token_type"`
			ExpiresIn    int    `json:"expires_in"`
			Error        string `json:"error"`
			Interval     int    `json:"interval"`
		}
		form := url.Values{"client_id": {clientID}, "device_code": {deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}
		if deviceForm(ctx, m.oauthClient, spec.TokenURL, form, &result) != nil {
			return
		}
		switch result.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			if reported := time.Duration(result.Interval) * time.Second; reported > interval {
				interval = reported
			}
			continue
		case "":
		default:
			return
		}
		if result.AccessToken == "" || !strings.EqualFold(result.TokenType, "Bearer") {
			return
		}
		m.mu.Lock()
		active := m.oauthFlows[id] == flow && !flow.claimed
		if active {
			flow.claimed = true
		}
		m.mu.Unlock()
		if !active {
			return
		}
		grant := OAuthGrant{ClientID: clientID, AuthMethod: "device", TokenURL: spec.TokenURL, Issuer: "https://github.com", Resource: resource, Scope: spec.Scope, AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, TokenRevision: 1}
		if result.ExpiresIn > 0 {
			grant.ExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
		}
		if complete == nil || complete(ctx, id, flow.state, grant) != nil {
			return
		}
		return
	}
}
