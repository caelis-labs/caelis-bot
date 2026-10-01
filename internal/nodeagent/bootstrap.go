package nodeagent

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Artifact is a native APP-owned release or explicitly reviewed local artifact.
// ExpectedSHA256 must come from that release manifest, never a remote response.
type Artifact struct {
	Path, ExpectedSHA256, Arch, SourceRevision string
	HostPath, HostExpectedSHA256               string
}
type BootstrapPlan struct {
	SSH       SSHConfig
	Artifact  Artifact
	Directory string
}

// VerifyArtifact checks bytes and executable architecture before any transfer.
func VerifyArtifact(a Artifact) error {
	if err := verifyExecutable(a.Path, a.ExpectedSHA256, a.Arch, a.SourceRevision); err != nil {
		return err
	}
	if a.HostPath != "" || a.HostExpectedSHA256 != "" {
		return verifyExecutable(a.HostPath, a.HostExpectedSHA256, a.Arch, a.SourceRevision)
	}
	return nil
}
func verifyExecutable(path, checksum, arch, revision string) error {
	a := Artifact{Path: path, ExpectedSHA256: checksum, Arch: arch, SourceRevision: revision}
	if !filepath.IsAbs(a.Path) || len(a.SourceRevision) != 40 || len(a.ExpectedSHA256) != 64 || (a.Arch != "amd64" && a.Arch != "arm64") {
		return errors.New("reviewed Linux agent artifact required")
	}
	if _, err := hex.DecodeString(a.SourceRevision); err != nil {
		return errors.New("reviewed agent source revision required")
	}
	info, err := os.Lstat(a.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 256<<20 {
		return errors.New("agent artifact unavailable")
	}
	f, err := os.Open(a.Path)
	if err != nil {
		return errors.New("agent artifact unavailable")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("agent artifact changed")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil || hex.EncodeToString(hash.Sum(nil)) != a.ExpectedSHA256 {
		return errors.New("agent artifact checksum mismatch")
	}
	if _, err = f.Seek(0, 0); err != nil {
		return err
	}
	var header [20]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return errors.New("agent ELF header unavailable")
	}
	want := uint16(62)
	if a.Arch == "arm64" {
		want = 183
	}
	kind := binary.LittleEndian.Uint16(header[16:18])
	if string(header[:4]) != "\x7fELF" || header[4] != 2 || header[5] != 1 || binary.LittleEndian.Uint16(header[18:20]) != want || (kind != 2 && kind != 3) {
		return errors.New("agent artifact architecture mismatch")
	}
	return nil
}

// ProbeArchitecture is a fixed, read-only bootstrap probe using existing SSH.
// The node-add owner must invoke it only after the user selects this target.
func ProbeArchitecture(ctx context.Context, s SSHConfig) (string, error) {
	args, err := s.args()
	if err != nil {
		return "", err
	}
	args = append(args, "-o", "ClearAllForwardings=yes", "--", s.Target, "uname -sm")
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.binary(), args...)
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return "", errors.New("SSH architecture probe unavailable")
	}
	switch strings.TrimSpace(out.String()) {
	case "Linux x86_64":
		return "amd64", nil
	case "Linux aarch64", "Linux arm64":
		return "arm64", nil
	}
	return "", errors.New("headless agent requires Linux amd64 or arm64")
}

