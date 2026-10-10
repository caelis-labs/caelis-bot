// Package plugins owns vetted portable package content and Bot-only selections.
package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const manifestSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
const mcpSchema = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"

var pluginName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,62}[a-z0-9])?$`)
var serviceName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type Entry struct {
	ID, Title, Version, Description, Source string
	Files                                   map[string]string
	Executables                             []string        `json:"executables,omitempty"`
	Display                                 DisplayCatalog  `json:"display,omitempty"`
	Connection                              *ConnectionSpec `json:"connection,omitempty"`
	Legacy                                  bool            `json:"legacy,omitempty"`
	UpstreamIntegrity                       string          `json:"upstreamIntegrity,omitempty"`
}

// ConnectionSpec describes only where an upstream MCP server expects a user
// credential. It is reviewed catalog metadata, never a value or permission.
type ConnectionSpec struct {
	Server, Kind, Placement, Name, Prefix, HelpURL string
	TrustCA                                        bool
	DeviceOAuth                                    *DeviceOAuthSpec
}
type DeviceOAuthSpec struct {
	ClientID, DeviceCodeURL, TokenURL, VerifyURL, InstallURL, Scope string
}
type DisplayCatalog struct {
	Publisher string                         `json:"publisher,omitempty"`
	Skills    map[string]DisplayContribution `json:"skills,omitempty"`
	Servers   map[string]DisplayContribution `json:"servers,omitempty"`
}
type DisplayContribution struct {
	NameEn        string `json:"nameEn,omitempty"`
	NameZh        string `json:"nameZh,omitempty"`
	DescriptionEn string `json:"descriptionEn,omitempty"`
	DescriptionZh string `json:"descriptionZh,omitempty"`
}
type Manifest struct {
	Schema      string          `json:"$schema"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Author      json.RawMessage `json:"author"`
	Homepage    string          `json:"homepage"`
	Repository  string          `json:"repository"`
	License     string          `json:"license"`
	Keywords    []string        `json:"keywords"`
	Extensions  json.RawMessage `json:"extensions"`
}
type Server struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	CWD     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}
type Issue struct {
	Component string `json:"component"`
	Name      string `json:"name"`
	Message   string `json:"message"`
}
type Package struct {
	Manifest Manifest
	Root     string
	Skills   []string
	Servers  []Server
	Issues   []Issue
}

func safeRelative(name string) bool {
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || filepath.Clean(name) != name {
		return false
	}
	if strings.HasPrefix(name, ".."+string(os.PathSeparator)) || strings.Contains(name, ":") {
		return false
	}
	return os.PathSeparator == '\\' || !strings.Contains(name, "\\")
}

// The reviewed inventory is portable slash syntax. Convert it to a native
// relative path only after rejecting backslashes supplied by the inventory.
func safeReviewedName(name string) bool {
	return !strings.Contains(name, "\\") && safeRelative(filepath.FromSlash(name)) && filepath.ToSlash(filepath.FromSlash(name)) == name
}

// Verify checks every byte against a Bot-reviewed inventory before any path is
// projected. Additional files and symlinks are not silently executable content.
func Verify(root string, files map[string]string) error {
	seen := map[string]bool{}
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if !safeRelative(rel) {
			return errors.New("unsafe package path")
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("package symlink is not allowed")
		}
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return errors.New("unsupported package file")
		}
		total += info.Size()
		if total > 64<<20 {
			return errors.New("package exceeds size limit")
		}
		want, ok := files[filepath.ToSlash(rel)]
		if !ok {
			return fmt.Errorf("unreviewed package file: %s", rel)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got := sha256.Sum256(body)
		if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
			return fmt.Errorf("package hash mismatch: %s", rel)
		}
		seen[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		return err
	}
	for name := range files {
		if !safeReviewedName(name) || !seen[name] {
			return fmt.Errorf("missing or unsafe reviewed file: %s", name)
		}
	}
	return nil
}

func Load(root string) (Package, error) {
	p := Package{Root: root, Skills: []string{}, Servers: []Server{}, Issues: []Issue{}}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return p, errors.New("invalid package root")
	}
	body, err := os.ReadFile(filepath.Join(root, "plugin.json"))
	if err != nil {
		return p, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return p, errors.New("invalid plugin manifest")
	}
	for _, key := range []string{"$schema", "name"} {
		if len(fields[key]) == 0 {
			return p, fmt.Errorf("missing manifest %s", key)
		}
	}
	if err := json.Unmarshal(body, &p.Manifest); err != nil {
		return p, err
	}
	m := p.Manifest
	if m.Schema != manifestSchema || !pluginName.MatchString(m.Name) || strings.Contains(m.Name, "--") || strings.Contains(m.Name, "..") {
		return p, errors.New("unsupported plugin manifest")
	}
	if len(m.Extensions) > 0 && string(m.Extensions) != "null" && m.Extensions[0] != '{' {
		p.Issues = append(p.Issues, Issue{"manifest", "extensions", "ignored non-object extensions"})
	}
	if len(m.Author) > 0 && string(m.Author) != "null" && m.Author[0] != '{' {
		return p, errors.New("invalid manifest author")
	}
	for name := range fields {
		if !strings.Contains("|$schema|name|version|description|author|homepage|repository|license|keywords|extensions|", "|"+name+"|") {
			p.Issues = append(p.Issues, Issue{"manifest", name, "unknown field ignored"})
		}
	}
	if err := loadSkills(&p); err != nil {
		p.Issues = append(p.Issues, Issue{"skills", "", err.Error()})
	}
	if err := loadMCP(&p); err != nil {
		p.Issues = append(p.Issues, Issue{"mcp", "", err.Error()})
	}
	if len(p.Skills) == 0 && len(p.Servers) == 0 {
		return p, errors.New("package has no usable skills or MCP servers")
	}
	return p, nil
}

