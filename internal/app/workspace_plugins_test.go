package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/botworkspace"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestWorkspacePluginReceiptSurvivesReplayAndSettingsChanges(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	req := botworkspace.PluginRequest{ID: "req-1", Action: "install", PluginID: "markdown-work", Version: "1.0.0"}
	status := consumePluginFixture(t, a, root, req)
	if status.State != "saved" || status.ID != req.ID || status.Revision == 0 || len(a.plugins.Selection().SkillRoots) != 1 {
		t.Fatalf("workspace install did not commit: %+v", status)
	}
	if _, err := a.PluginAction(t.Context(), req.PluginID, "uninstall"); err != nil {
		t.Fatal(err)
	}
	revision := a.plugins.Selection().Revision
	replay := consumePluginFixture(t, a, root, req)
	if replay != status || a.plugins.Selection().Revision != revision || len(a.plugins.Selection().SkillRoots) != 0 {
		t.Fatal("duplicate request mutated the original package")
	}
	req.Action, req.Version = "disable", ""
	if changed := consumePluginFixture(t, a, root, req); changed != status || a.plugins.Selection().Revision != revision {
		t.Fatalf("conflicting submission replaced the original outcome: %+v", changed)
	}
}

func TestWorkspacePluginVersionAndActionAreRestricted(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	for _, req := range []botworkspace.PluginRequest{
		{ID: "wrong-version", Action: "install", PluginID: "markdown-work", Version: "99.9.9"},
		{ID: "unsupported-action", Action: "retry", PluginID: "markdown-work"},
		{ID: "unreviewed-id", Action: "install", PluginID: "unreviewed"},
		{ID: "update-unpinned", Action: "update", PluginID: "markdown-work"},
	} {
		if status := consumePluginFixture(t, a, root, req); status.State != "rejected" || status.ID != req.ID || status.Revision != 0 {
			t.Fatalf("invalid request accepted: %+v", status)
		}
	}
	install := consumePluginFixture(t, a, root, botworkspace.PluginRequest{ID: "install", Action: "install", PluginID: "markdown-work"})
	update := consumePluginFixture(t, a, root, botworkspace.PluginRequest{ID: "update-current", Action: "update", PluginID: "markdown-work", Version: "1.0.0"})
	if install.State != "saved" || update.State != "saved" || update.Revision != install.Revision || len(a.plugins.Selection().SkillRoots) != 1 {
		t.Fatal("reviewed install and exact-version no-op update did not preserve projection")
	}
}

func TestWorkspacePluginUnknownClaimIsNeverReplayed(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	req := botworkspace.PluginRequest{ID: "interrupted", Action: "install", PluginID: "markdown-work"}
	body, _ := json.Marshal(req)
	id := sha256.Sum256([]byte(req.ID))
	digest := sha256.Sum256(body)
	file := filepath.Join(a.plugins.Root(), "workspace-requests", hex.EncodeToString(id[:])+".json")
	if err := localstate.WriteConfirmed(file, workspacePluginReceipt{Digest: hex.EncodeToString(digest[:]), workspacePluginStatus: workspacePluginStatus{PluginRequest: req, State: "unknown"}}); err != nil {
		t.Fatal(err)
	}
	status := consumePluginFixture(t, a, root, req)
	if status.State != "unknown" || status.ID != req.ID || len(a.plugins.Selection().SkillRoots) != 0 {
		t.Fatal("interrupted request was replayed")
	}
	other := botworkspace.PluginRequest{ID: "independent", Action: "install", PluginID: "github"}
	if later := consumePluginFixture(t, a, root, other); later.State != "saved" {
		t.Fatalf("unknown outcome prevented independent work: %+v", later)
	}
	dir := filepath.Join(root, "Notebook", ".agents")
	if retained := readPluginFixtureStatus(t, dir, req); retained != status {
		t.Fatalf("later request replaced original status: %+v", retained)
	}
	revision := a.plugins.Selection().Revision
	replay := consumePluginFixture(t, a, root, req)
	req.Action = "uninstall"
	changed := consumePluginFixture(t, a, root, req)
	if replay != status || changed != status || a.plugins.Selection().Revision != revision || len(a.plugins.Selection().SkillRoots) != 0 {
		t.Fatal("reopening the slot lost the original replay fence")
	}
}

