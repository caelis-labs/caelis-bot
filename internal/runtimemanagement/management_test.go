package runtimemanagement

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type archiveEntry struct {
	name, body string
	kind       byte
	mode       int64
}

func fixtureArchive(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0700
		}
		header := &tar.Header{Name: entry.name, Typeflag: kind, Mode: mode, Size: int64(len(entry.body))}
		if kind == tar.TypeSymlink {
			header.Linkname = "../../outside"
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := writer.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func fixtureRelease(t *testing.T, provider, version string, entries ...archiveEntry) (Release, []byte) {
	t.Helper()
	binary := "caelis"
	script := "#!/bin/sh\nprintf '%s\\n' '{\"version\":\"" + version + "\"}'\n"
	if provider == "codex" {
		binary = "bin/codex"
		script = "#!/bin/sh\nprintf '%s\\n' 'codex-cli " + version + "'\n"
	}
	if len(entries) == 0 {
		entries = []archiveEntry{{name: binary, body: script}}
	}
	if provider == "codex" {
		entries = append(entries, archiveEntry{name: "bin/codex-code-mode-host", body: "#!/bin/sh\nexit 0\n"}, archiveEntry{name: "codex-path/rg", body: "#!/bin/sh\nexit 0\n"})
	}
	archive := fixtureArchive(t, entries...)
	hash := sha256.Sum256(archive)
	return Release{Runtime: provider, Version: version, Arch: "amd64", URL: "https://releases.caelis.dev/fixture.tar.gz", SHA256: hex.EncodeToString(hash[:]), Size: int64(len(archive)), Binary: binary}, archive
}
func fixtureManager(t *testing.T) (*Manager, map[string][]byte) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "managed")
	var releases []Release
	archives := map[string][]byte{}
	for _, provider := range []string{"codex", "caelis"} {
		for _, version := range []string{"1.0.0", "2.0.0"} {
			release, archive := fixtureRelease(t, provider, version)
			releases = append(releases, release)
			archives[provider+version] = archive
		}
	}
	manager := newManager(root, "amd64", releases)
	manager.fetch = func(_ context.Context, r Release) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(archives[r.Runtime+r.Version])), nil
	}
	return manager, archives
}
func install(t *testing.T, m *Manager, provider, version, id string) Status {
	t.Helper()
	status, err := m.Manage(context.Background(), Request{Action: "install", Runtime: provider, Version: version, RequestID: id})
	if err != nil || status.Outcome != "accepted" || status.Version != version {
		t.Fatalf("install: %+v %v", status, err)
	}
	return status
}

func TestInstallUpdateAtomicSelectionPreservesPriorAndUnrelatedExecutable(t *testing.T) {
	for _, provider := range []string{"codex", "caelis"} {
		t.Run(provider, func(t *testing.T) {
			manager, _ := fixtureManager(t)
			unrelated := filepath.Join(filepath.Dir(manager.directory), "existing-runtime")
			if err := os.WriteFile(unrelated, []byte("existing user installation"), 0700); err != nil {
				t.Fatal(err)
			}
			status, err := manager.Manage(context.Background(), Request{Action: "detect", Runtime: provider})
			if err != nil || status.Installed {
				t.Fatal(status, err)
			}
			if _, err := os.Stat(manager.directory); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("read-only detect created state")
			}
			install(t, manager, provider, "1.0.0", "install-1")
			oldPath, err := manager.BinaryPath(provider)
			if err != nil {
				t.Fatal(err)
			}
			oldBytes, err := os.ReadFile(oldPath)
			if err != nil {
				t.Fatal(err)
			}
			status, err = manager.Manage(context.Background(), Request{Action: "check-update", Runtime: provider})
			if err != nil || status.UpdateState != "available" || status.LatestVersion != "2.0.0" {
				t.Fatal(status, err)
			}
			status, err = manager.Manage(context.Background(), Request{Action: "update", Runtime: provider, Version: "2.0.0", ExpectedVersion: "1.0.0", RequestID: "update-2"})
			if err != nil || status.Version != "2.0.0" || status.Outcome != "accepted" {
				t.Fatal(status, err)
			}
			if data, err := os.ReadFile(oldPath); err != nil || !bytes.Equal(data, oldBytes) {
				t.Fatal("update changed prior release")
			}
			if data, err := os.ReadFile(unrelated); err != nil || string(data) != "existing user installation" {
				t.Fatal("update touched unrelated installation")
			}
			for _, name := range []string{".owner", filepath.Join(provider, "current.json")} {
				info, err := os.Stat(filepath.Join(manager.directory, name))
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("private state permissions", err)
				}
			}
			matches, _ := filepath.Glob(filepath.Join(manager.directory, provider, ".stage-*"))
			if len(matches) != 0 {
				t.Fatal("staging artifacts leaked")
			}
		})
	}
}