// InstallVerified is the explicit node-add bootstrap action. It accepts no RPC
// command, credential, arbitrary download URL or persistent-service policy. A
// fixed receiver checks directory ownership, Linux architecture and SHA256,
// then publishes bytes in that user-owned directory. No Runtime is started.
func InstallVerified(ctx context.Context, p BootstrapPlan) error {
	if err := VerifyArtifact(p.Artifact); err != nil {
		return err
	}
	if !filepath.IsAbs(p.Directory) || filepath.Clean(p.Directory) != p.Directory || strings.ContainsAny(p.Directory, "\x00\r\n") {
		return errors.New("explicit target user directory required")
	}
	if p.Artifact.HostPath != "" {
		return installCompanions(ctx, p)
	}
	args, err := p.SSH.args()
	if err != nil {
		return err
	}
	machine := "x86_64"
	if p.Artifact.Arch == "arm64" {
		machine = "aarch64"
	}
	// Values are independently shell-quoted; this is a fixed binary bootstrap,
	// not the management transport. No user-controlled shell body is accepted.
	script := "set -eu; d=" + shellQuote(p.Directory) + "; test \"$(uname -s)\" = Linux; test \"$(uname -m)\" = " + shellQuote(machine) + "; test -d \"$d\"; test ! -L \"$d\"; test \"$(stat -c %u \"$d\")\" = \"$(id -u)\"; test \"$(stat -c %a \"$d\")\" = 700; umask 077; t=$(mktemp \"$d/.agent-stage.XXXXXX\"); trap 'rm -f \"$t\"' EXIT HUP INT TERM; cat >\"$t\"; test \"$(sha256sum \"$t\" | cut -d ' ' -f 1)\" = " + shellQuote(p.Artifact.ExpectedSHA256) + "; chmod 700 \"$t\"; mv -f \"$t\" \"$d/caelis-agent\""
	args = append(args, "-o", "ClearAllForwardings=yes", "--", p.SSH.Target, script)
	f, err := os.Open(p.Artifact.Path)
	if err != nil {
		return errors.New("agent artifact unavailable")
	}
	defer f.Close()
	// Recheck on the same opened descriptor; remote SHA also rejects a changed
	// source during upload without publishing it.
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil || hex.EncodeToString(hash.Sum(nil)) != p.Artifact.ExpectedSHA256 {
		return errors.New("agent artifact changed before transfer")
	}
	_, _ = f.Seek(0, 0)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.SSH.binary(), args...)
	cmd.Stdin = f
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if cmd.Run() != nil {
		return errors.New("verified agent bootstrap unavailable")
	}
	return nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4096 {
		return 0, errors.New("native probe output limit")
	}
	return b.Buffer.Write(p)
}

// PrepareNodeDirectory is part of the explicit native Add action. The target
// directory is fixed under that SSH user's HOME, never supplied by a renderer.
// Existing permissions are checked, never repaired or broadened.
func PrepareNodeDirectory(ctx context.Context, s SSHConfig) (string, error) {
	args, err := s.args()
	if err != nil {
		return "", err
	}
	script := `set -eu; test "${HOME#/}" != "$HOME"; d="$HOME/.local/share/caelis-bot/node-agent"; umask 077; mkdir -p "$d"; test -d "$d"; test ! -L "$d"; test "$(stat -c %u "$d")" = "$(id -u)"; test "$(stat -c %a "$d")" = 700; printf '%s\n' "$d"`
	args = append(args, "-o", "ClearAllForwardings=yes", "--", s.Target, script)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.binary(), args...)
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return "", errors.New("native target directory unavailable")
	}
	directory := strings.TrimSpace(out.String())
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || strings.ContainsAny(directory, "\x00\r\n") {
		return "", errors.New("native target directory incompatible")
	}
	return directory, nil
}

// PrepareJoinDirectory allocates an explicit enrolled node's outgoing slot on
// the chosen SSH broker. Its short fixed HOME namespace keeps Unix paths
// bounded; the node ID is hashed locally and never becomes shell syntax.
func PrepareJoinDirectory(ctx context.Context, s SSHConfig, nodeID string) (string, error) {
	if !identifier.MatchString(nodeID) {
		return "", errors.New("enrolled outgoing node identity required")
	}
	sum := sha256.Sum256([]byte(nodeID))
	slot := hex.EncodeToString(sum[:])[:16]
	args, err := s.args()
	if err != nil {
		return "", err
	}
	script := `set -eu; test "${HOME#/}" != "$HOME"; system=$(uname -s); case "$system" in Linux|Darwin) ;; *) exit 1;; esac; check_dir() { test -d "$1"; test ! -L "$1"; if test "$system" = Linux; then test "$(stat -c %u "$1")" = "$(id -u)"; test "$(stat -c %a "$1")" = 700; else test "$(stat -f %u "$1")" = "$(id -u)"; test "$(stat -f %Lp "$1")" = 700; fi; }; b="$HOME/.caelis-bot-joins"; umask 077; mkdir -p "$b"; check_dir "$b"; d="$b/` + slot + `"; mkdir -p "$d"; check_dir "$d"; printf '%s\n' "$d"`

	args = append(args, "-o", "ClearAllForwardings=yes", "--", s.Target, script)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.binary(), args...)
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return "", errors.New("native outgoing join directory unavailable")
	}
	directory := strings.TrimSpace(out.String())
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || !validSocket(filepath.Join(directory, "agent.sock")) {
		return "", errors.New("native outgoing socket directory incompatible")
	}
	return directory, nil
}

