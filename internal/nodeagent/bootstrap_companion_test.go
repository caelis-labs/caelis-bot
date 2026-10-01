package nodeagent

import (
	"archive/tar"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompanionVerificationAndStagedUpload(t *testing.T) {
	dir := t.TempDir()
	elf := make([]byte, 32)
	copy(elf, "\x7fELF")
	elf[4] = 2
	elf[5] = 1
	binary.LittleEndian.PutUint16(elf[16:18], 2)
	binary.LittleEndian.PutUint16(elf[18:20], 62)
	agent := filepath.Join(dir, "agent")
	host := filepath.Join(dir, "host")
	_ = os.WriteFile(agent, elf, 0700)
	_ = os.WriteFile(host, append(elf, 42), 0700)
	hostBytes, _ := os.ReadFile(host)
	a := Artifact{Path: agent, ExpectedSHA256: digestBytes(elf), Arch: "amd64", SourceRevision: strings.Repeat("a", 40), HostPath: host, HostExpectedSHA256: digestBytes(hostBytes)}
	if err := VerifyArtifact(a); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "upload.tar")
	command := filepath.Join(dir, "command")
	fake := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + command + "'\ncat > '" + capture + "'\n"
	_ = os.WriteFile(fake, []byte(script), 0700)
	if err := InstallVerified(context.Background(), BootstrapPlan{SSH: SSHConfig{Binary: fake, Target: "fixture"}, Artifact: a, Directory: "/tmp/private-target"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(capture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for _, name := range []string{"caelis-agent", "caelis-node"} {
		h, err := tr.Next()
		if err != nil || h.Name != name || h.Typeflag != tar.TypeReg || h.Mode != 0700 {
			t.Fatalf("closed companion archive %v %v", h, err)
		}
	}
	argv, _ := os.ReadFile(command)
	if !strings.Contains(string(argv), "committed=1") || !strings.Contains(string(argv), "trap rollback EXIT") {
		t.Fatal("paired publication rollback missing")
	}
	a.HostExpectedSHA256 = strings.Repeat("0", 64)
	_ = os.Remove(capture)
	if err := InstallVerified(context.Background(), BootstrapPlan{SSH: SSHConfig{Binary: fake, Target: "fixture"}, Artifact: a, Directory: "/tmp/private-target"}); err == nil {
		t.Fatal("bad companion accepted")
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatal("transferred before verification")
	}
}
