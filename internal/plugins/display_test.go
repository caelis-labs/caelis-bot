package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

func displayFixture(files map[string]string, details DisplayCatalog) (fstest.MapFS, Entry) {
	fsys := fstest.MapFS{}
	entry := Entry{ID: "fixture", Version: "1.0.0", Source: "https://catalog.example/fixture", Files: map[string]string{}, Display: details}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
		sum := sha256.Sum256([]byte(body))
		entry.Files[name] = hex.EncodeToString(sum[:])
	}
	return fsys, entry
}

func TestReviewedDisplayShowsActualPortableContributionsWithoutConfiguration(t *testing.T) {
	manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"fixture","version":"1.0.0","author":{"name":"Third Party","url":"https://publisher.example"},"repository":"https://code.example/fixture"}`
	skill := "---\nname: report-work\ndescription: Draft and edit structured reports.\n---\nBody\n"
	mcp := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"notes-api":{"type":"streamable-http","url":"https://private.example/mcp","headers":{"Authorization":"SYNTHETIC_PRIVATE_TOKEN"}},"local-index":{"type":"stdio","command":"./bin/index","env":{"PRIVATE":"SYNTHETIC_PRIVATE_TOKEN"}}}}`
	cases := []struct {
		name          string
		files         map[string]string
		wantSkills    int
		wantServers   int
		wantPublisher string
	}{
		{"skill-only", map[string]string{"plugin.json": manifest, "skills/report-work/SKILL.md": skill}, 1, 0, "Third Party"},
		{"mcp-only", map[string]string{"plugin.json": manifest, "mcp.json": mcp}, 0, 2, "Third Party"},
		{"mixed", map[string]string{"plugin.json": manifest, "skills/report-work/SKILL.md": skill, "mcp.json": mcp}, 1, 2, "Third Party"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, entry := displayFixture(tc.files, DisplayCatalog{Skills: map[string]DisplayContribution{"report-work": {NameZh: "报告写作", DescriptionZh: "起草和修订报告"}}, Servers: map[string]DisplayContribution{"notes-api": {DescriptionEn: "Find notes"}, "local-index": {DescriptionEn: "Search local notes"}, "not-in-mcp": {DescriptionEn: "Never show"}}})
			detail, err := reviewedDisplay(files, entry)
			if err != nil || detail.Publisher != tc.wantPublisher || detail.PublisherURL != "https://publisher.example" || detail.SourceURL != "https://code.example/fixture" || len(detail.Skills) != tc.wantSkills || len(detail.Servers) != tc.wantServers {
				t.Fatal(detail, err)
			}
			if tc.wantSkills > 0 && (detail.Skills[0].ID != "report-work" || detail.Skills[0].Description != "Draft and edit structured reports." || detail.Skills[0].NameZh != "报告写作") {
				t.Fatal("Skill metadata did not come from the reviewed SKILL.md", detail.Skills)
			}
			if tc.wantServers > 0 && (detail.Servers[0].ID != "local-index" || detail.Servers[1].ID != "notes-api" || detail.Servers[1].Description != "Find notes") {
				t.Fatal("MCP names did not match the validated mcp.json", detail.Servers)
			}
			body, _ := json.Marshal(detail)
			for _, forbidden := range []string{"SYNTHETIC_PRIVATE_TOKEN", "private.example", "./bin/index", "PRIVATE", "not-in-mcp"} {
				if strings.Contains(string(body), forbidden) {
					t.Fatal("configuration or undiscovered tool leaked into display metadata", forbidden)
				}
			}
		})
	}
}

func TestReviewedDisplayRequiresVerifiedBytesAndSafePresentation(t *testing.T) {
	if displayText("Read/write notes") != "Read/write notes" || displayText("Open /Users/private/notes") != "" || displayText("Open C:/Users/private/notes") != "" {
		t.Fatal("display text must allow prose while omitting local paths")
	}
	files, entry := displayFixture(map[string]string{
		"plugin.json":                 `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"fixture","version":"1.0.0","author":{"name":"Real Publisher","url":"https://publisher.example/?token=hidden"},"repository":"file:///Users/private/source"}`,
		"skills/report-work/SKILL.md": "---\nname: report-work\ndescription: /Users/private/SYNTHETIC_TOKEN\n---\nBody\n",
	}, DisplayCatalog{Publisher: "Unrelated Brand", Skills: map[string]DisplayContribution{"report-work": {DescriptionZh: "${PRIVATE_TOKEN}"}}})
	detail, err := reviewedDisplay(files, entry)
	if err != nil || detail.Publisher != "Real Publisher" || detail.PublisherURL != "" || detail.SourceURL != "https://catalog.example/fixture" || len(detail.Skills) != 1 || detail.Skills[0].Description != "" || detail.Skills[0].DescriptionZh != "" {
		t.Fatal("unsafe or invented presentation metadata was accepted", detail, err)
	}
	entry.Files["plugin.json"] = strings.Repeat("0", 64)
	if _, err := reviewedDisplay(files, entry); err == nil {
		t.Fatal("unverified manifest was shown")
	}
}

func TestBundledMarkdownDisplayFromReviewedCatalog(t *testing.T) {
	m, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Mutate(t.Context(), "markdown-work", "install", nil); err != nil {
		t.Fatal(err)
	}
	items := m.Snapshot().Items
	if len(items) != 6 || items[0].Publisher != "Caelis labs" || items[0].Description != "编写笔记、报告和 README" || !items[0].Bundled || len(items[0].Skills) != 1 || len(items[0].MCPServers) != 0 || items[0].Skills[0].ID != "markdown-work" {
		t.Fatal("bundled display differs from reviewed manifest and Skill", items)
	}
}

func TestPublicSnapshotMapsContributionHealthWithoutExposingDiagnostics(t *testing.T) {
	internal := Snapshot{Items: []Item{{
		ID: "fixture", Source: "https://private.example/?token=SYNTHETIC_PRIVATE_TOKEN",
		Skills:     []Contribution{{ID: "draft-notes", Name: "Draft notes"}},
		MCPServers: []Contribution{{ID: "notes-search", Name: "notes-search"}},
		Issues: []Issue{
			{Component: "skill", Name: "/Users/private/skills/draft-notes/SKILL.md", Message: "SYNTHETIC_PRIVATE_TOKEN"},
			{Component: "server", Name: RuntimeName("fixture", "notes-search"), Message: "/Users/private/token"},
			{Component: "runtime", Name: "/Users/private/store", Message: "SYNTHETIC_PRIVATE_TOKEN"},
		},
	}}}
	public := internal.Public()
	if public.Items[0].Issues[0].Component != "skill" || public.Items[0].Issues[0].Name != "draft-notes" || public.Items[0].Issues[1].Component != "server" || public.Items[0].Issues[1].Name != "notes-search" || public.Items[0].Issues[2].Component != "package" {
		t.Fatal("health did not retain safe contribution ownership", public.Items[0].Issues)
	}
	body, _ := json.Marshal(public)
	if strings.Contains(string(body), "private") || strings.Contains(string(body), "SYNTHETIC_PRIVATE_TOKEN") || public.Items[0].Source != "" {
		t.Fatal("private diagnostics crossed the UI boundary", string(body))
	}
	if internal.Items[0].Source == "" || internal.Items[0].Issues[0].Message == "" {
		t.Fatal("public projection changed the original diagnostic snapshot")
	}
}
