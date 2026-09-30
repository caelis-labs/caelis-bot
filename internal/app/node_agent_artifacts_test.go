package app

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeAgentArtifactRejectsMixedSourceChangedBytesAndArchitecture(t *testing.T) {
	directory := t.TempDir()
	revision := strings.Repeat("a", 40)
	name := "caelis-agent-linux-amd64"
	data := make([]byte, 64)
	copy(data, "\x7fELF")
	data[4] = 2
	data[5] = 1
	binary.LittleEndian.PutUint16(data[16:18], 2)
	binary.LittleEndian.PutUint16(data[18:20], 62)
	h := sha256.Sum256(data)
	manifest := nodeAgentManifest{Version: 1, SourceRevision: revision, Artifacts: []nodeAgentManifestEntry{{OS: "linux", Arch: "amd64", File: name, SHA256: hex.EncodeToString(h[:])}}}
	writeManifest := func() {
		t.Helper()
		b, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, "manifest.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest()
	if err := os.WriteFile(filepath.Join(directory, name), data, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readNodeAgentArtifact(directory, "amd64", revision); err != nil {
		t.Fatal(err)
	}
	if _, err := readNodeAgentArtifact(directory, "amd64", strings.Repeat("b", 40)); err == nil {
		t.Fatal("mixed APP/helper source accepted")
	}
	if _, err := readNodeAgentArtifact(directory, "arm64", revision); err == nil {
		t.Fatal("missing architecture accepted")
	}
	data[32] = 1
	if err := os.WriteFile(filepath.Join(directory, name), data, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readNodeAgentArtifact(directory, "amd64", revision); err == nil {
		t.Fatal("altered helper accepted")
	}
	h = sha256.Sum256(data)
	manifest.Artifacts[0].SHA256 = hex.EncodeToString(h[:])
	writeManifest()
	binary.LittleEndian.PutUint16(data[18:20], 183)
	h = sha256.Sum256(data)
	manifest.Artifacts[0].SHA256 = hex.EncodeToString(h[:])
	writeManifest()
	if err := os.WriteFile(filepath.Join(directory, name), data, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readNodeAgentArtifact(directory, "amd64", revision); err == nil {
		t.Fatal("mislabeled architecture accepted")
	}
	manifest.Artifacts[0].File = "../other"
	writeManifest()
	if _, err := readNodeAgentArtifact(directory, "amd64", revision); err == nil {
		t.Fatal("manifest path escape accepted")
	}
}
