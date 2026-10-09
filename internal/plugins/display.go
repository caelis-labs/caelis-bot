package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Contribution contains only reviewed presentation metadata. Runtime server
// configuration, local paths, headers and environment never enter this DTO.
type Contribution struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	NameZh        string `json:"nameZh,omitempty"`
	DescriptionZh string `json:"descriptionZh,omitempty"`
}

// Tool is presentation metadata from Runtime status or an explicit detail
// preview. Hints are server claims and never affect approval policy.
type Tool struct {
	Name            string `json:"name"`
	Title           string `json:"title,omitempty"`
	Description     string `json:"description,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

type ServerDetail struct {
	State     string `json:"state"`
	Tools     []Tool `json:"tools"`
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Preview   bool   `json:"preview,omitempty"`
}

type SkillDetail struct {
	Body string `json:"body"`
}

type displayMetadata struct {
	Publisher    string
	PublisherURL string
	SourceURL    string
	Bundled      bool
	Skills       []Contribution
	Servers      []Contribution
}

var displayPath = regexp.MustCompile(`(^|[\s(])(/|\./|~/|[A-Za-z]:/)[^\s]+`)

// Public removes runtime diagnostics and the catalog's raw source before a
// snapshot crosses the desktop UI boundary. Contribution IDs are reviewed
// names; paths, environment values and provider warnings remain internal.
func (snapshot Snapshot) Public() Snapshot {
	copyItems := make([]Item, len(snapshot.Items))
	copy(copyItems, snapshot.Items)
	snapshot.Items = copyItems
	for i := range snapshot.Items {
		item := &snapshot.Items[i]
		item.Source = ""
		issues := make([]Issue, 0, len(item.Issues))
		for _, raw := range item.Issues {
			issue := Issue{Component: "package"}
			switch raw.Component {
			case "skill":
				for _, skill := range item.Skills {
					if raw.Name == skill.ID || strings.Contains(filepath.ToSlash(raw.Name), "/"+skill.ID+"/") {
						issue.Component, issue.Name = "skill", skill.ID
						break
					}
				}
			case "server":
				for _, server := range item.MCPServers {
					if raw.Name == server.ID || raw.Name == RuntimeName(item.ID, server.ID) {
						issue.Component, issue.Name = "server", server.ID
						break
					}
				}
			}
			issues = append(issues, issue)
		}
		item.Issues = issues
	}
	return snapshot
}

func displayText(value string) string {
	return displayTextBound(value, 240)
}

func displayTextBound(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit || strings.ContainsAny(value, "\\$=") || strings.Contains(value, "://") || displayPath.MatchString(value) {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"token=", "token:", "secret=", "secret:", "password=", "password:", "api_key=", "api_key:"} {
		if strings.Contains(lower, marker) {
			return ""
		}
	}
	return value
}

// SafeDisplayText is the narrow presentation boundary for untrusted MCP
// directory labels. A rejected value is omitted, never rendered as raw config.
func SafeDisplayText(value string) string { return displayText(value) }

// SafeDisplayToolName matches Core's 256-character MCP remote tool-name
// boundary. The ready Runtime directory remains the authority for the name.
func SafeDisplayToolName(value string) string { return displayTextBound(value, 256) }

// SafeDisplayDescription keeps normal MCP tool explanations that are longer
// than a row label. Core bounds accepted descriptions to 1024 Unicode
// characters; retain that full range with the same safety exclusions.
func SafeDisplayDescription(value string) string {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 1024 {
		return ""
	}
	return displayTextBound(strings.Join(strings.Fields(value), " "), 1024)
}

func displayURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return u.String()
}

func reviewedFile(files fs.FS, entry Entry, name string) ([]byte, error) {
	want, ok := entry.Files[name]
	if !ok || !safeReviewedName(name) {
		return nil, errors.New("unreviewed display metadata")
	}
	body, err := fs.ReadFile(files, name)
	if err != nil || len(body) > 16<<20 {
		return nil, errors.New("reviewed display metadata unavailable")
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), want) {
		return nil, errors.New("reviewed display metadata hash mismatch")
	}
	return body, nil
}

func skillDisplay(body []byte) (name, description string) {
	if len(body) < 4 || len(body) > 16<<20 || !strings.HasPrefix(string(body), "---\n") {
		return "", ""
	}
	front, _, ok := strings.Cut(string(body[4:]), "\n---\n")
	if !ok || len(front) > 16<<10 {
		return "", ""
	}
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if yaml.Unmarshal([]byte(front), &meta) != nil {
		return "", ""
	}
	if meta.Name == "" || meta.Description == "" {
		return "", ""
	}
	return displayText(meta.Name), displayText(meta.Description)
}

func reviewedDisplay(files fs.FS, entry Entry) (displayMetadata, error) {
	out := displayMetadata{Bundled: strings.HasPrefix(entry.Source, "bundled:"), Skills: []Contribution{}, Servers: []Contribution{}}
	body, err := reviewedFile(files, entry, "plugin.json")
	if err != nil {
		return out, err
	}
	var manifest Manifest
	if json.Unmarshal(body, &manifest) != nil || manifest.Schema != manifestSchema || manifest.Name != entry.ID || manifest.Version != entry.Version {
		return out, errors.New("reviewed manifest identity mismatch")
	}
	var author struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	_ = json.Unmarshal(manifest.Author, &author)
	out.Publisher = displayText(author.Name)
	if override := displayText(entry.Display.Publisher); override != "" && strings.EqualFold(override, out.Publisher) {
		out.Publisher = override
	}
	out.PublisherURL = displayURL(author.URL)
	for _, candidate := range []string{manifest.Repository, manifest.Homepage, entry.Source} {
		if out.SourceURL = displayURL(candidate); out.SourceURL != "" {
			break
		}
	}
	for name := range entry.Files {
		if !strings.HasPrefix(name, "skills/") || path.Base(name) != "SKILL.md" || len(strings.Split(name, "/")) != 3 {
			continue
		}
		body, err := reviewedFile(files, entry, name)
		if err != nil {
			continue
		}
		id := path.Base(path.Dir(name))
		skillName, description := skillDisplay(body)
		if skillName == "" {
			continue
		}
		label := entry.Display.Skills[id]
		out.Skills = append(out.Skills, Contribution{ID: id, Name: displayText(label.NameEn), Description: displayText(label.DescriptionEn), NameZh: displayText(label.NameZh), DescriptionZh: displayText(label.DescriptionZh)})
		item := &out.Skills[len(out.Skills)-1]
		if item.Name == "" {
			item.Name = skillName
		}
		if item.Description == "" {
			item.Description = description
		}
	}
	sort.Slice(out.Skills, func(i, j int) bool { return out.Skills[i].ID < out.Skills[j].ID })
	if _, ok := entry.Files["mcp.json"]; !ok {
		return out, nil
	}
	body, err = reviewedFile(files, entry, "mcp.json")
	if err != nil {
		return out, err
	}
	var doc struct {
		Schema  string                     `json:"$schema"`
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || json.Unmarshal(body, &doc) != nil || doc.Schema != mcpSchema || doc.Servers == nil || len(fields) != 2 {
		return out, errors.New("reviewed MCP metadata unavailable")
	}
	for name, raw := range doc.Servers {
		var server Server
		var keys map[string]json.RawMessage
		if !serviceName.MatchString(name) || json.Unmarshal(raw, &keys) != nil || json.Unmarshal(raw, &server) != nil {
			continue
		}
		server.Name = name
		if validateServer(".", server, keys) != nil {
			continue
		}
		label := entry.Display.Servers[name]
		out.Servers = append(out.Servers, Contribution{ID: name, Name: name, Description: displayText(label.DescriptionEn), NameZh: displayText(label.NameZh), DescriptionZh: displayText(label.DescriptionZh)})
	}
	sort.Slice(out.Servers, func(i, j int) bool { return out.Servers[i].ID < out.Servers[j].ID })
	// A single-server MCP-only package has a verified package purpose that can
	// serve as its service summary. Multi-contribution packages need an explicit
	// service description; do not invent one from a server name.
	if len(out.Servers) == 1 && len(out.Skills) == 0 && out.Servers[0].Description == "" {
		out.Servers[0].Description = displayText(manifest.Description)
	}
	return out, nil
}
