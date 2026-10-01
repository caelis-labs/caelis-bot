package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// NativeEnrollmentIdentity is native pairing metadata, never a renderer DTO.
// Its directory is the existing enrollment, not a roaming or reverse-join slot.
type NativeEnrollmentIdentity struct {
	NodeID, Directory string
}

func ReadNativeEnrollmentIdentity(directory, nodeID string) (NativeEnrollmentIdentity, error) {
	value := NativeEnrollmentIdentity{NodeID: nodeID, Directory: directory}
	if !identifier.MatchString(nodeID) || CheckPrivateDirectory(directory) != nil {
		return value, errors.New("existing enrolled coordinator required")
	}
	path := filepath.Join(directory, "node.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 || !privateFileOwnedByCurrentUser(info) {
		return value, errors.New("private enrolled coordinator identity unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return value, errors.New("private enrolled coordinator identity unavailable")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0600 || opened.Size() > 4096 || !privateFileOwnedByCurrentUser(opened) {
		return value, errors.New("private enrolled coordinator identity changed")
	}
	var identity struct {
		ID string `json:"id"`
	}
	d := json.NewDecoder(io.LimitReader(f, 4097))
	d.DisallowUnknownFields()
	if d.Decode(&identity) != nil || d.Decode(new(any)) != io.EOF || identity.ID != nodeID {
		return value, errors.New("enrolled coordinator identity changed")
	}
	return value, nil
}

// This closed local port uses the Service's actual enrolled identity. A public
// presentation alias must not be substituted for a machine's stored node ID.
func (s *Service) NativeEnrollmentIdentity() (NativeEnrollmentIdentity, error) {
	return ReadNativeEnrollmentIdentity(s.options.Directory, s.options.NodeID)
}

func ValidateRoamingCoordinatorIdentity(m RoamingManagedDeployment) error {
	identity := m.CoordinatorIdentity
	if identity == nil || !identifier.MatchString(identity.NodeID) || !filepath.IsAbs(identity.Directory) || filepath.Clean(identity.Directory) != identity.Directory || m.BrokerNodeID == "" || m.BrokerNodeID != api.LocalNodeID && identity.NodeID != m.BrokerNodeID {
		return errors.New("exact enrolled coordinator identity required")
	}
	return nil
}

// The helper and directory come only from frozen native enrollment. This is
// one closed read-only command, not a command-bearing RPC or renderer input.
func RoamingCoordinatorVerificationCommand(m RoamingManagedDeployment) (string, error) {
	if err := ValidateRoamingCoordinatorIdentity(m); err != nil || !filepath.IsAbs(m.JoinHelper) || filepath.Clean(m.JoinHelper) != m.JoinHelper {
		return "", errors.New("fixed enrolled coordinator helper required")
	}
	return shellQuote(m.JoinHelper) + " verify-join-directory --directory " + shellQuote(m.CoordinatorIdentity.Directory) + " --node-id " + shellQuote(m.CoordinatorIdentity.NodeID), nil
}

// SSHEnrollmentIdentity inspects only this SSH user's fixed native Node slot.
// It neither creates an identity nor adopts a Bot/session/Runtime owner.
type SSHEnrollmentIdentity struct {
	Directory, NodeID             string
	AgentAvailable, HostAvailable bool
}

func ProbeSSHEnrollmentIdentity(ctx context.Context, ssh SSHConfig) (SSHEnrollmentIdentity, error) {
	var value SSHEnrollmentIdentity
	args, err := ssh.args()
	if err != nil {
		return value, err
	}
	script := `set -eu; test "${HOME#/}" != "$HOME"; d="$HOME/.local/share/caelis-bot/node-agent"; test ! -L "$d"; printf '%s\n' "$d"; if test ! -e "$d"; then printf 'missing\n0\n0\n{}\n'; exit 0; fi; test -d "$d"; test "$(stat -c %u "$d")" = "$(id -u)"; test "$(stat -c %a "$d")" = 700; f="$d/node.json"; test ! -L "$f"; if test -e "$f"; then test -f "$f"; test "$(stat -c %u "$f")" = "$(id -u)"; test "$(stat -c %a "$f")" = 600; test "$(stat -c %s "$f")" -le 4096; printf 'existing\n'; else printf 'missing\n'; fi; for h in caelis-agent caelis-node; do p="$d/$h"; test ! -L "$p"; if test -e "$p"; then test -f "$p"; test -x "$p"; test "$(stat -c %u "$p")" = "$(id -u)"; test "$(stat -c %a "$p")" = 700; printf '1\n'; else printf '0\n'; fi; done; if test -e "$f"; then cat "$f"; else printf '{}\n'; fi`
	args = append(args, "-o", "ClearAllForwardings=yes", "--", ssh.Target, script)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ssh.binary(), args...)
	var out boundedOutput
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	if cmd.Run() != nil {
		return value, errors.New("existing SSH Node identity unavailable")
	}
	parts := strings.SplitN(out.String(), "\n", 5)
	if len(parts) != 5 || !filepath.IsAbs(parts[0]) || filepath.Clean(parts[0]) != parts[0] || strings.ContainsAny(parts[0], "\x00\r") || (parts[1] != "missing" && parts[1] != "existing") || (parts[2] != "0" && parts[2] != "1") || (parts[3] != "0" && parts[3] != "1") {
		return value, errors.New("existing SSH Node probe invalid")
	}
	var identity struct {
		ID string `json:"id"`
	}
	d := json.NewDecoder(strings.NewReader(parts[4]))
	d.DisallowUnknownFields()
	if d.Decode(&identity) != nil || d.Decode(new(any)) != io.EOF || (parts[1] == "existing" && (!identifier.MatchString(identity.ID) || identity.ID == api.LocalNodeID)) || (parts[1] == "missing" && identity.ID != "") {
		return value, errors.New("existing SSH Node identity invalid")
	}
	return SSHEnrollmentIdentity{Directory: parts[0], NodeID: identity.ID, AgentAvailable: parts[2] == "1", HostAvailable: parts[3] == "1"}, nil
}