// installCompanions verifies both staged executables before publishing either.
// Backups remain in the private staging directory until both renames succeed;
// the EXIT trap restores the previous pair on any interrupted publication.
func installCompanions(ctx context.Context, p BootstrapPlan) error {
	agent, err := os.ReadFile(p.Artifact.Path)
	if err != nil {
		return err
	}
	host, err := os.ReadFile(p.Artifact.HostPath)
	if err != nil {
		return err
	}
	if digestBytes(agent) != p.Artifact.ExpectedSHA256 || digestBytes(host) != p.Artifact.HostExpectedSHA256 {
		return errors.New("companion artifact changed before transfer")
	}
	var payload bytes.Buffer
	tw := tar.NewWriter(&payload)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"caelis-agent", agent}, {"caelis-node", host}} {
		if err = tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0700, Size: int64(len(entry.data)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err = tw.Write(entry.data); err != nil {
			return err
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	args, err := p.SSH.args()
	if err != nil {
		return err
	}
	machine := "x86_64"
	if p.Artifact.Arch == "arm64" {
		machine = "aarch64"
	}
	script := `set -eu; d=` + shellQuote(p.Directory) + `; test "$(uname -s)" = Linux; test "$(uname -m)" = ` + shellQuote(machine) + `; test -d "$d"; test ! -L "$d"; test "$(stat -c %u "$d")" = "$(id -u)"; test "$(stat -c %a "$d")" = 700; umask 077; t=$(mktemp -d "$d/.companions.XXXXXX"); publishing=0; committed=0; rollback() { if test "$publishing" = 1 && test "$committed" = 0; then for n in caelis-agent caelis-node; do if test -f "$t/$n.old"; then mv -f "$t/$n.old" "$d/$n"; elif test -f "$t/$n.absent"; then rm -f "$d/$n"; fi; done; fi; rm -rf "$t"; }; trap rollback EXIT; trap 'exit 1' HUP INT TERM; tar -xf - -C "$t" caelis-agent caelis-node; test -f "$t/caelis-agent"; test ! -L "$t/caelis-agent"; test -f "$t/caelis-node"; test ! -L "$t/caelis-node"; test "$(sha256sum "$t/caelis-agent" | cut -d ' ' -f 1)" = ` + shellQuote(p.Artifact.ExpectedSHA256) + `; test "$(sha256sum "$t/caelis-node" | cut -d ' ' -f 1)" = ` + shellQuote(p.Artifact.HostExpectedSHA256) + `; chmod 700 "$t/caelis-agent" "$t/caelis-node"; for n in caelis-agent caelis-node; do if test -e "$d/$n" || test -L "$d/$n"; then test -f "$d/$n"; test ! -L "$d/$n"; cp -p "$d/$n" "$t/$n.old"; else touch "$t/$n.absent"; fi; done; publishing=1; mv -f "$t/caelis-agent" "$d/caelis-agent"; mv -f "$t/caelis-node" "$d/caelis-node"; committed=1`
	args = append(args, "-o", "ClearAllForwardings=yes", "--", p.SSH.Target, script)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.SSH.binary(), args...)
	cmd.Stdin = &payload
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if cmd.Run() != nil {
		return errors.New("verified companion bootstrap unavailable")
	}
	return nil
}
func digestBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
