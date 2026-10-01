package nodeagent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sshKeyword = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// SanitizedSSHConfiguration consumes native ssh -G output without exposing it.
// Connection, authentication references and known-host trust remain exact.
// Forwardings and session side effects are removed. Arbitrary executable
// proxy/known-host hooks cannot be represented by this private join profile.
func SanitizedSSHConfiguration(effective []byte) ([]byte, error) {
	if len(effective) == 0 || len(effective) > 256<<10 {
		return nil, errors.New("effective SSH configuration unavailable")
	}
	var out strings.Builder
	found := map[string]bool{}
	drop := map[string]bool{"stricthostkeychecking": true, "updatehostkeys": true, "localforward": true, "remoteforward": true, "dynamicforward": true, "controlmaster": true, "controlpath": true, "controlpersist": true, "clearallforwardings": true, "forwardagent": true, "forwardx11": true, "forwardx11trusted": true, "gatewayports": true, "streamlocalbindmask": true, "streamlocalbindunlink": true, "exitonforwardfailure": true, "permitlocalcommand": true, "localcommand": true, "remotecommand": true, "requesttty": true, "forkafterauthentication": true, "sessiontype": true, "stdinnull": true, "sendenv": true, "loglevel": true, "escapechar": true}
	for _, line := range strings.Split(string(effective), "\n") {
		key, value, ok := strings.Cut(line, " ")
		key = strings.ToLower(key)
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !ok || !sshKeyword.MatchString(key) || strings.ContainsAny(value, "\x00\r\n") {
			return nil, errors.New("effective SSH configuration incompatible")
		}
		switch key {
		case "proxycommand", "proxyjump", "knownhostscommand", "setenv":
			if value != "none" && strings.TrimSpace(value) != "" {
				return nil, errors.New("SSH join requires a connection without executable proxy or secret environment hooks")
			}
			continue
		case "host":
			if !sshTarget.MatchString(value) {
				return nil, errors.New("effective SSH target incompatible")
			}
			continue
		case "match", "include":
			return nil, errors.New("unresolved SSH configuration")
		}
		if drop[key] {
			continue
		}
		// A single identity/certificate/agent pathname is quoted as one value; ssh
		// -G emits its already expanded native pathname without shell evaluation.
		if key == "identityfile" || key == "certificatefile" || key == "identityagent" {
			value = "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(value) + "\""
		}
		out.WriteString(key + " " + value + "\n")
		found[key] = true
	}
	for _, key := range []string{"hostname", "user", "port", "userknownhostsfile", "globalknownhostsfile"} {
		if !found[key] {
			return nil, errors.New("effective SSH connection or host trust metadata incomplete")
		}
	}
	out.WriteString("StrictHostKeyChecking yes\nUpdateHostKeys no\nForwardAgent no\nForwardX11 no\nForwardX11Trusted no\nControlMaster no\nControlPath none\nControlPersist no\nPermitLocalCommand no\nRequestTTY no\nForkAfterAuthentication no\nGatewayPorts no\nStreamLocalBindMask 0177\nStreamLocalBindUnlink no\n")
	return []byte(out.String()), nil
}

func resolvedJoinConfig(ctx context.Context, s SSHConfig) (string, func(), error) {
	args, err := s.args()
	if err != nil {
		return "", nil, err
	}
	args = append([]string{"-G"}, args...)
	args = append(args, "-o", "ClearAllForwardings=yes", "--", s.Target)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.binary(), args...)
	var output configOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return "", nil, errors.New("effective existing SSH connection unavailable")
	}
	config, err := SanitizedSSHConfiguration(output.Bytes())
	if err != nil {
		return "", nil, err
	}
	directory, err := os.MkdirTemp("/tmp", "caelis-ssh-join-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	path := filepath.Join(directory, "config")
	if err := os.WriteFile(path, config, 0600); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

type configOutput struct{ bytes.Buffer }

func (b *configOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256<<10 {
		return 0, errors.New("effective SSH configuration limit")
	}
	return b.Buffer.Write(p)
}
