// Package runtimemanagement installs reviewed Linux releases into an explicitly
// selected user directory. It owns no SSH credentials, model configuration,
// shared service or autostart policy. The existing runtime setup APIs own those.
package runtimemanagement

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type Request struct {
	Action          string `json:"action"`
	Runtime         string `json:"runtime"`
	Version         string `json:"version,omitempty"`
	RequestID       string `json:"requestId,omitempty"`
	ExpectedVersion string `json:"expectedVersion,omitempty"`
}

type Status struct {
	Runtime       string `json:"runtime,omitempty"`
	Installed     bool   `json:"installed"`
	Version       string `json:"version,omitempty"`
	LatestVersion string `json:"latestVersion,omitempty"`
	UpdateState   string `json:"updateState,omitempty"`
	RequestID     string `json:"requestId,omitempty"`
	Outcome       string `json:"outcome"`
	Message       string `json:"message"`
}

type Manager struct {
	directory, arch string
	releases        []Release
	fetch           func(context.Context, Release) (io.ReadCloser, error)
	verify          func(context.Context, string, Release) error
	write           func(*os.Root, string, any) (bool, error)
	mu              sync.Mutex
}

const ownerMarker = "Caelis Bot explicit runtime installation v1\n"

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// New binds native installation authority before a request arrives. Product
// commands must bind this directory through trusted node configuration, never
// through model prose, remote URLs or a client-supplied product command field.
func New(directory string) (*Manager, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("managed remote installation requires Linux")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.New("user installation root unavailable")
	}
	if !homeDirectory(home, directory) {
		return nil, errors.New("installation directory must be inside this user's home")
	}
	return newManager(directory, runtime.GOARCH, officialReleases), nil
}

func homeDirectory(home, directory string) bool {
	if !filepath.IsAbs(directory) {
		return false
	}
	relative, err := filepath.Rel(home, directory)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return false
	}
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return false
	}
	ancestor := directory
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return false
		}
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return false
	}
	relative, err = filepath.Rel(canonicalHome, resolved)
	return err == nil && (relative == "." || filepath.IsLocal(relative))
}

func newManager(directory, arch string, releases []Release) *Manager {
	return &Manager{directory: filepath.Clean(directory), arch: arch, releases: append([]Release(nil), releases...), fetch: officialDownload, verify: verifyRelease, write: writeAtomic}
}

