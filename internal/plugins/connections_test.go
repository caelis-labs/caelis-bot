package plugins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/secretstore"
)

func TestConnectableReviewedPackagesAndLegacyMigration(t *testing.T) {
	m, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	items := m.Snapshot().Items
	if len(items) != 5 {
		t.Fatalf("want five connectable packages, got %d", len(items))
	}
	for _, item := range items {
		if len(item.Skills) != 0 || len(item.MCPServers) != 1 || item.Connection == nil {
			t.Fatal("invented or missing contribution", item.ID)
		}
		if _, err = m.Mutate(context.Background(), item.ID, "install", nil); err != nil {
			t.Fatal(item.ID, err)
		}
		e, _ := m.entry(item.ID)
		p, err := m.readInstalled(e)
		if err != nil || len(p.Servers) != 1 || len(p.Skills) != 0 {
			t.Fatal(item.ID, err, p.Issues)
		}
	}
	if _, err = m.Mutate(context.Background(), "markdown-work", "install", nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(m.root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Items[0].ID != "markdown-work" || len(reopened.Selection().SkillRoots) != 1 {
		t.Fatal("legacy installed Skill lost")
	}
}

func TestBundledBraveRealStdioToolDirectory(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node runtime unavailable")
	}
	m, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	m.secrets = secretstore.Functions{SaveFunc: func(id, v string) error { secrets[id] = v; return nil }, LoadFunc: func(id string) (string, error) { return secrets[id], nil }, DeleteFunc: func(id string) error { delete(secrets, id); return nil }}
	if _, err = m.Mutate(t.Context(), "brave-search", "install", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = m.ConfigureConnection(t.Context(), "brave-search", "SYNTHETIC_PRIVATE_TOKEN", "", false, nil); err != nil {
		t.Fatal(err)
	}
	e, _ := m.entry("brave-search")
	p, err := m.readInstalled(e)
	if err != nil {
		t.Fatal(err)
	}
	rev := m.state.Connections["brave-search"].Revision
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}` + "\n" + `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n"
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	var out, diagnostics bytes.Buffer
	err = runStdio(ctx, m, "brave-search", "brave-search", strings.NewReader(input), &out, &diagnostics, filepath.Base(p.Root), fmt.Sprint(rev))
	if err != nil {
		t.Fatalf("official Brave stdio did not exit: %v, diagnostic=%s", err, diagnostics.String())
	}
	if !strings.Contains(out.String(), `"tools"`) || !strings.Contains(out.String(), "brave_web_search") || strings.Contains(out.String(), "SYNTHETIC_PRIVATE_TOKEN") {
		t.Fatalf("official Brave tools/list failed: %s", out.String())
	}
}

func TestConnectionRevisionPrivateStoreActivationAndClear(t *testing.T) {
	root := t.TempDir()
	m, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	m.secrets = secretstore.Functions{SaveFunc: func(id, v string) error { secrets[id] = v; return nil }, LoadFunc: func(id string) (string, error) {
		v := secrets[id]
		if v == "" {
			return "", errors.New("missing")
		}
		return v, nil
	}, DeleteFunc: func(id string) error { delete(secrets, id); return nil }}
	if _, err = m.Mutate(context.Background(), "github", "install", nil); err != nil {
		t.Fatal(err)
	}
	if len(m.Selection().Servers) != 0 {
		t.Fatal("unconfigured service activated")
	}
	var selected Selection
	apply := func(_ context.Context, s Selection) error { selected = s.Clone(); return nil }
	if _, err = m.ConfigureConnection(context.Background(), "github", "SYNTHETIC_PRIVATE_TOKEN", "", false, apply); err != nil {
		t.Fatal(err)
	}
	if len(selected.Servers) != 1 || selected.Servers[0].ConnectionRevision == 0 || selected.Servers[0].Connection == nil {
		t.Fatal("credential revision not projected", selected)
	}
	stable := m.Snapshot().Revision
	if _, err = m.ConfigureConnection(context.Background(), "github", "SYNTHETIC_PRIVATE_TOKEN", "", false, func(context.Context, Selection) error { t.Fatal("duplicate credential reapplied Runtime"); return nil }); err != nil || m.Snapshot().Revision != stable {
		t.Fatal("duplicate credential advanced revision", err)
	}
	state, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), "SYNTHETIC_PRIVATE_TOKEN") || strings.Contains(fmt.Sprint(m.Snapshot().Public()), "SYNTHETIC_PRIVATE_TOKEN") {
		t.Fatal("credential leaked to state or snapshot")
	}
	if _, err = m.ConfigureConnection(context.Background(), "github", "", "", true, apply); err != nil {
		t.Fatal(err)
	}
	if len(selected.Servers) != 0 || len(secrets) != 0 || m.Snapshot().Items[1].Connection.State != "not_configured" {
		t.Fatal("clear left capability or credential")
	}
	if _, err = m.ConfigureConnection(context.Background(), "github", "bad\nheader", "", false, apply); err == nil {
		t.Fatal("control character accepted")
	}
	if _, err = m.ConfigureConnection(context.Background(), "github", "SYNTHETIC_PRIVATE_TOKEN", "", false, func(context.Context, Selection) error { return errors.New("runtime rejected") }); err == nil {
		t.Fatal("rejected activation committed")
	}
	if m.Snapshot().Items[1].Connection.State != "not_configured" || len(m.Selection().Servers) != 0 {
		t.Fatal("rejected activation leaked")
	}
	if _, err = m.ConfigureConnection(context.Background(), "github", "FIRST_SYNTHETIC_TOKEN", "", false, apply); err != nil {
		t.Fatal(err)
	}
	confirmed := m.Selection().Servers[0].ConnectionRevision
	if _, err = m.ConfigureConnection(context.Background(), "github", "SECOND_SYNTHETIC_TOKEN", "", false, func(context.Context, Selection) error { return errors.New("runtime rejected") }); err == nil {
		t.Fatal("rejected edit appeared successful")
	}
	if m.Selection().Servers[0].ConnectionRevision != confirmed || secrets[secretKey(root, "github", confirmed)] != "FIRST_SYNTHETIC_TOKEN" {
		t.Fatal("rejected edit lost confirmed credential")
	}
	if _, err = m.ConfigureConnection(context.Background(), "github", "SECOND_SYNTHETIC_TOKEN", "", false, apply); err != nil {
		t.Fatal(err)
	}
	if m.Selection().Servers[0].ConnectionRevision == confirmed || len(secrets) != 1 {
		t.Fatal("retry did not replace and revoke confirmed credential")
	}
}

