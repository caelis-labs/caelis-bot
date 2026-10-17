package windowsupdate

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func fixtureManifest(t *testing.T) (Manifest, []byte, []byte, ed25519.PublicKey) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	m := Manifest{1, "0.10.0-windows.preview.2", "v0.10.0-windows.preview.2", "preview", "windows-amd64", strings.Repeat("a", 40), true, []Artifact{{"nsis", "Caelis-Bot-0.10.0-windows.preview.2-windows-amd64-preview-Setup.exe", strings.Repeat("a", 64), 10}, {"zip", "Caelis-Bot-0.10.0-windows.preview.2-windows-amd64-preview.zip", strings.Repeat("b", 64), 20}}}
	b, _ := json.Marshal(m)
	s := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b)))
	return m, b, s, pub
}
func TestSignedWindowsManifestRejectsTamperingAndUnqualifiedArtifacts(t *testing.T) {
	m, b, s, key := fixtureManifest(t)
	if _, e := Verify(b, s, key); e != nil {
		t.Fatal(e)
	}
	b[10] ^= 1
	if _, e := Verify(b, s, key); e == nil {
		t.Fatal("tampered metadata accepted")
	}
	for _, mutate := range []func(*Manifest){func(m *Manifest) { m.Qualified = false }, func(m *Manifest) { m.Platform = "windows-arm64" }, func(m *Manifest) { m.Channel = "stable" }, func(m *Manifest) { m.Artifacts[0].File = "../evil.exe" }, func(m *Manifest) { m.Artifacts = m.Artifacts[:1] }} {
		pub, priv, _ := ed25519.GenerateKey(nil)
		fixture := m
		fixture.Artifacts = append([]Artifact(nil), m.Artifacts...)
		mutate(&fixture)
		bytes, _ := json.Marshal(fixture)
		signature := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, bytes)))
		if _, e := Verify(bytes, signature, pub); e == nil {
			t.Fatal("invalid signed manifest accepted")
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestWindowsFeedSelectsOnlyPinnedSignedChannelAndFormat(t *testing.T) {
	m, b, s, pub := fixtureManifest(t)
	base := DownloadURL(m, "")
	assets := []map[string]string{{"name": "windows-update.json", "browser_download_url": base + "windows-update.json"}, {"name": "windows-update.json.sig", "browser_download_url": base + "windows-update.json.sig"}, {"name": m.Artifacts[1].File, "browser_download_url": base + m.Artifacts[1].File}}
	feed, _ := json.Marshal([]any{map[string]any{"tag_name": m.Tag, "prerelease": true, "assets": assets}})
	c := &Client{Feed: Releases, PublicKey: pub, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body := feed
		switch r.URL.String() {
		case base + "windows-update.json":
			body = b
		case base + "windows-update.json.sig":
			body = s
		case Releases:
		default:
			t.Fatalf("unexpected URL %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}}
	offer, e := c.Check(context.Background(), "0.10.0-windows.preview.1", "preview", "zip")
	if e != nil || offer == nil || offer.Artifact.Format != "zip" {
		t.Fatal("valid ZIP update absent", e)
	}
	for _, v := range []struct{ current, channel, format string }{{m.Version, "preview", "zip"}, {"0.10.0-windows.preview.1", "stable", "zip"}, {"0.10.0-windows.preview.1", "preview", "nsis"}} {
		offer, e = c.Check(context.Background(), v.current, v.channel, v.format)
		if e != nil || offer != nil {
			t.Fatal("downgrade/channel/absent format offered", e)
		}
	}
	for _, url := range []string{"http://github.com/caelis-labs/caelis-bot/releases/download/v1/file", "https://evil.test/caelis-labs/caelis-bot/releases/download/v1/file", "https://github.com/another/repo/releases/download/v1/file", "https://github.com/caelis-labs/caelis-bot/releases/download/v1/file?token=secret"} {
		if trustedURL(url) {
			t.Fatal("untrusted release URL accepted")
		}
	}
}
