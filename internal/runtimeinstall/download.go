package runtimeinstall

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

const installerLimit = 1 << 20

// DownloadOfficialScript tries the native HTTPS stack first. The system curl
// fallback can use the user's existing proxy and certificate setup when the
// Finder-launched process has no equivalent Go HTTP configuration. Both paths
// keep output bounded and never display a script or proxy credential.
func DownloadOfficialScript(ctx context.Context, rawURL, host string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != host {
		return nil, errors.New("invalid official installer URL")
	}
	request, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 120 * time.Second, TLSHandshakeTimeout: 30 * time.Second}, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 4 || r.URL.Scheme != "https" || r.URL.Hostname() != host {
			return errors.New("unsupported installer redirect")
		}
		return nil
	}}
	if response, err := client.Do(request); err == nil {
		defer response.Body.Close()
		if response.StatusCode == http.StatusOK {
			// Reset only on received bytes; there is no total download deadline.
			idle := time.AfterFunc(120*time.Second, func() { _ = response.Body.Close() })
			defer idle.Stop()
			reader := io.TeeReader(response.Body, writerFunc(func(p []byte) (int, error) { idle.Reset(120 * time.Second); return len(p), nil }))
			body, readErr := io.ReadAll(io.LimitReader(reader, installerLimit+1))
			if readErr == nil && len(body) > 0 && len(body) <= installerLimit {
				return body, nil
			}
		}
	}
	file, err := os.CreateTemp("", "official-installer-*.sh")
	if err != nil {
		return nil, errors.New("cannot prepare installer download")
	}
	name := file.Name()
	_ = file.Close()
	defer os.Remove(name)
	curl := exec.CommandContext(ctx, "/usr/bin/curl", "--fail", "--silent", "--show-error", "--location", "--proto", "=https", "--proto-redir", "=https", "--max-redirs", "4", "--connect-timeout", "30", "--speed-limit", "1", "--speed-time", "120", "--max-filesize", "1048576", "--output", name, rawURL)
	curl.Env = runtimeenv.Clean(os.Environ())
	curl.Stdout, curl.Stderr = io.Discard, io.Discard
	if err := curl.Run(); err != nil {
		return nil, errors.New("official installer download failed")
	}
	body, err := os.ReadFile(name)
	if err != nil || len(body) == 0 || len(body) > installerLimit {
		return nil, errors.New("official installer content invalid")
	}
	return body, nil
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
