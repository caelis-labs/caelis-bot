package sshtransport

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func validManualHost(host string) bool {
	return net.ParseIP(host) != nil || validHost(host)
}
func validateTarget(v api.SSHTarget) error {
	if v.Port < 0 || v.Port > 65535 {
		return errors.New("ssh_target_invalid")
	}
	if v.Authentication == "" {
		if !validHost(v.Host) || v.SSHPort != 0 || v.User != "" || v.Identity != "" {
			return errors.New("ssh_target_invalid")
		}
		return nil
	}
	if !validManualHost(v.Host) || v.SSHPort < 1 || v.SSHPort > 65535 || v.User == "" || len(v.User) > 128 || strings.HasPrefix(v.User, "-") || strings.ContainsFunc(v.User, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return errors.New("ssh_target_invalid")
	}
	if v.Authentication != "password" && v.Authentication != "key" {
		return errors.New("ssh_target_invalid")
	}
	if v.Authentication == "key" && (len(v.Identity) != 64 || strings.ContainsAny(v.Identity, "/\\")) {
		return errors.New("ssh_key_required")
	}
	if v.Authentication == "password" && v.Identity != "" {
		return errors.New("ssh_target_invalid")
	}
	return nil
}
func targetKey(v api.SSHTarget) (string, error) { return Address(v) }
func Address(v api.SSHTarget) (string, error) {
	if v.Port == 0 {
		v.Port = 9317
	}
	if err := validateTarget(v); err != nil {
		return "", err
	}
	u := url.URL{Scheme: "ssh", Host: net.JoinHostPort(v.Host, strconv.Itoa(v.Port))}
	if v.Authentication != "" {
		q := url.Values{"sshPort": {strconv.Itoa(v.SSHPort)}, "user": {v.User}, "authentication": {v.Authentication}}
		if v.Identity != "" {
			q.Set("identity", v.Identity)
		}
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}
func Target(address string) (api.SSHTarget, bool) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "ssh" || u.User != nil || u.Path != "" || u.Fragment != "" {
		return api.SSHTarget{}, false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return api.SSHTarget{}, false
	}
	v := api.SSHTarget{Host: u.Hostname(), Port: port}
	if u.RawQuery != "" {
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return v, false
		}
		for key, values := range q {
			if len(values) != 1 || (key != "sshPort" && key != "user" && key != "authentication" && key != "identity") {
				return v, false
			}
		}
		v.SSHPort, _ = strconv.Atoi(q.Get("sshPort"))
		v.User = q.Get("user")
		v.Authentication = q.Get("authentication")
		v.Identity = q.Get("identity")
	}
	return v, validateTarget(v) == nil
}

func (t *Transport) manualArguments(v api.SSHTarget) ([]string, error) {
	if err := validateTarget(v); err != nil {
		return nil, err
	}
	if v.Authentication == "" {
		return nil, nil
	}
	// No shell, inherited forwards or user-defined executable configuration for a
	// manual destination. Existing Host connections still use normal OpenSSH layers.
	args := []string{"-o", "UserKnownHostsFile=" + quote(filepath.Join(t.home, ".ssh", "known_hosts")), "-p", strconv.Itoa(v.SSHPort), "-l", v.User, "-o", "StrictHostKeyChecking=ask", "-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no", "-o", "ForkAfterAuthentication=no"}
	if v.Authentication == "password" {
		return append(args, "-o", "PubkeyAuthentication=no", "-o", "PreferredAuthentications=keyboard-interactive,password"), nil
	}
	path, err := t.identityPath(v.Identity)
	if err != nil {
		return nil, err
	}
	return append(args, "-i", path, "-o", "IdentitiesOnly=yes", "-o", "PreferredAuthentications=publickey", "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no"), nil
}