func TestChecksumArchivePathAndVersionFailuresPreserveSelection(t *testing.T) {
	for _, failure := range []string{"checksum", "traversal", "symlink", "duplicate", "version", "download", "publish"} {
		t.Run(failure, func(t *testing.T) {
			manager, archives := fixtureManager(t)
			install(t, manager, "caelis", "1.0.0", "old")
			original, err := os.ReadFile(filepath.Join(manager.directory, "caelis/current.json"))
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "checksum":
				damaged := append([]byte(nil), archives["caelis2.0.0"]...)
				damaged[len(damaged)-1] ^= 1
				archives["caelis2.0.0"] = damaged
			case "traversal", "symlink", "duplicate":
				entries := []archiveEntry{{name: "caelis", body: "#!/bin/sh\nexit 0\n"}}
				switch failure {
				case "traversal":
					entries = append(entries, archiveEntry{name: "../outside", body: "escape"})
				case "symlink":
					entries = append(entries, archiveEntry{name: "link", kind: tar.TypeSymlink})
				case "duplicate":
					entries = append(entries, entries[0])
				}
				release, archive := fixtureRelease(t, "caelis", "2.0.0", entries...)
				manager.releases[3] = release
				archives["caelis2.0.0"] = archive
			case "version":
				manager.verify = func(context.Context, string, Release) error {
					return errors.New("untrusted private stderr must not escape")
				}
			case "download":
				manager.fetch = func(context.Context, Release) (io.ReadCloser, error) { return nil, context.Canceled }
			case "publish":
				manager.write = func(root *os.Root, name string, value any) (bool, error) {
					if name == "caelis/current.json" {
						return false, errors.New("disk failure")
					}
					return writeAtomic(root, name, value)
				}
			}
			status, err := manager.Manage(context.Background(), Request{Action: "update", Runtime: "caelis", Version: "2.0.0", ExpectedVersion: "1.0.0", RequestID: "failed-update"})
			if err == nil || status.Outcome != "rejected" {
				t.Fatalf("unsafe update accepted: %+v %v", status, err)
			}
			if failure == "checksum" && status.Message != "release checksum mismatch" {
				t.Fatal("checksum was not checked", status.Message)
			}
			if strings.Contains(status.Message, "private stderr") {
				t.Fatal("private verifier output escaped")
			}
			after, err := os.ReadFile(filepath.Join(manager.directory, "caelis/current.json"))
			if err != nil || !bytes.Equal(after, original) {
				t.Fatal("failed update changed active selection")
			}
			matches, _ := filepath.Glob(filepath.Join(manager.directory, "caelis/.stage-*"))
			if len(matches) != 0 {
				t.Fatal("failed staging leaked")
			}
			if _, err := os.Stat(filepath.Join(manager.directory, "outside")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("archive escaped staging")
			}
		})
	}
}

func TestExpectedVersionAndReviewedCatalogFenceMutations(t *testing.T) {
	manager, _ := fixtureManager(t)
	install(t, manager, "codex", "2.0.0", "installed")
	for _, request := range []Request{
		{Action: "update", Runtime: "codex", Version: "1.0.0", ExpectedVersion: "2.0.0", RequestID: "downgrade"},
		{Action: "update", Runtime: "codex", Version: "2.0.0", ExpectedVersion: "1.0.0", RequestID: "stale"},
		{Action: "update", Runtime: "codex", Version: "latest", ExpectedVersion: "2.0.0", RequestID: "unpinned"},
		{Action: "update", Runtime: "codex", Version: "2.0.0", ExpectedVersion: "2.0.0"},
		{Action: "install", Runtime: "codex", Version: "2.0.0", RequestID: "replace"},
		{Action: "start", Runtime: "codex"},
	} {
		if status, err := manager.Manage(context.Background(), request); err == nil || status.Outcome != "rejected" {
			t.Fatalf("invalid action accepted: %+v %v", status, err)
		}
	}
}

