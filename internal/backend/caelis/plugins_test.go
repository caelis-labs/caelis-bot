package caelis

import (
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

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
