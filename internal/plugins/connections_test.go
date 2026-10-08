package plugins

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if len(items) != 6 {
		t.Fatalf("want six connectable packages, got %d", len(items))
	}
	for _, item := range items {
		if len(item.Skills) != 0 || len(item.MCPServers) != 1 || item.Connection == nil {
			t.Fatal("invented or missing contribution", item.ID)
		}
		if item.Connection.Kind == "oauth" && !oauthNativeSupported {
			if _, err = m.Mutate(context.Background(), item.ID, "install", nil); err == nil {
				t.Fatal("unsupported OAuth installed")
			}
			continue
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

func TestConnectionReloadCanLaunchNewRevisionFromDisk(t *testing.T) {
	root := t.TempDir()
	m, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	m.secrets = secretstore.Functions{SaveFunc: func(id, v string) error { secrets[id] = v; return nil }, LoadFunc: func(id string) (string, error) {
		value, ok := secrets[id]
		if !ok {
			return "", errors.New("missing")
		}
		return value, nil
	}, DeleteFunc: func(id string) error { delete(secrets, id); return nil }}
	if _, err := m.Mutate(t.Context(), "github", "install", nil); err != nil {
		t.Fatal(err)
	}
	launch := func(selection Selection) error {
		if len(selection.Servers) != 1 {
			return errors.New("new server was not selected")
		}
		fresh, err := Open(root) // The real --plugin-mcp child does not share m.state.
		if err != nil {
			return err
		}
		fresh.secrets = m.secrets
		server := selection.Servers[0]
		return runStdio(t.Context(), fresh, server.PackageID, server.Name, strings.NewReader(""), io.Discard, io.Discard, filepath.Base(server.Root), fmt.Sprint(server.ConnectionRevision))
	}
	applyCount := 0
	apply := func(_ context.Context, selection Selection) error {
		applyCount++
		return launch(selection)
	}
	if _, err := m.ConfigureConnection(t.Context(), "github", "FIRST_SYNTHETIC_TOKEN", "", false, apply); err != nil {
		t.Fatal("first reload could not launch from disk", err)
	}
	first := m.Selection()
	if _, err := m.ConfigureConnection(t.Context(), "github", "SECOND_SYNTHETIC_TOKEN", "", false, apply); err != nil {
		t.Fatal("rotation reload could not launch from disk", err)
	}
	if applyCount != 2 || launch(first) == nil || len(secrets) != 1 {
		t.Fatal("old revision survived successful rotation", applyCount, secrets)
	}
	rejected := m.Selection().Revision + 1
	if _, err := m.ConfigureConnection(t.Context(), "github", "THIRD_SYNTHETIC_TOKEN", "", false, func(_ context.Context, selection Selection) error {
		applyCount++
		if err := launch(selection); err != nil {
			return err
		}
		return errors.New("runtime rejected reload")
	}); err == nil {
		t.Fatal("failed reload reported success")
	}
	if applyCount != 3 || m.Selection().Revision != rejected-1 || len(secrets) != 1 || secrets[secretKey(root, "github", rejected)] != "" {
		t.Fatal("failed revision or secret remained active", applyCount, secrets)
	}
	restored, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Selection().Revision != m.Selection().Revision || launch(m.Selection()) != nil {
		t.Fatal("confirmed revision was not restored on disk", err)
	}
	if _, err := m.ConfigureConnection(t.Context(), "github", "THIRD_SYNTHETIC_TOKEN", "", false, apply); err != nil || applyCount != 4 {
		t.Fatal("same credential could not retry after a rejected reload", err, applyCount)
	}
}

func TestConnectionFailuresRemoveNewSecretsAndKeepConfirmedCredential(t *testing.T) {
	root := t.TempDir()
	m, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	failCA := false
	m.secrets = secretstore.Functions{SaveFunc: func(id, value string) error {
		secrets[id] = value // Exercise a store that wrote before reporting failure.
		if failCA && strings.HasSuffix(id, "-ca") {
			return errors.New("CA write failed")
		}
		return nil
	}, LoadFunc: func(id string) (string, error) {
		value, ok := secrets[id]
		if !ok {
			return "", errors.New("missing")
		}
		return value, nil
	}, DeleteFunc: func(id string) error { delete(secrets, id); return nil }}
	if _, err := m.Mutate(t.Context(), "obsidian", "install", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConfigureConnection(t.Context(), "obsidian", "FIRST_SYNTHETIC_TOKEN", "", false, func(context.Context, Selection) error { return errors.New("runtime busy") }); err == nil || len(secrets) != 0 {
		t.Fatal("first rejected connection left a secret", err, secrets)
	}
	if _, err := m.ConfigureConnection(t.Context(), "obsidian", "", "", true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Mutate(t.Context(), "obsidian", "uninstall", nil); err != nil || len(secrets) != 0 {
		t.Fatal("clear/uninstall retained a rejected secret", err, secrets)
	}
	if _, err := m.Mutate(t.Context(), "obsidian", "install", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConfigureConnection(t.Context(), "obsidian", "FIRST_SYNTHETIC_TOKEN", "", false, nil); err != nil {
		t.Fatal(err)
	}
	confirmed := m.Selection().Servers[0].ConnectionRevision
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	failCA = true
	if _, err := m.ConfigureConnection(t.Context(), "obsidian", "SECOND_SYNTHETIC_TOKEN", ca, false, nil); err == nil {
		t.Fatal("failed CA write reported success")
	}
	if len(secrets) != 1 || secrets[secretKey(root, "obsidian", confirmed)] != "FIRST_SYNTHETIC_TOKEN" || m.Selection().Servers[0].ConnectionRevision != confirmed {
		t.Fatal("failed CA write lost confirmed credential or left new keys", secrets)
	}
}

func TestConnectionStateWriteFailureCleansNewSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode is not a Windows write barrier")
	}
	root := t.TempDir()
	m, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	m.secrets = secretstore.Functions{SaveFunc: func(id, value string) error { secrets[id] = value; return nil }, LoadFunc: func(id string) (string, error) { return secrets[id], nil }, DeleteFunc: func(id string) error { delete(secrets, id); return nil }}
	if _, err := m.Mutate(t.Context(), "github", "install", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0700)
	applyCount := 0
	_, err = m.ConfigureConnection(t.Context(), "github", "SYNTHETIC_PRIVATE_TOKEN", "", false, func(context.Context, Selection) error { applyCount++; return nil })
	if err == nil || applyCount != 0 || len(secrets) != 0 || m.state.Connections["github"].Configured {
		t.Fatal("failed state write published a connection or retained a secret", err, applyCount, secrets)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil || reopened.state.Connections["github"].Configured {
		t.Fatal("failed state write changed confirmed disk state", err)
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
