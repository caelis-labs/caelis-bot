package notebooksync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func profile(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "Notebook"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}
func put(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, "Notebook", name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, root, name string) string {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(root, "Notebook", name))
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestRsyncOrdinaryFilesExclusionsConflictCopiesAndNoDeletion(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync unavailable")
	}
	src, dst := profile(t), profile(t)
	put(t, src, "MEMORY.md", "new preferences")
	put(t, dst, "MEMORY.md", "old preferences")
	put(t, src, "2026/10/01/note.md", "user note")
	put(t, src, "assets/file.pdf", "attachment")
	put(t, dst, "removed-at-source.md", "retained user file")
	excluded := []string{"INDEX.md", "HANDOFF.md", "data.db", "memory.sqlite3", "memory.db-wal", "auth.json", "AUTH.JSON", "UPPER.SQLITE", "private.key", ".index-cache", "notes.tmp", "sessions/thread.json", ".caelis-sync-conflicts/old.md"}
	for _, p := range excluded {
		put(t, src, p, "not portable")
	}
	for _, p := range []string{"personal/outside.md", "providers/secret.json", "history/session.json"} {
		full := filepath.Join(src, p)
		_ = os.MkdirAll(filepath.Dir(full), 0700)
		_ = os.WriteFile(full, []byte("outside Notebook"), 0600)
	}
	err := (Rsync{}).Sync(t.Context(), Endpoint{Profile: src}, Endpoint{Profile: dst}, "first", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, dst, "MEMORY.md") != "new preferences" || read(t, dst, "assets/file.pdf") != "attachment" || read(t, dst, "2026/10/01/note.md") != "user note" {
		t.Fatal("ordinary files not copied")
	}
	if read(t, dst, "removed-at-source.md") != "retained user file" || read(t, dst, ".caelis-sync-conflicts/first/MEMORY.md") != "old preferences" {
		t.Fatal("target data lost")
	}
	for _, p := range excluded {
		if _, err := os.Stat(filepath.Join(dst, "Notebook", p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("excluded file transferred: %s", p)
		}
	}
	if err = (Rsync{}).Sync(t.Context(), Endpoint{Profile: src}, Endpoint{Profile: dst}, "retained-final", true, nil); err == nil {
		t.Fatal("retained deleted source note silently reactivated")
	}
	// Editing with the same length and mtime is still detected by --checksum.
	put(t, src, "MEMORY.md", "new preferenceS")
	if err = (Rsync{}).Sync(t.Context(), Endpoint{Profile: src}, Endpoint{Profile: dst}, "second", false, nil); err != nil {
		t.Fatal(err)
	}
	if read(t, dst, ".caelis-sync-conflicts/second/MEMORY.md") != "new preferences" {
		t.Fatal("previous backup missing")
	}
}
func TestFinalHandoffAndStaleTargetAreExplicit(t *testing.T) {
	src, dst := profile(t), profile(t)
	put(t, src, "MEMORY.md", "memory")
	put(t, src, "HANDOFF.md", "unfinished temporary output")
	valid := []byte("<!-- caelis-dream: completed -->\nCompleted context, no replay.\n")
	if err := (Rsync{}).Sync(t.Context(), Endpoint{Profile: src}, Endpoint{Profile: dst}, "final", true, valid); err != nil {
		t.Fatal(err)
	}
	if read(t, dst, "HANDOFF.md") != string(valid) {
		t.Fatal("validated host handoff was not transferred")
	}
	if err := (Rsync{}).Sync(t.Context(), Endpoint{Profile: src}, Endpoint{Profile: dst}, "stale", true, nil); err == nil {
		t.Fatal("stale standby handoff silently accepted")
	}
	if read(t, dst, "HANDOFF.md") != string(valid) {
		t.Fatal("standby handoff altered")
	}
}
func TestUnsafeDirectoriesAndTransportFailures(t *testing.T) {
	src, dst := profile(t), profile(t)
	put(t, src, "MEMORY.md", "memory")
	if err := os.Symlink("/tmp", filepath.Join(dst, "Notebook", "escape")); err != nil {
		t.Fatal(err)
	}
	if err := (Rsync{}).Sync(t.Context(), Endpoint{Profile: src}, Endpoint{Profile: dst}, "unsafe", false, nil); err == nil {
		t.Fatal("receiver symlink accepted")
	}
	dst = profile(t)
	calls := 0
	r := Rsync{Run: func(_ context.Context, b string, args ...string) error {
		calls++
		all := strings.Join(args, " ")
		if strings.Contains(all, "--delete") {
			t.Fatal("delete flag")
		}
		return errors.New("fixture transfer failure")
	}}
	if err := r.Sync(t.Context(), Endpoint{Profile: src}, Endpoint{Profile: dst}, "failed", false, nil); err == nil || calls != 1 {
		t.Fatal("failure not propagated")
	}
}
func TestRemotePathsAreQuotedAndUseProvidedSSH(t *testing.T) {
	src := profile(t)
	put(t, src, "MEMORY.md", "memory")
	remote := Endpoint{Profile: "/srv/APP profile", Target: "existing-node", Shell: []string{"ssh", "-F", "/tmp/existing config", "-o", "BatchMode=yes"}}
	rsyncCalls := 0
	r := Rsync{Version: func(context.Context, string) ([]byte, error) { return []byte("openrsync: protocol version 29"), nil }, Run: func(_ context.Context, b string, args ...string) error {
		if b == "ssh" {
			if args[len(args)-2] != "existing-node" || !strings.Contains(args[len(args)-1], "'/srv/APP profile/Notebook'") {
				t.Fatal("remote inspection not quoted")
			}
			return nil
		}
		rsyncCalls++
		all := strings.Join(args, " ")
		if rsyncCalls == 2 && (!strings.Contains(all, "'ssh' '-F' '/tmp/existing config'") || !strings.Contains(all, "existing-node:'/srv/APP profile/Notebook/'")) {
			t.Fatalf("SSH route or path lost: %s", all)
		}
		return nil
	}}
	if err := r.Sync(t.Context(), Endpoint{Profile: src}, remote, "quoted", false, nil); err != nil || rsyncCalls != 2 {
		t.Fatalf("quoted transfer: %v", err)
	}
	rsyncCalls = 0
	// Remote-to-remote is relayed as two ordinary rsync transfers through private staging.
	remote2 := remote
	remote2.Target = "backup-node"
	r.Run = func(_ context.Context, b string, args ...string) error {
		if b == "rsync" {
			rsyncCalls++
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "existing-node:") && strings.Contains(joined, "backup-node:") {
				t.Fatal("remote-to-remote rsync")
			}
		}
		return nil
	}
	if err := r.Sync(t.Context(), remote, remote2, "remote", false, nil); err != nil || rsyncCalls != 2 {
		t.Fatalf("relay: %v calls=%d", err, rsyncCalls)
	}
}