func (m *Manager) release(provider, version string) (Release, bool) {
	for _, r := range m.releases {
		if r.Runtime == provider && r.Version == version && r.Arch == m.arch {
			return r, true
		}
	}
	return Release{}, false
}
func (m *Manager) latest(provider string) (Release, bool) {
	var result Release
	for _, r := range m.releases {
		if r.Runtime == provider && r.Arch == m.arch && (result.Version == "" || compareVersions(r.Version, result.Version) > 0) {
			result = r
		}
	}
	return result, result.Version != ""
}
func compareVersions(a, b string) int {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(aa) {
			x, _ = strconv.Atoi(aa[i])
		}
		if i < len(bb) {
			y, _ = strconv.Atoi(bb[i])
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func (m *Manager) openRoot(create bool) (*os.Root, error) {
	info, err := os.Lstat(m.directory)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil, nil
		}
		if err := os.MkdirAll(filepath.Dir(m.directory), 0700); err != nil {
			return nil, errors.New("could not prepare user installation directory")
		}
		if err := os.Mkdir(m.directory, 0700); err != nil {
			return nil, errors.New("installation directory changed; inspect before retrying")
		}
		root, err := os.OpenRoot(m.directory)
		if err != nil {
			return nil, errors.New("could not open installation directory")
		}
		file, err := root.OpenFile(".owner", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			_, err = file.WriteString(ownerMarker)
			if err == nil {
				err = file.Sync()
			}
			closed := file.Close()
			if err == nil {
				err = closed
			}
		}
		if err != nil {
			_ = root.Close()
			return nil, errors.New("could not establish installation ownership")
		}
		return root, nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !privateOwner(info) {
		return nil, errors.New("installation directory is not private to this user")
	}
	root, err := os.OpenRoot(m.directory)
	if err != nil {
		return nil, errors.New("could not open installation directory")
	}
	markerInfo, markerErr := root.Lstat(".owner")
	if markerErr != nil || !markerInfo.Mode().IsRegular() || markerInfo.Mode().Perm() != 0600 || !privateOwner(markerInfo) || markerInfo.Size() != int64(len(ownerMarker)) {
		_ = root.Close()
		return nil, errors.New("installation ownership marker is invalid")
	}
	marker, err := root.ReadFile(".owner")
	if err != nil || string(marker) != ownerMarker {
		_ = root.Close()
		return nil, errors.New("installation directory is not managed by this installer")
	}
	return root, nil
}

type activeRelease struct {
	Schema        int    `json:"schema"`
	Runtime       string `json:"runtime"`
	Version       string `json:"version"`
	ArchiveSHA256 string `json:"archiveSHA256"`
	BinarySHA256  string `json:"binarySHA256"`
	RequestID     string `json:"requestId"`
	Digest        string `json:"digest"`
}
type receipt struct {
	Schema  int     `json:"schema"`
	Request Request `json:"request"`
	Digest  string  `json:"digest"`
	Phase   string  `json:"phase"`
	Status  Status  `json:"status"`
}

func readJSON(root *os.Root, name string, out any) error {
	if err := privateParents(root, name); err != nil {
		return err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !privateOwner(info) || info.Size() > 1<<20 {
		return errors.New("managed state is not private and regular")
	}
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return errors.New("managed state is invalid")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("managed state has trailing content")
	}
	return nil
}
func fileDigest(root *os.Root, name string) (string, error) {
	if err := privateParents(root, name); err != nil {
		return "", errors.New("installed executable directory is invalid")
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Mode().Perm()&0100 == 0 || !privateOwner(info) || info.Size() > maxExpanded {
		return "", errors.New("installed executable is invalid")
	}
	file, err := root.Open(name)
	if err != nil {
		return "", errors.New("installed executable unavailable")
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxExpanded+1)); err != nil {
		return "", errors.New("installed executable unavailable")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func (m *Manager) active(root *os.Root, provider string) (activeRelease, bool, error) {
	var current activeRelease
	if root == nil {
		return current, false, nil
	}
	err := readJSON(root, filepath.Join(provider, "current.json"), &current)
	if errors.Is(err, os.ErrNotExist) {
		return current, false, nil
	}
	if err != nil {
		return current, false, errors.New("installed selection cannot be verified")
	}
	release, ok := m.release(provider, current.Version)
	if current.Schema != 1 || current.Runtime != provider || !ok || release.SHA256 != current.ArchiveSHA256 {
		return current, false, errors.New("installed selection does not match a reviewed release")
	}
	hash, err := fileDigest(root, filepath.Join(provider, "releases", release.Version, release.Binary))
	if err != nil || hash != current.BinarySHA256 {
		return current, false, errors.New("installed executable changed; selection was preserved")
	}
	return current, true, nil
}

func reject(status Status, message string) (Status, error) {
	status.Outcome = "rejected"
	status.Message = message
	return status, errors.New(message)
}
func unknown(status Status, message string) (Status, error) {
	status.Outcome = "unknown"
	status.Message = message
	return status, errors.New(message)
}
func digestRequest(request Request) string {
	body, _ := json.Marshal(request)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func receiptName(provider, id string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + id))
	return filepath.Join("receipts", hex.EncodeToString(sum[:])+".json")
}

func (m *Manager) Manage(ctx context.Context, request Request) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{Outcome: "rejected"}
	latest, ok := m.latest(request.Runtime)
	if !ok {
		return reject(status, "runtime or Linux architecture is unsupported")
	}
	status.Runtime = request.Runtime
	status.LatestVersion = latest.Version
	switch request.Action {
	case "detect", "check-update", "install", "update", "resolve":
	default:
		return reject(status, "runtime action is unsupported")
	}
	mutation := request.Action == "install" || request.Action == "update"
	if (mutation || request.Action == "resolve") && !identifier.MatchString(request.RequestID) {
		return reject(status, "stable request ID is required")
	}
	if request.RequestID != "" && !identifier.MatchString(request.RequestID) {
		return reject(status, "request ID is invalid")
	}
	status.RequestID = request.RequestID
	if request.Version != "" {
		if _, ok := m.release(request.Runtime, request.Version); !ok {
			return reject(status, "release version is not in the reviewed catalog")
		}
	}
	if mutation && request.Version == "" {
		return reject(status, "explicit reviewed version is required")
	}
	root, err := m.openRoot(mutation)
	if err != nil {
		return reject(status, err.Error())
	}
	if root != nil {
		defer root.Close()
	}
	if mutation {
		unlock, err := lockRoot(ctx, root)
		if err != nil {
			return reject(status, "runtime management is busy or cancelled")
		}
		defer unlock()
	}
	var prior receipt
	name := receiptName(request.Runtime, request.RequestID)
	if root != nil && (mutation || request.Action == "resolve") {
		err := readJSON(root, name, &prior)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return unknown(status, "original installation receipt cannot be verified")
		}
		if err == nil {
			original := request
			if request.Action == "resolve" {
				original.Action = prior.Request.Action
			}
			if prior.Schema != 1 || prior.Digest != digestRequest(original) {
				return reject(status, "request ID is bound to different installation input")
			}
			if prior.Phase == "done" {
				return prior.Status, nil
			}
			current, installed, err := m.active(root, request.Runtime)
			if err == nil && installed && current.RequestID == request.RequestID && current.Digest == prior.Digest {
				status.Installed = true
				status.Version = current.Version
				status.Outcome = "accepted"
				status.UpdateState = "current"
				status.Message = "Original installation selection confirmed"
				prior.Phase = "done"
				prior.Status = status
				_, _ = m.write(root, name, prior)
				return status, nil
			}
			return unknown(status, "original installation outcome is unresolved; do not redispatch")
		}
	}
	if request.Action == "resolve" {
		return reject(status, "original installation request was not recorded")
	}
	current, installed, err := m.active(root, request.Runtime)
	if err != nil {
		return reject(status, err.Error())
	}
	status.Installed = installed
	status.Version = current.Version
	status.UpdateState = "not-installed"
	if installed {
		status.UpdateState = "current"
		if compareVersions(latest.Version, current.Version) > 0 {
			status.UpdateState = "available"
		}
	}
	if !mutation {
		status.Outcome = "accepted"
		status.Message = "Reviewed installation status inspected"
		return status, nil
	}
	if request.Action == "install" && installed {
		return reject(status, "managed runtime is already installed; use an explicit update")
	}
	if request.Action == "update" && (!installed || request.ExpectedVersion == "" || request.ExpectedVersion != current.Version) {
		return reject(status, "installed version changed or is unavailable; inspect before updating")
	}
	if request.Action == "install" && request.ExpectedVersion != "" {
		return reject(status, "installation expected an existing version")
	}
	if installed && compareVersions(request.Version, current.Version) < 0 {
		return reject(status, "update would replace a newer release")
	}
	digest := digestRequest(request)
	prior = receipt{Schema: 1, Request: request, Digest: digest, Phase: "intent", Status: status}
	if err := privateMkdir(root, "receipts"); err != nil {
		return reject(status, "installation receipt directory unavailable")
	}
	if _, err := m.write(root, name, prior); err != nil {
		return unknown(status, "installation intent could not be durably confirmed")
	}
	release, _ := m.release(request.Runtime, request.Version)
	binaryHash, err := m.stage(ctx, root, release)
	if err != nil {
		status, _ = reject(status, err.Error())
		prior.Phase = "done"
		prior.Status = status
		if _, writeErr := m.write(root, name, prior); writeErr != nil {
			return unknown(status, "installation rejection could not be recorded")
		}
		return status, err
	}
	next := activeRelease{Schema: 1, Runtime: request.Runtime, Version: release.Version, ArchiveSHA256: release.SHA256, BinarySHA256: binaryHash, RequestID: request.RequestID, Digest: digest}
	published, err := m.write(root, filepath.Join(request.Runtime, "current.json"), next)
	if err != nil {
		if published {
			return unknown(status, "selection changed; confirm the original installation request")
		}
		status, _ = reject(status, "selection publication failed; previous runtime preserved")
		prior.Phase = "done"
		prior.Status = status
		_, _ = m.write(root, name, prior)
		return status, errors.New(status.Message)
	}
	status.Installed = true
	status.Version = release.Version
	status.Outcome = "accepted"
	status.UpdateState = "current"
	if compareVersions(latest.Version, release.Version) > 0 {
		status.UpdateState = "available"
	}
	status.Message = "Reviewed release installed; service activation is separate"
	prior.Phase = "done"
	prior.Status = status
	if _, err := m.write(root, name, prior); err != nil {
		return unknown(status, "release installed; confirm the original installation request")
	}
	return status, nil
}