func loadSkills(p *Package) error {
	root := filepath.Join(p.Root, "skills")
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid skills directory")
	}
	children, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, child := range children {
		path := filepath.Join(root, child.Name(), "SKILL.md")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			p.Issues = append(p.Issues, Issue{"skill", child.Name(), "SKILL.md missing or unsafe"})
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			p.Issues = append(p.Issues, Issue{"skill", child.Name(), err.Error()})
			continue
		}
		if !strings.HasPrefix(string(body), "---\n") || !strings.Contains(string(body), "\nname:") || !strings.Contains(string(body), "\ndescription:") {
			p.Issues = append(p.Issues, Issue{"skill", child.Name(), "invalid Skill metadata"})
			continue
		}
		p.Skills = append(p.Skills, filepath.Join(root, child.Name()))
	}
	sort.Strings(p.Skills)
	return nil
}

func loadMCP(p *Package) error {
	path := filepath.Join(p.Root, "mcp.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid mcp.json")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc struct {
		Schema  string                     `json:"$schema"`
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || json.Unmarshal(body, &doc) != nil || doc.Schema != mcpSchema || doc.Servers == nil || len(fields) != 2 {
		return errors.New("invalid MCP document")
	}
	for name, raw := range doc.Servers {
		var s Server
		var keys map[string]json.RawMessage
		if !serviceName.MatchString(name) || json.Unmarshal(raw, &keys) != nil || json.Unmarshal(raw, &s) != nil {
			p.Issues = append(p.Issues, Issue{"server", name, "invalid MCP server"})
			continue
		}
		s.Name = name
		if err := validateServer(p.Root, s, keys); err != nil {
			p.Issues = append(p.Issues, Issue{"server", name, err.Error()})
			continue
		}
		p.Servers = append(p.Servers, s)
	}
	sort.Slice(p.Servers, func(i, j int) bool { return p.Servers[i].Name < p.Servers[j].Name })
	return nil
}

func validateServer(root string, s Server, keys map[string]json.RawMessage) error {
	allowed := "|type|url|headers|"
	if s.Type == "stdio" {
		allowed = "|type|command|args|env|cwd|"
		if s.Command == "" || strings.ContainsAny(s.Command, " \t\n") || !(strings.HasPrefix(s.Command, "./") || !strings.Contains(s.Command, "/")) {
			return errors.New("invalid executable token")
		}
		if strings.HasPrefix(s.Command, "./") && !inside(root, filepath.Join(root, s.Command)) {
			return errors.New("command leaves package")
		}
		if s.CWD != "" && !strings.HasPrefix(s.CWD, "./") && s.CWD != "${PLUGIN_ROOT}" && !strings.HasPrefix(s.CWD, "${PLUGIN_ROOT}/") && s.CWD != "${PLUGIN_DATA}" && !strings.HasPrefix(s.CWD, "${PLUGIN_DATA}/") {
			return errors.New("invalid cwd")
		}
		if _, ok := s.Env["PLUGIN_ROOT"]; ok {
			return errors.New("reserved environment")
		}
		if _, ok := s.Env["PLUGIN_DATA"]; ok {
			return errors.New("reserved environment")
		}
	} else if s.Type == "streamable-http" || s.Type == "sse" {
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
			return errors.New("invalid remote URL")
		}
		if u.Scheme != "https" {
			host := u.Hostname()
			ip := net.ParseIP(host)
			if u.Scheme != "http" || !(host == "localhost" || ip != nil && ip.IsLoopback()) {
				return errors.New("remote MCP requires HTTPS")
			}
		}
		for k, v := range s.Headers {
			if strings.TrimSpace(k) == "" || strings.ContainsAny(k, "\r\n :") || strings.ContainsAny(v, "\r\n") {
				return errors.New("invalid header")
			}
		}
	} else {
		return errors.New("unsupported MCP transport")
	}
	for key := range keys {
		if !strings.Contains(allowed, "|"+key+"|") {
			return fmt.Errorf("unknown server field: %s", key)
		}
	}
	return nil
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func Expand(value, root, data string) string {
	return strings.NewReplacer("${PLUGIN_ROOT}", root, "${PLUGIN_DATA}", data).Replace(value)
}

func (s Server) Resolve(root, data string) (Server, error) {
	if s.Type != "stdio" {
		return s, nil
	}
	s.Args = append([]string(nil), s.Args...)
	env := make(map[string]string, len(s.Env)+2)
	for k, v := range s.Env {
		env[k] = v
	}
	s.Env = env
	if strings.HasPrefix(s.Command, "./") {
		s.Command = filepath.Join(root, s.Command)
	}
	for i, arg := range s.Args {
		s.Args[i] = Expand(arg, root, data)
	}
	for key, v := range s.Env {
		s.Env[key] = Expand(v, root, data)
	}
	s.Env["PLUGIN_ROOT"], s.Env["PLUGIN_DATA"] = root, data
	if s.CWD == "" {
		s.CWD = root
	} else {
		s.CWD = Expand(s.CWD, root, data)
		if strings.HasPrefix(s.CWD, "./") {
			s.CWD = filepath.Join(root, s.CWD)
		}
	}
	if !inside(root, s.CWD) && !inside(data, s.CWD) {
		return s, errors.New("cwd leaves package/data")
	}
	return s, nil
}