func TestRemoteArgumentModesPreserveRoutesAndFinalHandoff(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		secluded      bool
	}{
		{"openrsync", "openrsync: protocol version 29", false},
		{"rsync2", "rsync  version 2.6.9  protocol version 29", false},
		{"rsync3", "rsync  version 3.4.1  protocol version 32", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := profile(t)
			put(t, src, "MEMORY.md", "fixture memory")
			remote := Endpoint{Profile: "/srv/APP 'quoted' [literal] profile", Target: "exact-alias", Shell: []string{"ssh", "-o", "BatchMode=yes"}}
			copies, handoffs := 0, 0
			r := Rsync{Version: func(context.Context, string) ([]byte, error) { return []byte(tc.version), nil }, Run: func(_ context.Context, binary string, args ...string) error {
				if binary == "ssh" {
					return nil
				}
				joined := strings.Join(args, " ")
				if strings.Contains(joined, "--delete") || strings.Contains(joined, "--old-args") || strings.Contains(joined, "--trust-sender") {
					t.Fatal("transfer safety weakened")
				}
				last := args[len(args)-1]
				if strings.HasPrefix(last, "exact-alias:") {
					copies++
					hasSecluded := false
					for _, arg := range args {
						hasSecluded = hasSecluded || arg == "-s"
					}
					path := "/srv/APP 'quoted' \\[literal\\] profile/Notebook/"
					if !tc.secluded {
						path = quote(remote.Profile + "/Notebook/")
					}
					if hasSecluded != tc.secluded || last != "exact-alias:"+path || !strings.Contains(joined, "'ssh' '-o' 'BatchMode=yes'") {
						t.Fatalf("wrong mode/route/path: %q", args)
					}
					if strings.Contains(joined, "--ignore-existing") {
						handoffs++
					}
				}
				return nil
			}}
			if err := r.Sync(t.Context(), Endpoint{Profile: src}, remote, "mode-fixture", true, []byte("<!-- caelis-dream: completed -->\nFixture handoff.\n")); err != nil {
				t.Fatal(err)
			}
			if copies != 2 || handoffs != 1 {
				t.Fatalf("remote copy/handoff coverage: %d/%d", copies, handoffs)
			}
		})
	}
}
func TestUnknownRemoteArgumentModeFailsBeforeTransfer(t *testing.T) {
	src := profile(t)
	put(t, src, "MEMORY.md", "fixture")
	for _, failure := range []error{nil, errors.New("fixture probe failure")} {
		calls := 0
		r := Rsync{Version: func(context.Context, string) ([]byte, error) { return []byte("unknown implementation"), failure }, Run: func(context.Context, string, ...string) error { calls++; return nil }}
		if err := r.Sync(t.Context(), Endpoint{Profile: src}, Endpoint{Profile: "/srv/backup", Target: "exact-alias", Shell: []string{"ssh"}}, "unknown", false, nil); err == nil || calls != 0 {
			t.Fatalf("unconfirmed argument mode transferred files: calls=%d err=%v", calls, err)
		}
	}
}

func TestTokenTopicsRemainPortableAndCredentialBasenamesStayExcluded(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync unavailable")
	}
	for _, final := range []bool{false, true} {
		src, dst := profile(t), profile(t)
		put(t, src, "MEMORY.md", "memory")
		ordinary := []string{"token-budget.md", "tokenizer.md", "Tokenization/design.md", "credentials-guide.md"}
		secrets := []string{"token", "tokens.json", "TOKEN.JSON", "credentials.json", "nested/tokens.json"}
		for _, name := range ordinary {
			put(t, src, name, "ordinary note")
		}
		for _, name := range secrets {
			put(t, src, name, "synthetic credential")
		}
		if err := (Rsync{}).Sync(t.Context(), Endpoint{Profile: src}, Endpoint{Profile: dst}, "topic", final, nil); err != nil {
			t.Fatal(err)
		}
		for _, name := range ordinary {
			if read(t, dst, name) != "ordinary note" {
				t.Fatal(name)
			}
		}
		for _, name := range secrets {
			if _, err := os.Stat(filepath.Join(dst, "Notebook", name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("credential transferred", name, err)
			}
		}
	}
}
