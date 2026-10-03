package machines

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestSSHHostInventory(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(filepath.Join(dir, "hosts"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config")
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(config, "Host * !excluded\n  User default\nHost fedora other\nHost=\"quoted\"\nInclude hosts/*.conf # relative to ~/.ssh\nInclude missing.conf\n")
	write(filepath.Join(dir, "hosts", "a.conf"), "hOsT Fedora *.wild ?wild !skip\nHost included\nInclude config\n")
	hosts, err := readSSHHosts(home, config)
	if err != nil || !reflect.DeepEqual(hosts, []string{"fedora", "included", "other", "quoted"}) {
		t.Fatalf("inventory %v %v", hosts, err)
	}
	write(config, "Host=updated\n")
	hosts, err = readSSHHosts(home, config)
	if err != nil || !reflect.DeepEqual(hosts, []string{"updated"}) {
		t.Fatalf("refresh %v %v", hosts, err)
	}
	hosts, err = readSSHHosts(home, filepath.Join(home, "absent"))
	if err != nil || len(hosts) != 0 {
		t.Fatal("missing config must allow new connection", err)
	}
	write(config, "Host would-be-partial\n"+string(make([]byte, 2<<20)))
	if hosts, err = readSSHHosts(home, config); err == nil || len(hosts) != 0 {
		t.Fatal("unbounded config or misleading partial inventory")
	}
}

func TestSSHConfigOverridesFormDefaultsThroughNativeOpenSSH(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ssh"); err != nil {
		t.Skip("native OpenSSH unavailable")
	}
	config := filepath.Join(t.TempDir(), "config")
	// Same Host/HostName catches the previous heuristic that only imported Port
	// after HostName changed. Identity and ProxyJump must remain native settings.
	if err := os.WriteFile(config, []byte("Host fixture\n HostName fixture\n User configured-user\n Port 22022\n IdentityFile /fixture/private-key\n ProxyJump jump.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	in := api.MachineInput{Address: "fixture", Port: 5555, User: "stale-form-user", Authentication: "key", PrivateKey: "/stale/key", SSHConfig: true}
	resolved, jump, err := resolveWithConfig(t.Context(), in, config)
	if err != nil || resolved.User != "configured-user" || resolved.Port != 22022 || jump != "jump.example" || resolved.PrivateKey != "" || resolved.Authentication != "agent" {
		t.Fatalf("native configuration resolution: %+v %s %v", resolved, jump, err)
	}
	// Check the real invocation consumes config identities rather than supplying -i.
	s, err := Open(t.TempDir(), inert{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	args, err := s.sshArgs(profile{View: api.Machine{ID: "machine-test", Address: in.Address, Port: resolved.Port, User: resolved.User, SSHConfig: true, Authentication: "agent"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	args = append([]string{"-G", "-F", config}, args...)
	b, err := exec.CommandContext(t.Context(), "/usr/bin/ssh", args...).Output()
	if err != nil || !contains(b, []byte("identityfile /fixture/private-key\n")) || !contains(b, []byte("proxyjump jump.example\n")) {
		t.Fatal("config identity or route lost in actual SSH arguments", err)
	}
}
