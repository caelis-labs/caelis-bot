package caelisruntime

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

const releaseURL = "https://releases.caelis.dev"
const maxReleaseBytes = 1 << 30

var releaseVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`)
var errReleaseChecksum = errors.New("Caelis release checksum verification failed")

// InstallProgress contains only public release transfer measurements. Speed is
// a rolling five-second rate, not a completion estimate. Total is zero when
// the server has not supplied a trustworthy Content-Length.
type InstallProgress struct {
	Phase          string `json:"phase"`
	Downloaded     int64  `json:"downloaded"`
	Total          int64  `json:"total"`
	BytesPerSecond int64  `json:"bytesPerSecond"`
}

type progressKey struct{}

func WithInstallProgress(ctx context.Context, report func(InstallProgress)) context.Context {
	return context.WithValue(ctx, progressKey{}, report)
}

func ReportInstallProgress(ctx context.Context, progress InstallProgress) {
	if report, ok := ctx.Value(progressKey{}).(func(InstallProgress)); ok && report != nil {
		report(progress)
	}
}

func curlRead(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	file, err := os.CreateTemp("", "caelis-release-metadata-*")
	if err != nil {
		return nil, err
	}
	name := file.Name()
	_ = file.Close()
	defer os.Remove(name)
	cmd := exec.CommandContext(ctx, "/usr/bin/curl", "--fail", "--silent", "--show-error", "--location", "--proto", "=https", "--proto-redir", "=https", "--connect-timeout", "30", "--speed-limit", "1", "--speed-time", "120", "--max-filesize", strconv.FormatInt(limit, 10), "--output", name, rawURL)
	cmd.Env = runtimeenv.Clean(os.Environ())
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		return nil, errors.New("Caelis release metadata could not be downloaded")
	}
	info, err := os.Stat(name)
	if err != nil || info.Size() == 0 || info.Size() > limit {
		return nil, errors.New("Caelis release metadata has an invalid size")
	}
	return os.ReadFile(name)
}

func checksumFor(body []byte, filename string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != filename {
			continue
		}
		if len(fields[0]) != 64 {
			break
		}
		if _, err := hex.DecodeString(fields[0]); err == nil {
			return strings.ToLower(fields[0]), nil
		}
		break
	}
	return "", errReleaseChecksum
}

func releaseLength(ctx context.Context, rawURL string) int64 {
	cmd := exec.CommandContext(ctx, "/usr/bin/curl", "--fail", "--silent", "--show-error", "--location", "--head", "--proto", "=https", "--proto-redir", "=https", "--connect-timeout", "30", "--speed-limit", "1", "--speed-time", "120", rawURL)
	cmd.Env = runtimeenv.Clean(os.Environ())
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil || len(out) > 32<<10 {
		return 0
	}
	var total int64
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.EqualFold(key, "content-length") {
			if n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && n > 0 && n <= maxReleaseBytes {
				total = n
			}
		}
	}
	return total
}

type transferSample struct {
	at   time.Time
	size int64
}

func curlArchive(ctx context.Context, rawURL, path string, total int64) error {
	for attempt := 0; attempt < 3; attempt++ {
		args := []string{"--fail", "--silent", "--show-error", "--location", "--proto", "=https", "--proto-redir", "=https", "--connect-timeout", "30", "--speed-limit", "1", "--speed-time", "120", "--max-filesize", strconv.Itoa(maxReleaseBytes), "--continue-at", "-", "--output", path, rawURL}
		cmd := exec.CommandContext(ctx, "/usr/bin/curl", args...)
		cmd.Env = runtimeenv.Clean(os.Environ())
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		cmd.WaitDelay = 3 * time.Second
		if err := cmd.Start(); err != nil {
			return errors.New("Caelis release download could not start")
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		ticker := time.NewTicker(500 * time.Millisecond)
		samples := make([]transferSample, 0, 12)
		observe := func() {
			info, err := os.Stat(path)
			if err != nil {
				return
			}
			now := time.Now()
			samples = append(samples, transferSample{now, info.Size()})
			for len(samples) > 2 && now.Sub(samples[0].at) > 5*time.Second {
				samples = samples[1:]
			}
			var speed int64
			if len(samples) > 1 {
				oldest := samples[0]
				if seconds := now.Sub(oldest.at).Seconds(); seconds > 0 {
					speed = int64(float64(info.Size()-oldest.size) / seconds)
				}
			}
			ReportInstallProgress(ctx, InstallProgress{Phase: "downloading", Downloaded: info.Size(), Total: total, BytesPerSecond: speed})
		}
		observe()
		var result error
	loop:
		for {
			select {
			case <-ticker.C:
				observe()
			case result = <-done:
				observe()
				break loop
			}
		}
		ticker.Stop()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if result == nil {
			return nil
		}
		if info, err := os.Stat(path); err == nil && info.Size() > maxReleaseBytes {
			return errors.New("Caelis release exceeds the download size limit")
		}
	}
	return errors.New("Caelis release download could not finish after resuming")
}

// stageOfficialRelease downloads and verifies the official archive once, then
// lets the unchanged public install.sh install it from a private file:// mirror.
// That keeps the CLI's shell/PATH behavior in one official script while giving
// Bot a resumable transfer and measured progress.
func stageOfficialRelease(ctx context.Context) (string, func(), error) {
	ReportInstallProgress(ctx, InstallProgress{Phase: "checking"})
	latest, err := curlRead(ctx, releaseURL+"/latest.txt", 128)
	if err != nil {
		return "", nil, err
	}
	version := strings.TrimSpace(string(latest))
	if !releaseVersion.MatchString(version) {
		return "", nil, errors.New("Caelis release version is invalid")
	}
	if runtime.GOOS != "darwin" || (runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64") {
		return "", nil, errors.New("Caelis release is unavailable for this platform")
	}
	name := "caelis_" + strings.TrimPrefix(version, "v") + "_darwin_" + runtime.GOARCH + ".tar.gz"
	base := releaseURL + "/releases/" + version + "/"
	checksums, err := curlRead(ctx, base+"checksums.txt", 1<<20)
	if err != nil {
		return "", nil, err
	}
	want, err := checksumFor(checksums, name)
	if err != nil {
		return "", nil, err
	}
	stage, err := os.MkdirTemp("", "caelis-verified-release-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(stage) }
	dir := filepath.Join(stage, "releases", version)
	if err := os.MkdirAll(dir, 0700); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(stage, "latest.txt"), []byte(version+"\n"), 0600); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), checksums, 0600); err != nil {
		cleanup()
		return "", nil, err
	}
	archive := filepath.Join(dir, name)
	total := releaseLength(ctx, base+name)
	if err := curlArchive(ctx, base+name, archive, total); err != nil {
		cleanup()
		return "", nil, err
	}
	ReportInstallProgress(ctx, InstallProgress{Phase: "verifying", Downloaded: fileSize(archive), Total: total})
	file, err := os.Open(archive)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	hash := sha256.New()
	n, readErr := io.Copy(hash, io.LimitReader(file, maxReleaseBytes+1))
	_ = file.Close()
	if readErr != nil || n == 0 || n > maxReleaseBytes || hex.EncodeToString(hash.Sum(nil)) != want {
		cleanup()
		return "", nil, errReleaseChecksum
	}
	if total > 0 && n != total {
		cleanup()
		return "", nil, errors.New("Caelis release size verification failed")
	}
	return (&url.URL{Scheme: "file", Path: stage}).String(), cleanup, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
