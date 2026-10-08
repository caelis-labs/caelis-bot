package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewedInstallActivationRecoveryAndRollback(t *testing.T) {
	root := t.TempDir()
	m, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if items := m.Snapshot().Items; len(items) != 6 || items[0].Installed {
		t.Fatal(items)
	}
	var applied []Selection
	apply := func(_ context.Context, s Selection) error { applied = append(applied, s.Clone()); return nil }
	ctx := context.Background()
	if _, err = m.Mutate(ctx, "markdown-work", "install", apply); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || len(applied[0].SkillRoots) != 1 || len(applied[0].Servers) != 0 {
		t.Fatal(applied)
	}
	if _, err = m.Mutate(ctx, "markdown-work", "install", apply); err != nil || len(applied) != 1 {
		t.Fatal("duplicate installation reapplied", err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Selection().SkillRoots) != 1 {
		t.Fatal("selection did not recover")
	}
	if _, err = reopened.Mutate(ctx, "markdown-work", "disable", apply); err != nil {
		t.Fatal(err)
	}
	if len(applied[1].SkillRoots) != 0 {
		t.Fatal("disable still projected Skill")
	}
	if _, err = reopened.Mutate(ctx, "markdown-work", "enable", func(context.Context, Selection) error { return errors.New("runtime rejected") }); err == nil {
		t.Fatal("activation rejection ignored")
	}
	if reopened.Snapshot().Items[0].Enabled {
		t.Fatal("failed activation committed")
	}
	if _, err = reopened.Mutate(ctx, "markdown-work", "enable", apply); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Mutate(ctx, "markdown-work", "uninstall", apply); err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Items[0].Installed || len(reopened.Selection().SkillRoots) != 0 {
		t.Fatal("uninstall remained active")
	}
	if _, err = reopened.Mutate(ctx, "markdown-work", "uninstall", apply); err != nil {
		t.Fatal(err)
	}
}

func TestActivationPersistenceFailureUsesFreshRollbackContext(t *testing.T) {
	root := t.TempDir()
	m, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	// The adapter can complete before the caller's request is cancelled. A
	// failed state write must still restore the confirmed selection.
	if err := os.Mkdir(filepath.Join(root, "state.json"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls []Selection
	rollbackCtxCanceled := false
	apply := func(callCtx context.Context, selected Selection) error {
		calls = append(calls, selected.Clone())
		if len(calls) == 1 {
			cancel()
		} else if callCtx.Err() != nil {
			rollbackCtxCanceled = true
			return errors.New("rollback inherited cancelled request")
		}
		return nil
	}
	_, err = m.Mutate(ctx, "markdown-work", "install", apply)
	if err == nil || rollbackCtxCanceled {
		t.Fatal("persistence failure did not use a fresh rollback context", err)
	}
	if len(calls) != 2 || len(calls[0].SkillRoots) != 1 || len(calls[1].SkillRoots) != 0 || m.Snapshot().Items[0].Installed {
		t.Fatal("failed persistence retained the new selection", calls)
	}
}

func TestVerifiedBytesAndPathSafety(t *testing.T) {
	m, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Mutate(context.Background(), "markdown-work", "install", nil); err != nil {
		t.Fatal(err)
	}
	e, _ := m.entry("markdown-work")
	root := m.packageRoot(e)
	if err := os.WriteFile(filepath.Join(root, "extra.sh"), []byte("exit 0"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.readInstalled(e); err == nil {
		t.Fatal("unreviewed executable accepted")
	}
	if err := os.Remove(filepath.Join(root, "extra.sh")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "plugin.json")
	if err := os.WriteFile(path, []byte(`{"name":"changed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.readInstalled(e); err == nil {
		t.Fatal("modified manifest accepted")
	}
	if !safeReviewedName("skills/markdown-work/SKILL.md") || !safeRelative(filepath.FromSlash("skills/markdown-work/SKILL.md")) {
		t.Fatal("portable reviewed inventory did not resolve to a native relative path")
	}
	if safeRelative("../escape") || safeRelative("/absolute") || safeReviewedName("skills\\escape") {
		t.Fatal("unsafe path accepted")
	}
}

func TestCorruptPackageReinstallKeepsOldVersionPath(t *testing.T) {
	m, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := m.Mutate(ctx, "markdown-work", "install", nil); err != nil {
		t.Fatal(err)
	}
	oldRoot := m.Selection().SkillRoots[0]
	if err := os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(oldRoot)), "unexpected.txt"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Mutate(ctx, "markdown-work", "install", nil); err == nil {
		t.Fatal("duplicate install falsely confirmed a corrupt package")
	}
	if _, err := m.Mutate(ctx, "markdown-work", "uninstall", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Mutate(ctx, "markdown-work", "install", nil); err != nil {
		t.Fatal("reviewed package could not be reinstalled", err)
	}
	newRoot := m.Selection().SkillRoots[0]
	if newRoot == oldRoot {
		t.Fatal("reinstall replaced a path held by an old Runtime snapshot")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(oldRoot)), "unexpected.txt")); err != nil {
		t.Fatal("old snapshot path was removed before drain", err)
	}
	if reopened, err := Open(m.Root()); err != nil || len(reopened.Selection().SkillRoots) != 1 || reopened.Selection().SkillRoots[0] != newRoot {
		t.Fatal("reinstalled generation did not recover", err)
	}
}

func TestPrivateStoreRejectsSymlinkedDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Plugins")
	outside := t.TempDir()
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("symlinked plugin store accepted")
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	m, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "versions")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Mutate(context.Background(), "markdown-work", "install", nil); err == nil {
		t.Fatal("symlinked version directory accepted")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatal("plugin wrote outside its private store", err, entries)
	}
}

func fixturePackage(t *testing.T, skill, mcp bool) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, value string) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("plugin.json", `{"$schema":"`+manifestSchema+`","name":"fixture","version":"0.1.0","description":"fixture"}`)
	if skill {
		write("skills/useful/SKILL.md", "---\nname: useful\ndescription: Useful fixture\n---\n\nUse this only on request.\n")
		write("skills/useful/references/readme.md", "fixture resource")
	}
	if mcp {
		write("mcp.json", `{"$schema":"`+mcpSchema+`","mcpServers":{"local-validator":{"type":"stdio","command":"./bin/check","args":["${PLUGIN_ROOT}/rules.json","${PLUGIN_DATA}/cache"],"env":{"RULES":"${PLUGIN_ROOT}/rules.json"},"cwd":"${PLUGIN_ROOT}"},"remote":{"type":"streamable-http","url":"https://example.com/mcp","headers":{"X-Tenant":"public"}}}}`)
		write("bin/check", "#!/bin/sh\nexit 0\n")
		write("rules.json", "{}")
	}
	return root
}

func TestPortableComponentsAndResources(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		skill, mcp              bool
		wantSkills, wantServers int
	}{{"skills", true, false, 1, 0}, {"mcp", false, true, 0, 2}, {"combined", true, true, 1, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixturePackage(t, tc.skill, tc.mcp)
			p, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Skills) != tc.wantSkills || len(p.Servers) != tc.wantServers {
				t.Fatal(p)
			}
			if tc.mcp {
				data := filepath.Join(t.TempDir(), "state")
				s, err := p.Servers[0].Resolve(root, data)
				if err != nil {
					t.Fatal(err)
				}
				if s.Command != filepath.Join(root, "bin/check") || s.Env["PLUGIN_ROOT"] != root || s.Env["PLUGIN_DATA"] != data || !strings.HasPrefix(s.Args[0], root) {
					t.Fatal(s)
				}
			}
			files := map[string]string{}
			_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				body, _ := os.ReadFile(path)
				sum := sha256.Sum256(body)
				rel, _ := filepath.Rel(root, path)
				files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
				return nil
			})
			if err := Verify(root, files); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMalformedServerDoesNotHideHealthySkillOrServer(t *testing.T) {
	root := fixturePackage(t, true, true)
	path := filepath.Join(root, "mcp.json")
	var doc map[string]any
	body, _ := os.ReadFile(path)
	_ = json.Unmarshal(body, &doc)
	servers := doc["mcpServers"].(map[string]any)
	servers["bad"] = map[string]any{"type": "stdio", "command": "../outside"}
	body, _ = json.Marshal(doc)
	_ = os.WriteFile(path, body, 0600)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Skills) != 1 || len(p.Servers) != 2 || len(p.Issues) != 1 || p.Issues[0].Name != "bad" {
		t.Fatal(p)
	}
}
