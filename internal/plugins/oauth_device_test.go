package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/secretstore"
)

func TestGitHubDeviceOAuthConnectCancelReplaceAndDisconnect(t *testing.T) {
	root := t.TempDir()
	m, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	var secretMu sync.Mutex
	secrets := map[string]string{}
	m.secrets = secretstore.Functions{
		SaveFunc: func(k, v string) error { secretMu.Lock(); defer secretMu.Unlock(); secrets[k] = v; return nil },
		LoadFunc: func(k string) (string, error) {
			secretMu.Lock()
			defer secretMu.Unlock()
			v, ok := secrets[k]
			if !ok {
				return "", errors.New("missing")
			}
			return v, nil
		},
		DeleteFunc: func(k string) error { secretMu.Lock(); defer secretMu.Unlock(); delete(secrets, k); return nil },
	}
	if _, err := m.Mutate(t.Context(), "github", "install", nil); err != nil {
		t.Fatal(err)
	}
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device":
			if got := r.FormValue("scope"); got != "" {
				t.Errorf("GitHub App device authorization requested OAuth App scope %q", got)
			}
			_, _ = w.Write([]byte(`{"device_code":"FIXTURE_DEVICE_SECRET","user_code":"ABCD-EFGH","verification_uri":"` + serverURL + `/verify","expires_in":60,"interval":1}`))
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"FIXTURE_ACCESS_SECRET","token_type":"bearer"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	serverURL = server.URL
	defer server.Close()
	for i := range m.catalog {
		if m.catalog[i].ID == "github" {
			spec := *m.catalog[i].Connection
			device := *spec.DeviceOAuth
			device.ClientID, device.DeviceCodeURL, device.TokenURL, device.VerifyURL = "fixture123", server.URL+"/device", server.URL+"/token", server.URL+"/verify"
			spec.DeviceOAuth = &device
			m.catalog[i].Connection = &spec
		}
	}
	opened := ""
	complete := func(ctx context.Context, id, state string, grant OAuthGrant) error {
		_, err := m.ConfigureOAuthGrant(ctx, id, state, grant, nil)
		return err
	}
	snapshot, err := m.BeginOAuth(t.Context(), "github", func(u string) error { opened = u; return nil }, complete)
	if err != nil {
		t.Fatal(err)
	}
	if opened != server.URL+"/verify" || snapshot.Items == nil {
		t.Fatal("device authorization did not open reviewed verification URL")
	}
	var pending ConnectionView
	for _, item := range snapshot.Items {
		if item.ID == "github" {
			pending = *item.Connection
		}
	}
	if pending.State != "pending" || pending.UserCode != "ABCD-EFGH" || strings.Contains(mustJSON(t, snapshot), "FIXTURE_DEVICE_SECRET") {
		t.Fatal("pending snapshot exposed wrong state or private device code")
	}
	m.CancelOAuth("github")
	if state := connectionState(m.Snapshot(), "github"); state != "not_configured" {
		t.Fatalf("cancel state = %s", state)
	}
	if _, err := m.BeginOAuth(t.Context(), "github", func(string) error { return nil }, complete); err != nil {
		t.Fatal("retry after cancel:", err)
	}
	deadline := time.After(8 * time.Second)
	for connectionState(m.Snapshot(), "github") != "configured" {
		select {
		case <-deadline:
			t.Fatal("device authorization did not complete")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if strings.Contains(mustJSON(t, m.Snapshot()), "FIXTURE_ACCESS_SECRET") {
		t.Fatal("snapshot exposed access token")
	}
	revision := m.state.Connections["github"].Revision
	if m.state.Connections["github"].Mode != "oauth" {
		t.Fatal("OAuth connection mode not saved")
	}
	secretMu.Lock()
	raw := secrets[secretKey(root, "github", revision)]
	secretMu.Unlock()
	var grant OAuthGrant
	if json.Unmarshal([]byte(raw), &grant) != nil || grant.AccessToken != "FIXTURE_ACCESS_SECRET" || grant.Scope != "" {
		t.Fatal("OAuth grant not saved privately")
	}
	if _, err := m.ConfigureConnection(t.Context(), "github", "FIXTURE_TOKEN_REPLACEMENT", "", false, nil); err != nil {
		t.Fatal(err)
	}
	secretMu.Lock()
	_, retained := secrets[secretKey(root, "github", revision)]
	secretMu.Unlock()
	if !retained {
		t.Fatal("old OAuth generation revoked before Runtime projection")
	}
	if _, err := m.ConfigureConnection(t.Context(), "github", "FIXTURE_TOKEN_REPLACEMENT_2", "", false, nil); err != nil {
		t.Fatal(err)
	}
	secretMu.Lock()
	_, retained = secrets[secretKey(root, "github", revision)]
	secretMu.Unlock()
	if !retained {
		t.Fatal("old OAuth generation lost across a second token replacement")
	}
	if err := m.ConfirmOAuthProjection(m.state.Revision); err != nil {
		t.Fatal(err)
	}
	secretMu.Lock()
	_, retained = secrets[secretKey(root, "github", revision)]
	secretMu.Unlock()
	if retained {
		t.Fatal("old OAuth generation retained after Runtime projection")
	}
	if _, err := m.ConfigureConnection(t.Context(), "github", "", "", true, nil); err != nil {
		t.Fatal(err)
	}
	secretMu.Lock()
	remaining := len(secrets)
	secretMu.Unlock()
	if remaining != 0 || connectionState(m.Snapshot(), "github") != "not_configured" {
		t.Fatalf("disconnect retained credentials or state: %d", remaining)
	}
}

func connectionState(snapshot Snapshot, id string) string {
	for _, item := range snapshot.Items {
		if item.ID == id && item.Connection != nil {
			return item.Connection.State
		}
	}
	return "missing"
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
