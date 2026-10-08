package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// oauthBearer takes a cross-process lock before reading the native item. A
// rejected token is refreshed only for the next distinct MCP request; the
// original JSON-RPC ID is never replayed.
func (m *Manager) oauthBearer(ctx context.Context, id string, revision uint64, rejected string) (string, error) {
	unlock, err := lockOAuth(m.root, id)
	if err != nil {
		return "", err
	}
	defer unlock()
	key := secretKey(m.root, id, revision)
	raw, err := m.secrets.Load(key)
	if err != nil {
		return "", errors.New("OAuth connection unavailable")
	}
	var grant OAuthGrant
	if json.Unmarshal([]byte(raw), &grant) != nil || grant.ClientID == "" || grant.Resource == "" {
		return "", errors.New("invalid OAuth grant")
	}
	if grant.AccessToken == "" {
		return "", errors.New("OAuth authorization required")
	}
	expired := !grant.ExpiresAt.IsZero() && time.Until(grant.ExpiresAt) < 30*time.Second
	if !expired && (rejected == "" || rejected != grant.AccessToken) {
		return grant.AccessToken, nil
	}
	if grant.RefreshToken == "" {
		return "", errors.New("OAuth authorization required")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {grant.RefreshToken}, "client_id": {grant.ClientID}, "resource": {grant.Resource}}
	if grant.AuthMethod == "client_secret_post" {
		form.Set("client_secret", grant.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, grant.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", errors.New("invalid OAuth token endpoint")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if grant.AuthMethod == "client_secret_basic" {
		request.SetBasicAuth(grant.ClientID, grant.ClientSecret)
	}
	response, err := oauthHTTP(m.oauthClient).Do(request)
	if err != nil {
		return "", errors.New("OAuth refresh failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10+1))
	if err != nil || len(data) > 64<<10 {
		return "", errors.New("invalid OAuth refresh response")
	}
	var token struct {
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		TokenType    string      `json:"token_type"`
		ExpiresIn    json.Number `json:"expires_in"`
		Error        string      `json:"error"`
	}
	if json.Unmarshal(data, &token) != nil {
		return "", errors.New("invalid OAuth refresh response")
	}
	if response.StatusCode != http.StatusOK || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") {
		if token.Error == "invalid_grant" {
			grant.AccessToken = ""
			grant.RefreshToken = ""
			grant.TokenRevision++
			replacement, _ := json.Marshal(grant)
			if err := m.secrets.Save(key, string(replacement)); err != nil {
				return "", errors.New("OAuth grant invalid; private store update failed")
			}
			return "", errors.New("OAuth authorization required")
		}
		return "", fmt.Errorf("OAuth refresh returned HTTP %d", response.StatusCode)
	}
	grant.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		grant.RefreshToken = token.RefreshToken
	}
	if seconds, err := token.ExpiresIn.Int64(); err == nil && seconds > 0 {
		grant.ExpiresAt = time.Now().Add(time.Duration(seconds) * time.Second)
	} else {
		grant.ExpiresAt = time.Time{}
	}
	grant.TokenRevision++
	replacement, _ := json.Marshal(grant)
	if err := m.secrets.Save(key, string(replacement)); err != nil {
		return "", errors.New("OAuth refresh could not be saved")
	}
	return grant.AccessToken, nil
}

func (m *Manager) revokeOAuthKey(id string, revision uint64) error {
	unlock, err := lockOAuth(m.root, id)
	if err != nil {
		return err
	}
	defer unlock()
	return m.secrets.Delete(secretKey(m.root, id, revision))
}
