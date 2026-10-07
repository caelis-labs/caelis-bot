package caelis

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

func TestPublicConfigurationPatchCanExplicitlyClearPluginSelection(t *testing.T) {
	fields := []string{"mcp_servers", "skill_dirs", "skill_roots"}
	for _, tc := range []struct {
		name  string
		patch wire.ApplicationConfigurationPatch
		want  bool
	}{
		{name: "nil_omits", patch: wire.ApplicationConfigurationPatch{}},
		{name: "empty_clears", patch: wire.ApplicationConfigurationPatch{McpServers: []wire.ApplicationMCPServer{}, SkillDirs: []string{}, SkillRoots: []string{}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.patch)
			if err != nil {
				t.Fatal(err)
			}
			var encoded map[string]json.RawMessage
			if err := json.Unmarshal(body, &encoded); err != nil {
				t.Fatal(err)
			}
			for _, name := range fields {
				value, exists := encoded[name]
				if exists != tc.want || tc.want && string(value) != "[]" {
					t.Fatalf("%s: %s must %v, got %s", tc.name, name, tc.want, body)
				}
			}
		})
	}
}

func TestPluginTransactionHoldsCoreTurnAdmission(t *testing.T) {
	s := New(Options{Directory: t.TempDir()})
	entered, release, transactionDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		_ = s.WithBotPluginAdmission(func(func(context.Context, plugins.Selection) error) error {
			close(entered)
			<-release
			return nil
		})
		close(transactionDone)
	}()
	<-entered
	attempted, submitted := make(chan struct{}), make(chan struct{})
	go func() {
		close(attempted)
		_, _ = s.Submit(context.Background(), api.Submission{ID: "plugin-gate-check", Text: "fixture"}, nil)
		close(submitted)
	}()
	<-attempted
	select {
	case <-submitted:
		close(release)
		t.Fatal("new Core turn entered before the plugin transaction committed")
	case <-time.After(40 * time.Millisecond):
	}
	close(release)
	select {
	case <-transactionDone:
	case <-time.After(2 * time.Second):
		t.Fatal("plugin transaction did not release admission")
	}
	select {
	case <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("Core turn did not resume after plugin transaction")
	}
}

func TestPublicCorePluginProjectionKeepsPortableEnvironmentPrivate(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data", "sample")
	selection := plugins.Selection{Revision: 2, SkillRoots: []string{filepath.Join(root, "skills", "useful")}, Servers: []plugins.SelectedServer{
		{PackageID: "sample", Name: "local-validator", Root: filepath.Join(root, "versions", "sample", "v1"), Data: data, Server: plugins.Server{Type: "stdio", Command: "./bin/check", Env: map[string]string{"RULES": "${PLUGIN_ROOT}/rules.json"}}},
		{PackageID: "sample", Name: "remote", Root: root, Data: data, Server: plugins.Server{Type: "streamable-http", URL: "https://example.com/mcp", Headers: map[string]string{"X-Tenant": "public"}}},
	}}
	servers, skills, issues := corePlugins(selection, "/Applications/CaelisBotDev.app/Contents/MacOS/CaelisBot", []string{filepath.Join(root, "built-in")})
	if len(servers) != 1 || len(skills) != 2 || len(issues) != 1 || issues[0].Component != "server" {
		t.Fatal(servers, skills, issues)
	}
	if servers[0].Name != plugins.RuntimeName("sample", "local-validator") || *servers[0].Command != "/Applications/CaelisBotDev.app/Contents/MacOS/CaelisBot" || len(servers[0].Args) != 5 || servers[0].Args[0] != "--plugin-mcp" || servers[0].Args[4] != "v1" {
		t.Fatal(servers[0])
	}
	for _, arg := range servers[0].Args {
		if arg == "RULES" || arg == "${PLUGIN_ROOT}/rules.json" {
			t.Fatal("environment reached Core profile")
		}
	}
}