func TestWorkspacePluginSymlinkRequestIsIgnored(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	dir := filepath.Join(root, "Notebook", ".agents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(outside, []byte(`{"id":"outside","action":"install","pluginId":"markdown-work"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, botworkspace.PluginRequestName)); err != nil {
		t.Skip("test environment does not support symlinks")
	}
	if err := a.consumeWorkspacePluginRequest(t.Context()); err == nil || len(a.plugins.Selection().SkillRoots) != 0 {
		t.Fatal("symlink request was accepted")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("original outside request was modified", err)
	}
}

func TestWorkspacePluginStatusFailureRetainsConfirmedReceipt(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	dir := filepath.Join(root, "Notebook", ".agents")
	req := botworkspace.PluginRequest{ID: "status-failure", Action: "install", PluginID: "markdown-work"}
	statusPath := filepath.Join(dir, req.StatusName())
	if err := os.MkdirAll(statusPath, 0700); err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(dir, botworkspace.PluginRequestName)
	if err := localstate.WriteConfirmed(requestPath, req); err != nil {
		t.Fatal(err)
	}
	if err := a.consumeWorkspacePluginRequest(t.Context()); err == nil || len(a.plugins.Selection().SkillRoots) != 1 {
		t.Fatal("status failure lost the confirmed package action")
	}
	revision := a.plugins.Selection().Revision
	if _, err := os.Stat(requestPath); err != nil {
		t.Fatal("request was removed before its status was published", err)
	}
	if err := os.Remove(statusPath); err != nil {
		t.Fatal(err)
	}
	if err := a.consumeWorkspacePluginRequest(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(requestPath); !errors.Is(err, os.ErrNotExist) || a.plugins.Selection().Revision != revision {
		t.Fatal("confirmed request was replayed or left pending", err)
	}
}

func TestWorkspacePluginDataStaysOutOfNotebookIndex(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	notes := filepath.Join(root, "Notebook")
	data := filepath.Join(a.plugins.Root(), "data")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(notes, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Join(data, "plugin-note.md"), filepath.Join(notes, "ordinary-note.md")} {
		if err := os.WriteFile(name, []byte("# User note\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(notes, "INDEX.md"))
	if err != nil || strings.Contains(string(body), "plugin-note.md") || !strings.Contains(string(body), "ordinary-note.md") {
		t.Fatal("Notebook index did not isolate plugin data", err)
	}
}

func consumePluginFixture(t *testing.T, a *Application, root string, req botworkspace.PluginRequest) workspacePluginStatus {
	t.Helper()
	dir := filepath.Join(root, "Notebook", ".agents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(dir, botworkspace.PluginRequestName)
	if err := localstate.WriteConfirmed(requestPath, req); err != nil {
		t.Fatal(err)
	}
	if err := a.consumeWorkspacePluginRequest(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(requestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("request slot remained occupied: %v", err)
	}
	return readPluginFixtureStatus(t, dir, req)
}

func readPluginFixtureStatus(t *testing.T, dir string, req botworkspace.PluginRequest) workspacePluginStatus {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, req.StatusName()))
	if err != nil {
		t.Fatal(err)
	}
	var status workspacePluginStatus
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestMalformedWorkspacePluginRequestReleasesSlot(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	dir := filepath.Join(root, "Notebook", ".agents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"{", `{"id":"invalid/id"}`, `{"id":"valid","extra":42}`, strings.Repeat("X", workspacePluginLimit+1)} {
		if err := os.WriteFile(filepath.Join(dir, botworkspace.PluginRequestName), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := a.consumeWorkspacePluginRequest(t.Context()); err == nil {
			t.Fatal("missing malformed request warning")
		}
		files, err := filepath.Glob(filepath.Join(dir, ".bot-plugin-rejected-*"))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, file := range files {
			saved, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			found = found || string(saved) == body
		}
		if !found {
			t.Fatal("original request missing")
		}
	}
	if status := consumePluginFixture(t, a, root, botworkspace.PluginRequest{ID: "after-invalid", Action: "install", PluginID: "markdown-work"}); status.State != "saved" {
		t.Fatal(status)
	}
}
