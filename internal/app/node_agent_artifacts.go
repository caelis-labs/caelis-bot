package app

import (
	"crypto/sha256"
	"debug/macho"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

// nodeAgentBuildRevision is pinned by the same build that seals the APP bundle.
// Native tests can inject an artifact resolver; renderer input never selects a
// local executable, download URL or expected digest.
var nodeAgentBuildRevision string

// DefaultNativeJoinHelper is the signed standalone native protocol helper,
// rather than the Wails APP executable. It accepts no renderer-selected path.
func DefaultNativeJoinHelper() (string, error) {
	return defaultNativeNodeExecutable("caelis-agent")
}

// DefaultNativeNodeHost is the independently signed headless owner. Its native
// power/process adapter is built with cgo; it does not depend on the APP UI.
func DefaultNativeNodeHost() (string, error) {
	return defaultNativeNodeExecutable("caelis-node")
}

// Resolve only native executable-relative package locations: the desktop APP
// uses Contents/Resources/NodeAgent; standalone helpers use their adjacent
// manifest. There is no CWD/env/wire-selected fallback or manifest search.
func nodeAgentArtifactDirectory(executable string) string {
	directory := filepath.Dir(executable)
	if filepath.Base(directory) == "MacOS" && filepath.Base(filepath.Dir(directory)) == "Contents" {
		return filepath.Join(filepath.Dir(directory), "Resources", "NodeAgent")
	}
	return directory
}

func defaultNativeNodeExecutable(program string) (string, error) {
	executable, err := os.Executable()
	if err != nil || nodeAgentBuildRevision == "" {
		return "", errors.New("packaged native join helper unavailable")
	}
	directory := nodeAgentArtifactDirectory(executable)
	file := filepath.Join(directory, "manifest.json")
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
		return "", errors.New("packaged native join manifest unavailable")
	}
	b, err := os.ReadFile(file)
	var manifest nodeAgentManifest
	if err != nil || len(b) > 16<<10 || json.Unmarshal(b, &manifest) != nil || manifest.Version != 1 || manifest.SourceRevision != nodeAgentBuildRevision {
		return "", errors.New("packaged native join source mismatch")
	}
	name := program + "-darwin-" + runtime.GOARCH
	var digest string
	for _, entry := range manifest.Artifacts {
		if entry.OS == "darwin" && entry.Arch == runtime.GOARCH && entry.File == name {
			if digest != "" || entry.File != name {
				return "", errors.New("packaged native join manifest incompatible")
			}
			digest = entry.SHA256
		}
	}
	path, err := filepath.Abs(filepath.Join(directory, name))
	if err != nil || digest == "" {
		return "", errors.New("packaged native join architecture unavailable")
	}
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return "", errors.New("packaged native join bytes unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("packaged native join bytes changed")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil || hex.EncodeToString(h.Sum(nil)) != digest {
		return "", errors.New("packaged native join checksum mismatch")
	}
	if _, err = f.Seek(0, 0); err != nil {
		return "", err
	}
	m, err := macho.NewFile(f)
	if err != nil {
		return "", errors.New("packaged native join executable incompatible")
	}
	defer m.Close()
	want := macho.CpuArm64
	if runtime.GOARCH == "amd64" {
		want = macho.CpuAmd64
	}
	if m.Cpu != want || m.Type != macho.TypeExec {
		return "", errors.New("packaged native join architecture mismatch")
	}
	return path, nil
}

type nodeAgentManifest struct {
	Version        int                      `json:"version"`
	SourceRevision string                   `json:"sourceRevision"`
	Artifacts      []nodeAgentManifestEntry `json:"artifacts"`
}

type nodeAgentManifestEntry struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

// DefaultNodeAgentArtifact resolves only the verified headless payload in this
// installed APP or adjacent standalone helper package. Source checkouts
// without packaged payloads fail clearly; they
// must use an explicitly reviewed native resolver for temporary acceptance.
func DefaultNodeAgentArtifact(arch string) (nodeagent.Artifact, error) {
	executable, err := os.Executable()
	if err != nil {
		return nodeagent.Artifact{}, errors.New("packaged node agent unavailable")
	}
	directory := nodeAgentArtifactDirectory(executable)
	return readNodeAgentArtifact(directory, arch, nodeAgentBuildRevision)
}

func readNodeAgentArtifact(directory, arch, revision string) (nodeagent.Artifact, error) {
	if revision == "" || (arch != "amd64" && arch != "arm64") {
		return nodeagent.Artifact{}, errors.New("packaged node agent source unavailable")
	}
	file := filepath.Join(directory, "manifest.json")
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
		return nodeagent.Artifact{}, errors.New("packaged node agent manifest unavailable")
	}
	f, err := os.Open(file)
	if err != nil {
		return nodeagent.Artifact{}, errors.New("packaged node agent manifest unavailable")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nodeagent.Artifact{}, errors.New("packaged node agent manifest changed")
	}
	b, err := io.ReadAll(io.LimitReader(f, (16<<10)+1))
	var manifest nodeAgentManifest
	if err != nil || len(b) > 16<<10 || json.Unmarshal(b, &manifest) != nil || manifest.Version != 1 || manifest.SourceRevision != revision {
		return nodeagent.Artifact{}, errors.New("packaged node agent source mismatch")
	}
	result := nodeagent.Artifact{Arch: arch, SourceRevision: revision}
	for _, entry := range manifest.Artifacts {
		if entry.OS != "linux" || entry.Arch != arch {
			continue
		}
		absolute, err := filepath.Abs(filepath.Join(directory, entry.File))
		if err != nil {
			return nodeagent.Artifact{}, err
		}
		switch entry.File {
		case "caelis-agent-linux-" + arch:
			if result.Path != "" {
				return nodeagent.Artifact{}, errors.New("duplicate packaged node agent")
			}
			result.Path, result.ExpectedSHA256 = absolute, entry.SHA256
		case "caelis-node-linux-" + arch:
			if result.HostPath != "" {
				return nodeagent.Artifact{}, errors.New("duplicate packaged node host")
			}
			result.HostPath, result.HostExpectedSHA256 = absolute, entry.SHA256
		default:
			return nodeagent.Artifact{}, errors.New("packaged node agent manifest incompatible")
		}
	}
	if result.Path == "" || result.HostPath == "" {
		return nodeagent.Artifact{}, errors.New("packaged node agent architecture unavailable")
	}
	if err := nodeagent.VerifyArtifact(result); err != nil {
		return nodeagent.Artifact{}, err
	}
	return result, nil
}
