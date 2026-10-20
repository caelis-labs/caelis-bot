package runtimeinstall

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodexRedirectPolicy(t *testing.T) {
	for _, raw := range []string{"https://chatgpt.com/codex/install.sh", "https://releases.openai.com/codex/install.sh", "https://releases.openai.com:443/codex/install.sh"} {
		req, _ := http.NewRequest("GET", raw, nil)
		if err := codexRedirect(req, nil); err != nil {
			t.Errorf("official URL rejected: %v", err)
		}
	}
	for _, raw := range []string{"http://releases.openai.com/codex/install.sh", "https://releases.openai.com.evil.test/install.sh", "https://evil.test/install.sh", "https://user@releases.openai.com/install.sh", "https://releases.openai.com:444/install.sh"} {
		req, _ := http.NewRequest("GET", raw, nil)
		if !errors.Is(codexRedirect(req, nil), errCodexRedirect) {
			t.Errorf("unsafe URL allowed: %s", raw)
		}
	}
	req, _ := http.NewRequest("GET", CodexURL, nil)
	if !errors.Is(codexRedirect(req, make([]*http.Request, 5)), errCodexRedirectLimit) {
		t.Fatal("redirect loop allowed")
	}
}
func TestCodexDownloadErrors(t *testing.T) {
	for _, original := range []error{errCodexRedirect, errCodexRedirectLimit} {
		wrapped := &url.Error{Op: "Get", URL: CodexURL, Err: original}
		if !errors.Is(codexDownloadError(wrapped), original) {
			t.Fatal("redirect reason lost")
		}
	}
	if codexDownloadError(context.Canceled).Error() == codexDownloadError(context.DeadlineExceeded).Error() {
		t.Fatal("cancel and timeout conflated")
	}
}

type codexTransport func(*http.Request) (*http.Response, error)

func (f codexTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCodexOfficialRedirectDownload(t *testing.T) {
	calls := 0
	client := &http.Client{CheckRedirect: codexRedirect, Transport: codexTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() == CodexURL {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://releases.openai.com/codex/install.sh"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		if r.URL.Hostname() != "releases.openai.com" {
			t.Fatalf("unexpected host: %s", r.URL.Hostname())
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("#!/bin/sh\n")), Request: r}, nil
	})}
	response, err := client.Get(CodexURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 2 || response.StatusCode != 200 {
		t.Fatal("official redirect did not complete")
	}
}

func TestManagedInstallerRefusesEscapingDirectory(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("automatic installer supports macOS/Linux")
	}
	home, other := t.TempDir(), t.TempDir()
	if err := os.Symlink(other, filepath.Join(home, ".local")); err != nil {
		t.Fatal(err)
	}
	if managedDirectory(home, filepath.Join(home, ".local", "bin")) == nil {
		t.Fatal("followed redirected directory")
	}
}

func TestInstallerReportsOnlyRealStagesAndFixesDestination(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("automatic installer supports macOS/Linux")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_INSTALL_DIR", "/not-the-server-user")
	t.Setenv("CODEX_INSTALL_DAEMON_ONLY", "1")
	t.Setenv("CODEX_RELEASE", "untrusted-override")
	script := filepath.Join(t.TempDir(), "installer.sh")
	body := `test "$CODEX_INSTALL_DIR" = "$HOME/.local/bin" || exit 2
 test "$CODEX_NON_INTERACTIVE" = 1 || exit 3
 test -z "${CODEX_RELEASE:-}${CODEX_INSTALL_DAEMON_ONLY:-}" || exit 4
 echo '==> Downloading Codex CLI'
 echo 'PRIVATE_AUTH_SECRET'
 echo '==> Installing standalone package to /PRIVATE_PATH'
 `
	if err := os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	var phases []string
	ctx := WithProgress(t.Context(), func(p string) { phases = append(phases, p) })
	if err := runCodexInstaller(ctx, script); err != nil {
		t.Fatal(err)
	}
	if strings.Join(phases, ",") != "downloading,installing,verifying" {
		t.Fatal(phases)
	}
}
