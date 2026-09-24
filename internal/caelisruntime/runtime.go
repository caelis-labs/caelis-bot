// Package caelisruntime uses the public Caelis CLI for installation and service lifecycle.
// It never embeds the runtime or takes ownership of a shared Host process.
package caelisruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

const InstallerURL = "https://caelis.dev/install.sh"

type Status struct {
	LatestVersion string
	UpdateState   string
	Installed     bool   `json:"installed"`
	Path          string `json:"path"`
	Version       string `json:"version"`
	Message       string `json:"message"`
}

func getLocale(loc ...i18n.Locale) i18n.Locale {
	if len(loc) > 0 && loc[0] != "" {
		return loc[0]
	}
	return i18n.DefaultLocale
}

func text(l i18n.Locale, key string, args ...map[string]any) string {
	var a map[string]any
	if len(args) > 0 {
		a = args[0]
	}
	return i18n.Text(l, key, a)
}

func Find(path string, loc ...i18n.Locale) (string, error) {
	l := getLocale(loc...)
	if path != "" {
		if !filepath.IsAbs(path) {
			return "", errors.New(text(l, "host.enterCaelisFullPath"))
		}
		i, e := os.Stat(path)
		if e != nil || !i.Mode().IsRegular() || i.Mode()&0111 == 0 {
			return "", errors.New(text(l, "host.caelisPathNotExecutable"))
		}
		return filepath.Clean(path), nil
	}
	if p, e := exec.LookPath("caelis"); e == nil {
		return filepath.Abs(p)
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".local/bin/caelis"), "/opt/homebrew/bin/caelis", "/usr/local/bin/caelis"} {
		if f, e := Find(p, l); e == nil {
			return f, nil
		}
	}
	return "", errors.New(text(l, "host.caelisNotFoundInstallOrSelect"))
}

func Store(path string, loc ...i18n.Locale) (string, error) {
	l := getLocale(loc...)
	if path != "" {
		if !filepath.IsAbs(path) {
			return "", errors.New(text(l, "host.caelisDataDirFullPath"))
		}
		return filepath.Clean(path), nil
	}
	h, e := os.UserHomeDir()
	return filepath.Join(h, ".caelis"), e
}

// run bounds output and never returns raw stderr (which can include private configuration).
func run(ctx context.Context, loc i18n.Locale, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	out := limitedBuffer{loc: loc}
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.Env = runtimeenv.Clean(os.Environ())
	if err := cmd.Run(); err != nil {
		return nil, errors.New(text(loc, "host.caelisCommandFailed"))
	}
	return out.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	loc i18n.Locale
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, errors.New(text(b.loc, "host.outputExceededLimit"))
	}
	return b.Buffer.Write(p)
}