func (m *Manager) stage(ctx context.Context, root *os.Root, release Release) (string, error) {
	if err := privateMkdir(root, filepath.Join(release.Runtime, "releases")); err != nil {
		return "", errors.New("release directory unavailable")
	}
	target := filepath.Join(release.Runtime, "releases", release.Version)
	var cached activeRelease
	if err := readJSON(root, filepath.Join(target, "release.json"), &cached); err == nil {
		hash, err := fileDigest(root, filepath.Join(target, release.Binary))
		if err != nil || cached.Schema != 1 || cached.Runtime != release.Runtime || cached.Version != release.Version || cached.ArchiveSHA256 != release.SHA256 || cached.BinarySHA256 != hash {
			return "", errors.New("existing immutable release changed")
		}
		return hash, nil
	}
	if _, err := root.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("existing release directory cannot be replaced")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", errors.New("release staging identity unavailable")
	}
	stage := filepath.Join(release.Runtime, ".stage-"+hex.EncodeToString(random))
	archive := stage + ".tar.gz"
	if err := root.Mkdir(stage, 0700); err != nil {
		return "", errors.New("could not create release staging directory")
	}
	defer root.RemoveAll(stage)
	defer root.Remove(archive)
	source, err := m.archiveSource(ctx, root, release)
	if err != nil {
		return "", err
	}
	err = saveArchive(ctx, root, archive, release, source)
	closed := source.Close()
	if err != nil {
		return "", err
	}
	if closed != nil {
		return "", errors.New("official release download incomplete")
	}
	if err := extractArchive(ctx, root, archive, stage); err != nil {
		return "", err
	}
	binary := filepath.Join(stage, release.Binary)
	hash, err := fileDigest(root, binary)
	if err == nil && release.Runtime == "codex" {
		for _, companion := range []string{"bin/codex-code-mode-host", "codex-path/rg"} {
			if _, companionErr := fileDigest(root, filepath.Join(stage, companion)); companionErr != nil {
				err = errors.New("Codex native package is incomplete")
				break
			}
		}
	}
	if err != nil {
		return "", err
	}
	if err := m.verify(ctx, filepath.Join(m.directory, binary), release); err != nil {
		return "", errors.New("staged executable version could not be verified")
	}
	record := activeRelease{Schema: 1, Runtime: release.Runtime, Version: release.Version, ArchiveSHA256: release.SHA256, BinarySHA256: hash}
	if _, err := m.write(root, filepath.Join(stage, "release.json"), record); err != nil {
		return "", errors.New("could not persist verified release")
	}
	if err := root.Rename(stage, target); err != nil {
		return "", errors.New("could not publish immutable release; previous selection preserved")
	}
	return hash, nil
}

