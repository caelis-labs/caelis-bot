package machines

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func quote(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
func validate(v api.MachineInput) error {
	if v.Address == "" || len(v.Address) > 255 || strings.HasPrefix(v.Address, "-") || strings.ContainsAny(v.Address, " \t\r\n\x00/@;$`()\"") {
		return errors.New("invalid_address")
	}
	if v.Port < 1 || v.Port > 65535 {
		return errors.New("invalid_port")
	}
	if len(v.User) > 128 || strings.ContainsAny(v.User, " \t\r\n\x00/@;$`()\"") || strings.HasPrefix(v.User, "-") {
		return errors.New("invalid_user")
	}
	if v.Authentication != "agent" && v.Authentication != "password" && v.Authentication != "key" {
		return errors.New("invalid_authentication")
	}
	if v.Authentication == "key" {
		if !filepath.IsAbs(v.PrivateKey) || strings.ContainsAny(v.PrivateKey, "\r\n\x00") {
			return errors.New("invalid_key")
		}
		if i, e := os.Stat(v.PrivateKey); e != nil || !i.Mode().IsRegular() {
			return errors.New("invalid_key")
		}
	}
	if len(v.Secret) > 16384 || strings.ContainsRune(v.Secret, 0) {
		return errors.New("invalid_secret")
	}
	if v.ID != "" {
		if !strings.HasPrefix(v.ID, "machine-") || len(v.ID) != 34 {
			return errors.New("machine_unknown")
		}
		for _, c := range strings.TrimPrefix(v.ID, "machine-") {
			if !(c >= 'A' && c <= 'Z' || c >= '2' && c <= '7') {
				return errors.New("machine_unknown")
			}
		}
	}
	return nil
}
func resolve(ctx context.Context, v api.MachineInput) (api.MachineInput, string, error) {
	if v.SSHConfig {
		hosts, err := configuredSSHHosts()
		if err != nil {
			return v, "", err
		}
		found := false
		for _, host := range hosts {
			if strings.EqualFold(host, v.Address) {
				found = true
				break
			}
		}
		if !found {
			return v, "", errors.New("ssh_config_host_missing")
		}
	}
	return resolveWithConfig(ctx, v, "")
}

// OpenSSH remains the authority for effective settings. Reusing a Host never
// passes form defaults as -l/-p/-i overrides, including on reconnect.
func resolveWithConfig(ctx context.Context, v api.MachineInput, config string) (api.MachineInput, string, error) {
	jump := ""
	originalAddress := v.Address
	if v.SSHConfig {
		v.Port, v.User, v.PrivateKey, v.Authentication = 22, "", "", "agent"
	}
	if e := validate(v); e != nil {
		return v, jump, e
	}
	args := []string{"-G"}
	if config != "" {
		args = append(args, "-F", config)
	}
	if v.Port != 22 {
		args = append(args, "-p", strconv.Itoa(v.Port))
	}
	if v.User != "" {
		args = append(args, "-l", v.User)
	}
	args = append(args, "--", v.Address)
	b, e := exec.CommandContext(ctx, "/usr/bin/ssh", args...).Output()
	if e != nil {
		return v, jump, errors.New("ssh_configuration")
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "hostname":
			v.Address = f[1]
		case "user":
			v.User = f[1]
		case "port":
			if v.SSHConfig || v.Port == 22 && v.Address != originalAddress {
				v.Port, _ = strconv.Atoi(f[1])
			}
		case "proxyjump":
			if f[1] != "none" {
				if strings.Contains(f[1], ",") {
					return v, jump, errors.New("unsupported_ssh_route")
				}
				jump = f[1]
			}
		case "proxycommand":
			if f[1] != "none" {
				return v, jump, errors.New("unsupported_ssh_route")
			}
		}
	}
	return v, jump, validate(v)
}
func scanKey(ctx context.Context, v api.MachineInput, jump string) (string, string, error) {
	var b []byte
	var e error
	if jump == "" {
		b, e = exec.CommandContext(ctx, "/usr/bin/ssh-keyscan", "-T", "8", "-p", strconv.Itoa(v.Port), "-t", "ed25519,ecdsa,rsa", v.Address).Output()
	} else {
		jump = strings.ReplaceAll(strings.ReplaceAll(jump, "[", ""), "]", "")
		command := "ssh-keyscan -T 8 -p " + strconv.Itoa(v.Port) + " -t ed25519,ecdsa,rsa " + quote(v.Address)
		b, e = exec.CommandContext(ctx, "/usr/bin/ssh", "-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ForwardAgent=no", "-o", "ConnectTimeout=8", "--", jump, command).Output()
	}
	if e != nil {
		return "", "", errors.New("unreachable")
	}
	for _, kind := range []string{"ssh-ed25519", "ecdsa-sha2-nistp256", "ssh-rsa"} {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) != 3 || f[1] != kind || (f[1] != "ssh-ed25519" && f[1] != "ecdsa-sha2-nistp256" && f[1] != "ssh-rsa") {
				continue
			}
			raw, e := base64.StdEncoding.DecodeString(f[2])
			if e != nil {
				continue
			}
			sum := sha256.Sum256(raw)
			return f[1] + " " + f[2], "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
		}
	}
	return "", "", errors.New("host_key_unavailable")
}
func (s *Service) controlDirectory() (string, error) {
	sum := sha256.Sum256([]byte(s.root))
	controlDir := filepath.Join("/tmp", "caelis-ssh-"+base64.RawURLEncoding.EncodeToString(sum[:9]))
	if err := os.Mkdir(controlDir, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(controlDir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("ssh_control_directory_invalid")
	}
	return controlDir, nil
}
func (s *Service) sshArgs(p profile, tty bool) ([]string, error) {
	d := filepath.Join(s.root, p.View.ID)
	controlDir, err := s.controlDirectory()
	if err != nil {
		return nil, err
	}
	a := []string{"-T"}
	if tty {
		a = []string{"-tt"}
	}
	a = append(a, "-p", strconv.Itoa(p.View.Port), "-l", p.View.User, "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+filepath.Join(d, "known_hosts"), "-o", "GlobalKnownHostsFile=/dev/null", "-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ConnectTimeout=12", "-o", "ControlMaster=auto", "-o", "ControlPersist=600", "-o", "ControlPath="+filepath.Join(controlDir, p.View.ID+".sock"), "-o", "UpdateHostKeys=no")
	switch p.View.Authentication {
	case "agent":
		batch := "yes"
		if p.View.SSHConfig && (s.secrets[p.View.ID] != "" || p.View.Remember) {
			batch = "no"
		}
		a = append(a, "-o", "BatchMode="+batch, "-o", "NumberOfPasswordPrompts=1")
	case "key":
		a = append(a, "-i", p.View.PrivateKey, "-o", "IdentitiesOnly=yes")
	case "password":
		a = append(a, "-o", "PreferredAuthentications=password,keyboard-interactive", "-o", "PubkeyAuthentication=no")
	}
	return append(a, "--", p.View.Address), nil
}
func (s *Service) command(ctx context.Context, p profile, command string, input io.Reader) ([]byte, error) {
	args, e := s.sshArgs(p, false)
	if e != nil {
		return nil, e
	}
	args = append(args, command)
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", args...)
	cmd.Stdin = input
	cmd.Stderr = io.Discard
	secret := s.secrets[p.View.ID]
	if secret == "" && p.View.Remember {
		secret, _ = loadSecret(p.View.ID)
	}
	var cleanup func()
	if secret != "" {
		d, e := os.MkdirTemp(s.root, ".auth-")
		if e != nil {
			return nil, e
		}
		cleanup = func() { os.RemoveAll(d) }
		defer cleanup()
		f := filepath.Join(d, "secret")
		if e = os.WriteFile(f, []byte(secret), 0600); e != nil {
			return nil, e
		}
		ask := filepath.Join(d, "askpass")
		if e = os.WriteFile(ask, []byte("#!/bin/sh\nexec /bin/cat "+quote(f)+"\n"), 0700); e != nil {
			return nil, e
		}
		cmd.Env = append(os.Environ(), "SSH_ASKPASS="+ask, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=caelis-bot")
	}
	b, e := cmd.Output()
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("ssh_authentication_or_connection")
	}
	if len(b) > 8<<20 {
		return nil, errors.New("remote_response_too_large")
	}
	return b, nil
}
