// Package caelisruntime uses the public Caelis CLI for installation and service lifecycle.
// It never embeds the runtime or takes ownership of a shared Host process.
package caelisruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"github.com/caelis-labs/caelis-bot/internal/runtimeinstall"
	"github.com/caelis-labs/caelis-bot/internal/updates"
)

const InstallerURL = "https://caelis.dev/install.sh"

type Status struct {
	LatestVersion string
	UpdateState   string
	Source        string `json:"source"`
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
	status := Status{Installed: true, Path: p, Version: v.Version, Message: text(l, "host.caelisInstalled")}
	status.Source = runtimeinstall.Detect(ctx, p, "caelis", "@caelis/caelis").Kind
	if status.Source == "unknown" {
		home, _ := os.UserHomeDir()
		standard := filepath.Join(home, ".local", "bin", "caelis")
		if filepath.Clean(p) == standard {
			if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink == 0 {
				status.Source = "official"
			}
		}
	}
	return status, nil
}
func Manage(ctx context.Context, action, path, store string, loc ...i18n.Locale) (Status, error) {
	l := getLocale(loc...)
	if action != "install" && action != "update" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
	}
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
		installed, err := Inspect(ctx, "", l)
		if err != nil || !installed.Installed {
			return installed, errors.New(text(l, "host.caelisCommandFailed"))
		}
		return installed, nil
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
		before, err := Inspect(ctx, p, l)
		if err != nil {
			return before, err
		}
		if action == "update" && (before.Source == "brew-formula" || before.Source == "brew-cask") {
			source := runtimeinstall.Detect(ctx, p, "caelis", "@caelis/caelis")
			if source.Kind != before.Source {
				return before, errors.New("Caelis 安装来源已变化，请重新检测")
			}
			if err := runtimeinstall.Upgrade(ctx, source); err != nil {
				return before, err
			}
			after, err := Inspect(ctx, source.Entry, l)
			if err != nil || !after.Installed || after.Source != before.Source {
				return after, errors.New("更新后的 Caelis 程序未能验证，请重新检测原安装渠道")
			}
			if order, err := updates.CompareVersions(after.Version, before.Version); err != nil || order < 0 {
				return after, errors.New("更新后的 Caelis 版本低于原版本，请核对安装渠道")
			}
			if after.Version == before.Version {
				after.UpdateState = "current"
				after.Message = "原安装渠道当前没有更新版本"
			}
			return after, nil
		}
		if action == "update" && before.Source == "unknown" {
			return before, errors.New("无法确认 Caelis 安装来源；请核对原安装渠道或选择现有程序")
		}
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
		if action == "update" && v.Source != before.Source {
			return v, errors.New("Caelis 更新后安装来源发生变化，请重新检测")
		}
		if action == "update" {
			if order, err := updates.CompareVersions(v.Version, before.Version); err != nil || order < 0 {
				return v, errors.New("Caelis 更新后的版本未能验证，请重新检测")
			}
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
	ReportInstallProgress(ctx, InstallProgress{Phase: "checking"})
	b, e := runtimeinstall.DownloadOfficialScript(ctx, InstallerURL, "caelis.dev")
	if e != nil {
		return errors.New(text(l, "host.cannotDownloadOfficialInstaller"))
	}
	localReleaseURL, cleanup, e := stageOfficialRelease(ctx)
	if e != nil {
		if errors.Is(e, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New(text(l, "host.caelisDownloadTimedOut"))
		}
		if errors.Is(e, errReleaseChecksum) {
			return errors.New(text(l, "host.caelisChecksumFailed"))
		}
		return errors.New(text(l, "host.caelisDownloadFailed"))
	}
	defer cleanup()
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
	ReportInstallProgress(ctx, InstallProgress{Phase: "installing"})
	e = runInstaller(ctx, l, file.Name(), localReleaseURL)
	if e != nil {
		return errors.New(text(l, "host.caelisInstallFailed"))
	}
	return nil
}

// Parse only the CLI's bounded completion line, never expose installer output.
var availableUpdate = regexp.MustCompile(`^update available: [A-Za-z0-9.+-]+ -> ([A-Za-z0-9.+-]+) \([^\n]+\)$`)
var completedUpdate = regexp.MustCompile(`^Caelis ([A-Za-z0-9.+-]+) is installed \(updated from [A-Za-z0-9.+-]+ via (?:raw|npm)\); it takes effect on the next start\.$`)

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
	if match := completedUpdate.FindStringSubmatch(line); !check && len(match) == 2 {
		if order, err := updates.CompareVersions(match[1], v.Version); err != nil || order != 0 {
			return v, errors.New(text(l, "host.updateResultUnconfirmed"))
		}
		v.Message = text(l, "host.programUpdatedEnablingService")
		return v, nil
	}
	return v, errors.New(text(l, "host.updateResultUnconfirmed"))
}
