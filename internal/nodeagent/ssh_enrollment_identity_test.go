package nodeagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHEnrollmentIdentityProbeValidatesClosedPrivateMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		valid        bool
	}{
		{"existing", "/home/fixture/.local/share/caelis-bot/node-agent\nexisting\n1\n1\n{\"id\":\"node-UPPERCASE\"}", true},
		{"empty slot", "/home/fixture/.local/share/caelis-bot/node-agent\nmissing\n0\n0\n{}", true},
		{"missing identity", "/home/fixture/node\nexisting\n1\n1\n{}", false},
		{"unexpected metadata", "/home/fixture/node\nexisting\n1\n1\n{\"id\":\"node-A\",\"token\":\"untrusted\"}", false},
		{"local alias", "/home/fixture/node\nexisting\n1\n1\n{\"id\":\"local\"}", false},
		{"oversized output", strings.Repeat("a", 5000), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			script := filepath.Join(directory, "ssh")
			trace := filepath.Join(directory, "trace")
			body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(trace) + "\nprintf '%s' " + shellQuote(tc.output) + "\n"
			if err := os.WriteFile(script, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			_, err := ProbeSSHEnrollmentIdentity(t.Context(), SSHConfig{Binary: script, Target: "fixture"})
			if (err == nil) != tc.valid {
				t.Fatal(tc.name, err)
			}
			args, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{"StrictHostKeyChecking=yes", "ClearAllForwardings=yes", "stat -c %u", "stat -c %a", "test ! -L", "node.json"} {
				if !strings.Contains(string(args), required) {
					t.Fatal("missing private probe fence", required)
				}
			}
			for _, mutation := range []string{"mkdir", "chmod", "mv -f", "serve-bot", "rm -"} {
				if strings.Contains(string(args), mutation) {
					t.Fatal("identity probe mutated", mutation)
				}
			}
		})
	}
}