func TestStaleRuntimePluginLauncherCannotRestartDisabledOrOldConnection(t *testing.T) {
	m, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	m.secrets = secretstore.Functions{SaveFunc: func(id, v string) error { secrets[id] = v; return nil }, LoadFunc: func(id string) (string, error) { return secrets[id], nil }, DeleteFunc: func(id string) error { delete(secrets, id); return nil }}
	if _, err = m.Mutate(t.Context(), "github", "install", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = m.ConfigureConnection(t.Context(), "github", "FIRST_SYNTHETIC_TOKEN", "", false, nil); err != nil {
		t.Fatal(err)
	}
	first := m.Selection().Servers[0]
	if _, err = m.ConfigureConnection(t.Context(), "github", "SECOND_SYNTHETIC_TOKEN", "", false, nil); err != nil {
		t.Fatal(err)
	}
	if err = runStdio(t.Context(), m, "github", "github", strings.NewReader(""), io.Discard, io.Discard, filepath.Base(first.Root), fmt.Sprint(first.ConnectionRevision)); err == nil || !strings.Contains(err.Error(), "no longer active") {
		t.Fatal("old credential revision launched", err)
	}
	if _, err = m.Mutate(t.Context(), "github", "disable", nil); err != nil {
		t.Fatal(err)
	}
	current := m.state.Connections["github"]
	if err = runStdio(t.Context(), m, "github", "github", strings.NewReader(""), io.Discard, io.Discard, filepath.Base(first.Root), fmt.Sprint(current.Revision)); err == nil || !strings.Contains(err.Error(), "no longer active") {
		t.Fatal("disabled package launched", err)
	}
}
