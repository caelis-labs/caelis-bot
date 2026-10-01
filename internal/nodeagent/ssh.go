package nodeagent

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var sshTarget = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._:-]{0,253}$`)

// SSHConfig contains ordinary existing SSH configuration. Tailscale addresses
// are ordinary Target values; this package never logs in or manages accounts.
type SSHConfig struct{ Binary, Target string }

func (s SSHConfig) args() ([]string, error) {
	if !sshTarget.MatchString(s.Target) || strings.Contains(s.Target, "::") {
		return nil, errors.New("invalid existing SSH target")
	}
	return []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UpdateHostKeys=no", "-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "ForwardX11Trusted=no", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ForkAfterAuthentication=no", "-o", "PermitLocalCommand=no", "-o", "ConnectTimeout=15"}, nil
}
func (s SSHConfig) binary() string {
	if s.Binary != "" {
		return s.Binary
	}
	return "ssh"
}
func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
func validSocket(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, ":\x00\r\n") && len(path) < 100
}

// JoinArgs describes one foreground outbound SSH connection. The destination
// socket is private; the caller must prepare its 0700 user-owned directory.
// The remote helper validates ownership in a separate preflight. Existing
// sockets are never removed or replaced. No TCP/public listener is requested.
func JoinArgs(s SSHConfig, helper, remoteDirectory, localSocket string) ([]string, error) {
	args, err := s.args()
	remoteSocket := filepath.Join(remoteDirectory, "agent.sock")
	if err != nil || !filepath.IsAbs(helper) || strings.ContainsAny(helper, "\x00\r\n") || !validSocket(remoteSocket) || !validSocket(localSocket) {
		return nil, errors.New("invalid private agent join")
	}
	args = append(args, "-o", "ExitOnForwardFailure=yes", "-o", "GatewayPorts=no", "-o", "StreamLocalBindMask=0177", "-o", "StreamLocalBindUnlink=no", "-R", remoteSocket+":"+localSocket, "--", s.Target, shellQuote(helper)+" verify-join-directory --directory "+shellQuote(remoteDirectory)+" --hold")
	return args, nil
}

// Join owns only a temporary reverse-forwarding process. Ending it detaches
// observation and never shuts down the agent or an existing Runtime.
func Join(ctx context.Context, s SSHConfig, helper, remoteDirectory, localSocket string) error {
	args, err := JoinArgs(s, helper, remoteDirectory, localSocket)
	if err != nil {
		return err
	}
	config, cleanup, err := resolvedJoinConfig(ctx, s)
	if err != nil {
		return err
	}
	defer cleanup()
	args = append([]string{"-F", config}, args...)
	verifyArgs, err := s.args()
	if err != nil {
		return err
	}
	verifyArgs = append([]string{"-F", config}, verifyArgs...)
	verifyArgs = append(verifyArgs, "-o", "ClearAllForwardings=yes", "--", s.Target, shellQuote(helper)+" verify-join-directory --directory "+shellQuote(remoteDirectory))
	verify := exec.CommandContext(ctx, s.binary(), verifyArgs...)
	verify.Stdout, verify.Stderr = io.Discard, io.Discard
	if verify.Run() != nil {
		return errors.New("private join destination unavailable")
	}
	// Persist only the explicit nonsecret pairing after its existing authorization
	// and private destination have been verified. The foreground serving agent
	// can bind a reviewed deployment to this exact outbound route.
	if err = WriteManagedPrivateJSON(filepath.Join(filepath.Dir(localSocket), "outgoing-route.json"), OutgoingRoute{s.Target, helper, remoteDirectory}); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, s.binary(), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, io.Discard, io.Discard
	if cmd.Run() != nil {
		return errors.New("private agent join unavailable")
	}
	return nil
}