func TestOriginalRequestReplayAndDigestMismatch(t *testing.T) {
	manager, _ := fixtureManager(t)
	var fetches atomic.Int32
	fetch := manager.fetch
	manager.fetch = func(ctx context.Context, r Release) (io.ReadCloser, error) { fetches.Add(1); return fetch(ctx, r) }
	request := Request{Action: "install", Runtime: "caelis", Version: "1.0.0", RequestID: "stable"}
	first, err := manager.Manage(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := manager.Manage(context.Background(), request)
	if err != nil || replay != first || fetches.Load() != 1 {
		t.Fatal("replay repeated installation", replay, err)
	}
	request.Version = "2.0.0"
	if status, err := manager.Manage(context.Background(), request); err == nil || status.Outcome != "rejected" {
		t.Fatal("changed request reused receipt")
	}
}

func TestPublicationUncertaintyReconcilesOnlyOriginalRequest(t *testing.T) {
	manager, _ := fixtureManager(t)
	request := Request{Action: "install", Runtime: "caelis", Version: "1.0.0", RequestID: "lost-response"}
	manager.write = func(root *os.Root, name string, value any) (bool, error) {
		published, err := writeAtomic(root, name, value)
		if name == "caelis/current.json" && err == nil {
			return published, errors.New("directory sync acknowledgement lost")
		}
		return published, err
	}
	status, err := manager.Manage(context.Background(), request)
	if err == nil || status.Outcome != "unknown" {
		t.Fatal("publication ambiguity lost", status, err)
	}
	manager.write = writeAtomic
	manager.fetch = func(context.Context, Release) (io.ReadCloser, error) {
		t.Fatal("reconciliation redispatched installation")
		return nil, nil
	}
	request.Action = "resolve"
	status, err = manager.Manage(context.Background(), request)
	if err != nil || status.Outcome != "accepted" || status.Version != "1.0.0" {
		t.Fatal("original effect was not reconciled", status, err)
	}
	request.Version = "2.0.0"
	if status, err := manager.Manage(context.Background(), request); err == nil || status.Outcome != "rejected" {
		t.Fatal("different input reconciled original receipt")
	}
}

func TestUnresolvedIntentDoesNotRedispatch(t *testing.T) {
	manager, _ := fixtureManager(t)
	root, err := manager.openRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Action: "install", Runtime: "caelis", Version: "1.0.0", RequestID: "interrupted"}
	_, err = writeAtomic(root, receiptName(request.Runtime, request.RequestID), receipt{Schema: 1, Request: request, Digest: digestRequest(request), Phase: "intent"})
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	manager.fetch = func(context.Context, Release) (io.ReadCloser, error) {
		t.Fatal("unknown operation redispatched")
		return nil, nil
	}
	if status, err := manager.Manage(context.Background(), request); err == nil || status.Outcome != "unknown" {
		t.Fatal(status, err)
	}
}

func TestPrivateRootAndExecutableTamperingRejected(t *testing.T) {
	manager, _ := fixtureManager(t)
	if err := os.Mkdir(manager.directory, 0755); err != nil {
		t.Fatal(err)
	}
	if status, err := manager.Manage(context.Background(), Request{Action: "install", Runtime: "caelis", Version: "1.0.0", RequestID: "unsafe-root"}); err == nil || status.Outcome != "rejected" {
		t.Fatal("public unmanaged directory accepted")
	}
	manager, _ = fixtureManager(t)
	install(t, manager, "caelis", "1.0.0", "initial")
	executable, err := manager.BinaryPath("caelis")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("changed"), 0700); err != nil {
		t.Fatal(err)
	}
	if status, err := manager.Manage(context.Background(), Request{Action: "detect", Runtime: "caelis"}); err == nil || status.Outcome != "rejected" {
		t.Fatal("tampered binary reported installed")
	}
}

