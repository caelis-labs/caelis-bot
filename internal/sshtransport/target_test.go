package sshtransport

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualTargetsKeepLoginAndBotPortsSeparate(t *testing.T) {
	v := fixtureTransport(t)
	key := filepath.Join(t.TempDir(), "private key")
	if err := os.WriteFile(key, []byte("fixture-not-real-key"), 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := v.RegisterIdentity(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []api.SSHTarget{{Host: "fedora", Port: 19329}, {Host: "192.0.2.1", Port: 19329, SSHPort: 2222, User: "root", Authentication: "password"}, {Host: "::1", Port: 19329, SSHPort: 2222, User: "root", Authentication: "key", Identity: identity.ID}} {
		address, err := Address(target)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := Target(address)
		if !ok || got != target {
			t.Fatal(address, got, target)
		}
		args, err := v.manualArguments(target)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Join(args, " ")
		if strings.Contains(text, "19329") {
			t.Fatal("Bot port used as SSH port")
		}
		if target.Authentication != "" && !strings.Contains(text, "-p 2222 -l root") {
			t.Fatal(args)
		}
		if target.Authentication == "key" && !strings.Contains(text, key) {
			t.Fatal("selected key not passed to OpenSSH")
		}
		if strings.Contains(address, key) {
			t.Fatal("native path escaped into connection metadata")
		}
	}
	if _, err := v.identityPath("unknown"); err == nil {
		t.Fatal("unselected identity allowed")
	}
}
func TestManualTargetsRejectCredentialAndOptionInjection(t *testing.T) {
	for _, address := range []string{"ssh://root:password@host:9317", "ssh://host:9317?authentication=password&user=-x&sshPort=22", "ssh://host:9317?authentication=password&user=root&sshPort=22&password=secret", "ssh://host:9317?authentication=password&user=root&sshPort=22&sshPort=2222"} {
		if _, ok := Target(address); ok {
			t.Fatal(address)
		}
	}
}
