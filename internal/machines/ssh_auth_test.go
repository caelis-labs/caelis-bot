package machines

import (
	"bufio"
	"context"
	"crypto/rand"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Real /usr/bin/ssh against an isolated synthetic server, without changing the
// user's password, authorized_keys, keychain, known_hosts or running ssh-agent.
func TestNativeSSHAuthentication(t *testing.T) {
	python := os.Getenv("CAELIS_BOT_SSH_AUTH_PYTHON")
	if python == "" {
		t.Skip("set CAELIS_BOT_SSH_AUTH_PYTHON for opt-in native authentication acceptance")
	}
	root := t.TempDir()
	fixture, err := filepath.Abs("testdata/ssh-auth.py")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, fixture, root)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal("fixture startup", err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	// Direct address with no preconfigured SSH alias.
	in := api.MachineInput{Address: "127.0.0.1", Port: port, User: "fixture", Authentication: "password"}
	resolved, jump, err := resolve(ctx, in)
	if err != nil || jump != "" || resolved.Port != port {
		t.Fatal("direct address", err)
	}
	key, fp, err := scanKey(ctx, in, "")
	if err != nil || fp == "" {
		t.Fatal("native host-key scan", err)
	}
	s, err := Open(filepath.Join(root, `Application Support`, `Machines %h "quoted"\path`), inert{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Separate agent owned by this fixture; never add keys to the user's agent.
	agentOut, err := exec.Command("/usr/bin/ssh-agent", "-s").Output()
	if err != nil {
		t.Fatal(err)
	}
	agentPID := ""
	for _, line := range strings.Split(string(agentOut), "\n") {
		field := strings.SplitN(line, ";", 2)[0]
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		if k == "SSH_AUTH_SOCK" {
			t.Setenv(k, v)
		}
		if k == "SSH_AGENT_PID" {
			agentPID = v
		}
	}
	if agentPID == "" {
		t.Fatal("fixture agent unavailable")
	}
	t.Cleanup(func() {
		pid, _ := strconv.Atoi(agentPID)
		if p, e := os.FindProcess(pid); e == nil {
			p.Kill()
		}
	})
	if err = exec.Command("/usr/bin/ssh-add", filepath.Join(root, "client.plain")).Run(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"password", "key", "passphrase", "agent", "config-password", "wrong-password", "untrusted-host"} {
		t.Run(mode, func(t *testing.T) {
			id := "machine-" + rand.Text()
			dir := filepath.Join(s.root, id)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			host := "[127.0.0.1]:" + strconv.Itoa(port)
			if err := os.WriteFile(filepath.Join(dir, "known_hosts"), []byte(host+" "+key+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			view := api.Machine{ID: id, Address: in.Address, Port: port, User: in.User, Authentication: mode}
			switch mode {
			case "password":
				s.secrets[id] = "fixture-password"
			case "config-password":
				view.Authentication = "agent"
				view.SSHConfig = true
				s.secrets[id] = "fixture-password"
				t.Setenv("SSH_AUTH_SOCK", "")
			case "key":
				view.PrivateKey = filepath.Join(root, "client.plain")
			case "passphrase":
				view.Authentication = "key"
				view.PrivateKey = filepath.Join(root, "client.encrypted")
				s.secrets[id] = "fixture-passphrase"
			case "wrong-password":
				view.Authentication = "password"
				s.secrets[id] = "wrong"
			case "untrusted-host":
				view.Authentication = "agent"
				otherKey, err := exec.Command("/usr/bin/ssh-keygen", "-y", "-f", filepath.Join(root, "client.plain")).Output()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "known_hosts"), []byte(host+" "+strings.TrimSpace(string(otherKey))+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			b, err := s.command(ctx, profile{View: view}, "printf auth-proof", nil)
			if mode == "untrusted-host" {
				if err == nil || err.Error() != "ssh_host_verification_failed" {
					t.Fatal("host verification failure must be rejected and distinguished from authentication", err)
				}
				return
			}
			if mode == "wrong-password" {
				if err == nil {
					t.Fatal("accepted incorrect password")
				}
				return
			}
			if err != nil || string(b) != "AUTH_OK\n" {
				t.Fatal("native SSH authentication", err)
			}
			files, err := os.ReadDir(s.root)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if strings.HasPrefix(file.Name(), ".auth-") {
					t.Fatal("plaintext askpass file retained")
				}
			}
		})
	}
}