func TestConcurrentManagersSerializeInstallation(t *testing.T) {
	first, archives := fixtureManager(t)
	second := newManager(first.directory, first.arch, first.releases)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	first.fetch = func(_ context.Context, r Release) (io.ReadCloser, error) {
		once.Do(func() { close(entered) })
		<-release
		return io.NopCloser(bytes.NewReader(archives[r.Runtime+r.Version])), nil
	}
	result := make(chan error, 1)
	go func() {
		_, err := first.Manage(context.Background(), Request{Action: "install", Runtime: "caelis", Version: "1.0.0", RequestID: "first"})
		result <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	second.fetch = func(context.Context, Release) (io.ReadCloser, error) {
		t.Error("concurrent manager bypassed native lock")
		return nil, errors.New("unexpected")
	}
	status, err := second.Manage(ctx, Request{Action: "install", Runtime: "caelis", Version: "2.0.0", RequestID: "second"})
	close(release)
	if installErr := <-result; installErr != nil {
		t.Fatal(installErr)
	}
	if err == nil || status.Outcome != "rejected" {
		t.Fatal("concurrent request was not fenced")
	}
}

func TestStrictNativeCommandRejectsSecretAndConfigurationFields(t *testing.T) {
	for _, input := range []string{
		`{"action":"install","runtime":"caelis","apiKey":"private-key"}`,
		`{"action":"install","runtime":"codex","token":"private-token"}`,
		`{"action":"update","runtime":"caelis","configuration":{"model":"x","secret":"private-secret"}}`,
		`{"action":"detect","runtime":"caelis","directory":"/other/node"}`,
		`{"action":"detect","runtime":"caelis","url":"https://untrusted.invalid/a"}`,
		`{"action":"detect","runtime":"caelis"} {"action":"install"}`,
	} {
		var output bytes.Buffer
		err := RunCommand(context.Background(), filepath.Join(t.TempDir(), "untouched"), strings.NewReader(input), &output)
		if err == nil {
			t.Fatal("unsafe native request accepted")
		}
		if strings.Contains(output.String(), "private-") {
			t.Fatal("secret input echoed")
		}
		var status Status
		if json.Unmarshal(output.Bytes(), &status) != nil || status.Outcome != "rejected" {
			t.Fatal("missing rejection envelope")
		}
	}
}

func TestVersionVerificationDoesNotInheritCredentialsOrHome(t *testing.T) {
	manager, archives := fixtureManager(t)
	t.Setenv("OPENAI_API_KEY", "private-credential")
	t.Setenv("CODEX_HOME", "/private/user-config")
	script := "#!/bin/sh\n[ -z \"${OPENAI_API_KEY+x}\" ] || exit 2\n[ \"$CODEX_HOME\" != /private/user-config ] || exit 3\n[ ! -f \"$HOME/user-marker\" ] || exit 4\nprintf '%s\\n' 'codex-cli 1.0.0'\n"
	release, archive := fixtureRelease(t, "codex", "1.0.0", archiveEntry{name: "bin/codex", body: script})
	manager.releases[0] = release
	archives["codex1.0.0"] = archive
	install(t, manager, "codex", "1.0.0", "empty-home")
	matches, _ := filepath.Glob(filepath.Join(manager.directory, "codex/releases/1.0.0/bin/.verify-home-*"))
	if len(matches) != 0 {
		t.Fatal("verification home leaked")
	}
}

func TestHomeDirectoryRejectsSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "redirect")); err != nil {
		t.Fatal(err)
	}
	if homeDirectory(home, filepath.Join(home, "redirect", "managed")) {
		t.Fatal("user-directory symlink escaped to external location")
	}
	if homeDirectory(home, home) {
		t.Fatal("home itself accepted as installation root")
	}
	if !homeDirectory(home, filepath.Join(home, ".local", "managed")) {
		t.Fatal("private new user directory rejected")
	}
}

func TestManagedDirectorySymlinkDoesNotRedirectInstallation(t *testing.T) {
	manager, _ := fixtureManager(t)
	root, err := manager.openRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir("other", 0700); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink("other", "caelis"); err != nil {
		t.Fatal(err)
	}
	root.Close()
	status, err := manager.Manage(context.Background(), Request{Action: "install", Runtime: "caelis", Version: "1.0.0", RequestID: "symlink-root"})
	if err == nil || status.Outcome != "rejected" {
		t.Fatal("existing symlink redirected managed release")
	}
	children, err := os.ReadDir(filepath.Join(manager.directory, "other"))
	if err != nil || len(children) != 0 {
		t.Fatal("symlink target was changed")
	}
}

func TestInstalledReleaseDirectorySymlinkRejected(t *testing.T) {
	manager, _ := fixtureManager(t)
	install(t, manager, "caelis", "1.0.0", "original")
	versionDir := filepath.Join(manager.directory, "caelis/releases/1.0.0")
	if err := os.Rename(versionDir, filepath.Join(manager.directory, "retained")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../retained", versionDir); err != nil {
		t.Fatal(err)
	}
	if status, err := manager.Manage(context.Background(), Request{Action: "detect", Runtime: "caelis"}); err == nil || status.Outcome != "rejected" {
		t.Fatal("symlinked release was reported installed")
	}
}
