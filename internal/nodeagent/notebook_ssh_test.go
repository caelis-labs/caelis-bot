package nodeagent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
)

func TestNotebookProxyJumpRejectsCommandsAndPreservesJoinBoundary(t *testing.T) {
	base := []byte("hostname notebook.invalid\nuser fixture\nport 22\nuserknownhostsfile /dev/null\nglobalknownhostsfile /dev/null\n")
	for _, value := range []string{"private-hop", "user@private-hop:2222", "first-hop,user@second-hop:22", "user@[2001:db8::1]:2222", "ssh://user@private-hop:2222", "none"} {
		effective := append(append([]byte{}, base...), []byte("proxyjump "+value+"\n")...)
		if _, err := sanitizedSSHConfiguration(effective, true); err != nil {
			t.Fatal("ordinary jump rejected", value, err)
		}
		if value != "none" {
			if _, err := SanitizedSSHConfiguration(effective); err == nil {
				t.Fatal("reverse join boundary changed")
			}
		}
	}
	for _, line := range []string{"proxyjump hop;touch /tmp/untrusted", "proxyjump $(command)", "proxyjump `command`", "proxyjump hop -oProxyCommand=command", "proxyjump user@hop:0", "proxyjump user@hop:65536", "proxyjump [invalid::address]", "proxyjump hop,,other", "proxyjump ssh://user:password@hop", "proxycommand arbitrary-command", "knownhostscommand arbitrary-command", "setenv PRIVATE=untrusted"} {
		if _, err := sanitizedSSHConfiguration(append(append([]byte{}, base...), []byte(line+"\n")...), true); err == nil {
			t.Fatal("command or invalid route accepted", line)
		}
	}
}

// OpenSSH actually parses a contained multi-Host config with an existing jump
// alias. Only the transport leg is a local script: rsync really copies fixture
// bytes, with no network, native credential, or user SSH configuration access.
func TestNotebookSSHProxyJumpUsesOriginalAliasForRsync(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH unavailable")
	}
	rsync, err := exec.LookPath("rsync")
	if err != nil {
		t.Skip("rsync unavailable")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "ssh-config")
	contents := "Host fixture-notebook\n HostName notebook.invalid\n User fixture\n ProxyJump fixture-hop\nHost fixture-hop\n HostName jump.invalid\n User jump-user\n Port 2222\nHost *\n UserKnownHostsFile /dev/null\n GlobalKnownHostsFile /dev/null\n IdentityFile none\n"
	if err = os.WriteFile(config, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(root, "ssh-invocation")
	transport := filepath.Join(root, "ssh-fixture")
	script := "#!/bin/sh\nif [ \"$1\" = -G ]; then exec " + shellQuote(ssh) + " -F " + shellQuote(config) + " \"$@\"; fi\nprintf '%s\\n' \"$@\" > " + shellQuote(trace) + "\nwhile [ \"$#\" -gt 0 ]; do case \"$1\" in -o) shift 2;; -T) shift;; --) shift; break;; *) break;; esac; done\n[ \"$1\" = fixture-notebook ] || exit 2\nshift\nexec /bin/sh -c \"$*\"\n"
	if err = os.WriteFile(transport, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	shell, close, err := NotebookSSH(t.Context(), SSHConfig{Binary: transport, Target: "fixture-notebook"})
	if err != nil {
		t.Fatal("actual OpenSSH ProxyJump parse", err)
	}
	defer close()
	for _, arg := range shell {
		if arg == "-F" {
			t.Fatal("Notebook replaced native alias namespace")
		}
	}
	source, target := filepath.Join(root, "source profile"), filepath.Join(root, "target 'quoted' [literal] profile")
	for _, profile := range []string{source, target} {
		if err = os.MkdirAll(filepath.Join(profile, "Notebook"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// A glob-like path must not select this unrelated sibling during final readback.
	unrelated := filepath.Join(root, "target 'quoted' l profile", "Notebook")
	if err = os.MkdirAll(unrelated, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(unrelated, "must-not-copy.md"), []byte("unrelated fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"MEMORY.md": "fixture ordinary memory\n", "note.md": "fixture portable bytes\n"} {
		if err = os.WriteFile(filepath.Join(source, "Notebook", name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(source, "Notebook", "private.sqlite"), []byte("excluded synthetic bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	var remoteCopy bool
	runner := func(ctx context.Context, binary string, args ...string) error {
		if binary == rsync {
			for i, arg := range args {
				if arg == "-e" {
					remoteCopy = true
					if i+1 >= len(args) || !strings.Contains(args[i+1], "ClearAllForwardings=yes") || strings.Contains(args[i+1], "'-F'") {
						t.Fatal("rsync changed approved shell route")
					}
				}
			}
		}
		cmd := exec.CommandContext(ctx, binary, args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("contained fixture command failed: %w: %s", err, output)
		}
		return nil
	}
	if err = (notebooksync.Rsync{Binary: rsync, Run: runner}).Sync(t.Context(), notebooksync.Endpoint{Profile: source}, notebooksync.Endpoint{Profile: target, Target: "fixture-notebook", Shell: shell}, "proxyjump-fixture", false, nil); err != nil {
		t.Fatal("actual contained rsync", err)
	}
	b, err := os.ReadFile(filepath.Join(target, "Notebook", "note.md"))
	if err != nil || string(b) != "fixture portable bytes\n" || !remoteCopy {
		t.Fatal("rsync fixture bytes missing", err)
	}
	if _, err = os.Stat(filepath.Join(target, "Notebook", "private.sqlite")); !os.IsNotExist(err) {
		t.Fatal("nonportable bytes copied")
	}
	invocation, err := os.ReadFile(trace)
	if err != nil || !strings.Contains(string(invocation), "fixture-notebook\nrsync\n--server\n") {
		t.Fatal("rsync did not use original enrolled alias", err)
	}

	// The final host-confirmed handoff uses the same filename mode as backups.
	handoff := []byte("<!-- caelis-dream: completed -->\nContained fixture handoff.\n")
	if err = (notebooksync.Rsync{Binary: rsync, Run: runner}).Sync(t.Context(), notebooksync.Endpoint{Profile: source}, notebooksync.Endpoint{Profile: target, Target: "fixture-notebook", Shell: shell}, "proxyjump-final-fixture", true, handoff); err != nil {
		t.Fatal("actual contained final handoff", err)
	}
	b, err = os.ReadFile(filepath.Join(target, "Notebook", "HANDOFF.md"))
	if err != nil || string(b) != string(handoff) {
		t.Fatal("final handoff bytes missing", err)
	}

	// Check the hop's own independent native Host block, without connecting.
	hop, err := exec.CommandContext(t.Context(), ssh, "-F", config, "-T", "-G", "fixture-hop").Output()
	if err != nil || !strings.Contains(string(hop), "hostname jump.invalid\n") || !strings.Contains(string(hop), "port 2222\n") {
		t.Fatal("native jump alias resolution changed", err)
	}
}