// BinaryPath is native-only. Never send this path as product connection authority.
func (m *Manager) BinaryPath(provider string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	root, err := m.openRoot(false)
	if err != nil || root == nil {
		return "", errors.New("managed runtime unavailable")
	}
	defer root.Close()
	current, installed, err := m.active(root, provider)
	if err != nil || !installed {
		return "", errors.New("managed runtime unavailable")
	}
	release, _ := m.release(provider, current.Version)
	return filepath.Join(m.directory, provider, "releases", release.Version, release.Binary), nil
}

func writeAtomic(root *os.Root, name string, value any) (bool, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return false, err
	}
	parent := filepath.Dir(name)
	if err := privateMkdir(root, parent); err != nil {
		return false, err
	}
	temporary := filepath.Join(parent, ".state-"+hex.EncodeToString(random))
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	defer root.Remove(temporary)
	err = json.NewEncoder(file).Encode(value)
	if err == nil {
		err = file.Sync()
	}
	closed := file.Close()
	if err == nil {
		err = closed
	}
	if err != nil {
		return false, err
	}
	if err := root.Rename(temporary, name); err != nil {
		return false, err
	}
	directory, err := root.Open(parent)
	if err != nil {
		return true, err
	}
	defer directory.Close()
	return true, directory.Sync()
}

func privateMkdir(root *os.Root, name string) error {
	if name == "." {
		return nil
	}
	clean, err := archiveName(filepath.ToSlash(name))
	if err != nil {
		return err
	}
	var current string
	for _, part := range strings.Split(clean, "/") {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(current, 0700); err != nil {
				return errors.New("managed directory changed")
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !privateOwner(info) {
			return errors.New("managed directory is not private and regular")
		}
	}
	return nil
}

func privateParents(root *os.Root, name string) error {
	parent := filepath.Dir(name)
	if parent == "." {
		return nil
	}
	clean, err := archiveName(filepath.ToSlash(parent))
	if err != nil {
		return err
	}
	var current string
	for _, part := range strings.Split(clean, "/") {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode().Perm() != 0700 || !privateOwner(info) {
			return errors.New("managed parent directory is invalid")
		}
	}
	return nil
}
