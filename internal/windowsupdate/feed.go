// Package windowsupdate owns signed Windows update discovery and staged bytes.
// Authenticode is a separate distribution policy; unsigned previews still require
// the pinned Ed25519 key before any automatic download can become executable.
package windowsupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/updates"
)

//go:embed public-key.txt
var publicKey string

const Releases = "https://api.github.com/repos/caelis-labs/caelis-bot/releases?per_page=100"

type Artifact struct {
	Format string `json:"format"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Length int64  `json:"length"`
}
type Manifest struct {
	Schema    int        `json:"schema"`
	Version   string     `json:"version"`
	Tag       string     `json:"tag"`
	Channel   string     `json:"channel"`
	Platform  string     `json:"platform"`
	Source    string     `json:"source"`
	Qualified bool       `json:"qualified"`
	Artifacts []Artifact `json:"artifacts"`
}
type Offer struct {
	Manifest  Manifest
	Artifact  Artifact
	Bytes     []byte
	Signature []byte
}

func Key() (ed25519.PublicKey, error) {
	b, e := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKey))
	if e != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("Windows update key is not configured")
	}
	return b, nil
}
func validHash(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == n && strings.ToLower(s) == s
}
func Verify(data, signature []byte, key ed25519.PublicKey) (Manifest, error) {
	var m Manifest
	sig, e := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if e != nil || len(key) != 32 || !ed25519.Verify(key, data, sig) || len(data) > 65536 {
		return m, errors.New("update manifest signature mismatch")
	}
	if json.Unmarshal(data, &m) != nil || m.Schema != 1 || !m.Qualified || m.Platform != "windows-amd64" || m.Tag != "v"+m.Version || !validHash(m.Source, 20) {
		return m, errors.New("unqualified Windows update manifest")
	}
	if _, e = updates.CompareVersions(m.Version, m.Version); e != nil {
		return m, e
	}
	if m.Channel != "preview" && m.Channel != "stable" || m.Channel == "stable" && strings.Contains(m.Version, "-") {
		return m, errors.New("update channel mismatch")
	}
	seen := map[string]bool{}
	for _, a := range m.Artifacts {
		suffix := "-Setup.exe"
		if a.Format == "zip" {
			suffix = ".zip"
		} else if a.Format != "nsis" {
			return m, errors.New("unsupported update format")
		}
		if a.File != "Caelis-Bot-"+m.Version+"-windows-amd64-"+m.Channel+suffix || seen[a.Format] || !validHash(a.SHA256, 32) || a.Length <= 0 || a.Length > 512<<20 {
			return m, errors.New("invalid update artifact")
		}
		seen[a.Format] = true
	}
	if !seen["nsis"] || !seen["zip"] {
		return m, errors.New("both Windows artifacts are required")
	}
	return m, nil
}

type Client struct {
	HTTP      *http.Client
	Feed      string
	PublicKey ed25519.PublicKey
}

func ProductionClient() (*Client, error) {
	k, e := Key()
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" || r.URL.User != nil || len(via) >= 10 {
			return errors.New("unsafe update redirect")
		}
		return nil
	}}, Feed: Releases, PublicKey: k}, e
}
func trustedURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Host == "github.com" && strings.HasPrefix(u.Path, "/caelis-labs/caelis-bot/releases/download/") && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}
func (c *Client) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "Caelis-Bot-Windows-update")
	req.Header.Set("Accept", "application/vnd.github+json")
	r, e := c.HTTP.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("update HTTP %d", r.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("update response too large")
	}
	return b, nil
}
func DownloadURL(m Manifest, file string) string {
	return "https://github.com/caelis-labs/caelis-bot/releases/download/" + m.Tag + "/" + file
}
func (c *Client) Check(ctx context.Context, current, channel, format string) (*Offer, error) {
	data, e := c.get(ctx, c.Feed, 2<<20)
	if e != nil {
		return nil, e
	}
	var releases []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.Unmarshal(data, &releases) != nil {
		return nil, errors.New("invalid release feed")
	}
	var best *Offer
	for _, r := range releases {
		if r.Draft || (channel == "stable" && r.Prerelease) {
			continue
		}
		compare, e := updates.CompareVersions(r.Tag, current)
		if e != nil || compare <= 0 {
			continue
		}
		if best != nil {
			cmp, e := updates.CompareVersions(r.Tag, best.Manifest.Version)
			if e != nil || cmp <= 0 {
				continue
			}
		}
		urls := map[string]string{}
		for _, a := range r.Assets {
			if trustedURL(a.URL) {
				urls[a.Name] = a.URL
			}
		}
		if urls["windows-update.json"] == "" || urls["windows-update.json.sig"] == "" {
			continue
		}
		b, e := c.get(ctx, urls["windows-update.json"], 65536)
		if e != nil {
			return nil, e
		}
		sig, e := c.get(ctx, urls["windows-update.json.sig"], 1024)
		if e != nil {
			return nil, e
		}
		m, e := Verify(b, sig, c.PublicKey)
		if e != nil {
			return nil, e
		}
		if m.Tag != r.Tag || m.Channel != channel {
			continue
		}
		for _, a := range m.Artifacts {
			if a.Format == format && urls[a.File] == DownloadURL(m, a.File) {
				best = &Offer{m, a, b, sig}
			}
		}
	}
	return best, nil
}
func VerifyArchive(path string, a Artifact) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, a.Length+1))
	if e != nil || n != a.Length || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return errors.New("update archive digest mismatch")
	}
	return nil
}
func (c *Client) Download(ctx context.Context, o *Offer, dir string) (string, error) {
	if o == nil {
		return "", errors.New("no update offer")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	path := filepath.Join(dir, o.Artifact.File)
	if VerifyArchive(path, o.Artifact) == nil {
		return path, nil
	}
	req, e := http.NewRequestWithContext(ctx, "GET", DownloadURL(o.Manifest, o.Artifact.File), nil)
	if e != nil {
		return "", e
	}
	req.Header.Set("User-Agent", "Caelis-Bot-Windows-update")
	r, e := c.HTTP.Do(req)
	if e != nil {
		return "", e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return "", fmt.Errorf("archive HTTP %d", r.StatusCode)
	}
	f, e := os.CreateTemp(dir, "download-")
	if e != nil {
		return "", e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	n, e := io.Copy(f, io.LimitReader(r.Body, o.Artifact.Length+1))
	closeErr := f.Close()
	if e != nil || closeErr != nil || n != o.Artifact.Length {
		return "", errors.New("incomplete update download")
	}
	if e = VerifyArchive(tmp, o.Artifact); e != nil {
		return "", e
	}
	if e = os.Rename(tmp, path); e != nil {
		return "", e
	}
	return path, nil
}