func Inspect(ctx context.Context, path string, loc ...i18n.Locale) (Status, error) {
	l := getLocale(loc...)
	p, e := Find(path, l)
	if e != nil {
		return Status{Message: e.Error()}, nil
	}
	ctx, c := context.WithTimeout(ctx, 8*time.Second)
	defer c()
	b, e := run(ctx, l, p, "version", "--format", "json")
	if e != nil {
		return Status{Path: p, Message: e.Error()}, e
	}
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &v) != nil || v.Version == "" {
		return Status{}, errors.New(text(l, "host.fileInvalidCaelisVersion"))
	}
	return Status{Installed: true, Path: p, Version: v.Version, Message: text(l, "host.caelisInstalled")}, nil
}
func Manage(ctx context.Context, action, path, store string, loc ...i18n.Locale) (Status, error) {
	l := getLocale(loc...)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if action == "detect" {
		return Inspect(ctx, path, l)
	}
	if action == "install" {
		if runtime.GOOS != "darwin" {
			return Status{}, errors.New(text(l, "host.platformNoInstaller"))
		}
		if _, e := Find(path, l); e == nil {
			return Status{}, errors.New(text(l, "host.caelisAlreadyInstalled"))
		}
		if path != "" {
			return Status{}, errors.New(text(l, "host.autoInstallDefaultPathOnly"))
		}
		if e := install(ctx, l); e != nil {
			return Status{}, e
		}
		return Inspect(ctx, "", l)
	}
	p, e := Find(path, l)
	if e != nil {
		return Status{}, e
	}
	dir, e := Store(store, l)
	if e != nil {
		return Status{}, e
	}
	switch action {
	case "check-update", "update":
		args := []string{"update", "--store-dir", dir}
		if action == "check-update" {
			args = append(args, "--check")
		}
		b, e := run(ctx, l, p, args...)
		if e != nil {
			return Status{}, e
		}
		v, e := Inspect(ctx, p, l)
		if e != nil {
			return v, e
		}
		return updateResult(v, string(b), action == "check-update", l)
	case "apply-update":
		// The public lifecycle owns selection, exact process identity, readiness,
		// rollback and keeping a newer release. Never stop or kill it ourselves.
		if _, e = run(ctx, l, p, "service", "start", "--store-dir", dir, "--format", "json"); e != nil {
			return Status{}, e
		}
		return Inspect(ctx, p, l)
	case "start":
		raw, e := run(ctx, l, p, "service", "status", "--store-dir", dir, "--format", "json")
		if e != nil {
			return Status{}, e
		}
		var service struct {
			State string `json:"state"`
		}
		if json.Unmarshal(raw, &service) != nil {
			return Status{}, errors.New(text(l, "host.cannotVerifyServiceStatus"))
		}
		if service.State == "running" {
			v, e := Inspect(ctx, p, l)
			v.Message = text(l, "host.caelisServiceAlreadyRunning")
			return v, e
		}
		if service.State != "stopped" {
			return Status{}, errors.New(text(l, "host.serviceStatusUnclearNoNewProcess"))
		}
		_, e = run(ctx, l, p, "service", "start", "--store-dir", dir, "--format", "json")
		if e != nil {
			return Status{}, e
		}
		v, e := Inspect(ctx, p, l)
		v.Message = text(l, "host.caelisServiceStarted")
		return v, e
	default:
		return Status{}, errors.New(text(l, "host.unsupportedRuntimeAction"))
	}
}
func install(ctx context.Context, loc ...i18n.Locale) error {
	l := getLocale(loc...)
	ctx, c := context.WithTimeout(ctx, 5*time.Minute)
	defer c()
	req, e := http.NewRequestWithContext(ctx, "GET", InstallerURL, nil)
	if e != nil {
		return e
	}
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 4 || r.URL.Scheme != "https" || r.URL.Hostname() != "caelis.dev" {
			return errors.New(text(l, "host.insecureInstallRedirect"))
		}
		return nil
	}}
	res, e := client.Do(req)
	if e != nil {
		return errors.New(text(l, "host.cannotDownloadOfficialInstaller"))
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New(text(l, "host.installerDownloadFailed", map[string]any{"code": res.StatusCode}))
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if e != nil || len(b) > 1<<20 {
		return errors.New(text(l, "host.installerContentInvalid"))
	}
	file, e := os.CreateTemp("", "caelis-installer-*.sh")
	if e != nil {
		return e
	}
	defer os.Remove(file.Name())
	_, e = file.Write(b)
	ce := file.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	_, e = run(ctx, l, "/bin/sh", file.Name())
	return e
}

// Parse only the CLI's bounded completion line, never expose installer output.
var availableUpdate = regexp.MustCompile(`^update available: [A-Za-z0-9.+-]+ -> ([A-Za-z0-9.+-]+) \([^\n]+\)$`)

func updateResult(v Status, output string, check bool, loc ...i18n.Locale) (Status, error) {
	l := getLocale(loc...)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if m := availableUpdate.FindStringSubmatch(line); check && len(m) == 2 {
		v.LatestVersion = m[1]
		v.UpdateState = "available"
		v.Message = text(l, "host.updateAvailableToVersion", map[string]any{"version": m[1]})
		return v, nil
	}
	if strings.HasPrefix(line, "caelis is up to date (") && strings.HasSuffix(line, ")") {
		v.LatestVersion = v.Version
		v.UpdateState = "current"
		v.Message = text(l, "host.installedLatestVersion", map[string]any{"version": v.Version})
		return v, nil
	}
	if !check && strings.HasPrefix(line, "Caelis ") && strings.Contains(line, " is installed (") && strings.HasSuffix(line, "it takes effect on the next start.") {
		v.Message = text(l, "host.programUpdatedEnablingService")
		return v, nil
	}
	return v, errors.New(text(l, "host.updateResultUnconfirmed"))
}
