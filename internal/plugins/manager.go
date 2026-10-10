package plugins

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/secretstore"
)

// The source tree is a reviewed, build-owned catalog. Publishing a new entry
// requires a Bot release; Settings cannot import arbitrary code or URLs.
//
//go:embed catalog.json all:packages
var reviewed embed.FS

type catalog struct {
	Version  int     `json:"version"`
	Packages []Entry `json:"packages"`
}
type installed struct {
	Version, Digest string
	Enabled         bool
	Root            string // immutable version directory; empty reads schema-1 state
}
type state struct {
	Version     int
	Revision    uint64
	Installed   map[string]installed
	Connections map[string]connectionRecord
}
type connectionRecord struct {
	Revision   uint64
	Configured bool
	HasCA      bool
	Mode       string `json:",omitempty"`
	// Old OAuth relays read their grant for each RPC. Keep these generations
	// until the Runtime confirms the replacement selection.
	RetiredOAuth []uint64 `json:",omitempty"`
}
type ConnectionView struct {
	Kind           string `json:"kind"`
	State          string `json:"state"`
	Stored         bool   `json:"stored,omitempty"`
	HelpURL        string `json:"helpUrl,omitempty"`
	TrustCA        bool   `json:"trustCA,omitempty"`
	HasCA          bool   `json:"hasCA,omitempty"`
	Mode           string `json:"mode,omitempty"`
	OAuthAvailable bool   `json:"oauthAvailable,omitempty"`
	InstallURL     string `json:"installUrl,omitempty"`
	OAuthError     bool   `json:"oauthError,omitempty"`
	UserCode       string `json:"userCode,omitempty"`
}

type Item struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Version      string          `json:"version"`
	Description  string          `json:"description"`
	Source       string          `json:"source"`
	Publisher    string          `json:"publisher,omitempty"`
	PublisherURL string          `json:"publisherUrl,omitempty"`
	SourceURL    string          `json:"sourceUrl,omitempty"`
	Bundled      bool            `json:"bundled"`
	Skills       []Contribution  `json:"skills"`
	MCPServers   []Contribution  `json:"mcpServers"`
	Installed    bool            `json:"installed"`
	Enabled      bool            `json:"enabled"`
	Status       string          `json:"status"`
	Issues       []Issue         `json:"issues"`
	Connection   *ConnectionView `json:"connection,omitempty"`
}
type Snapshot struct {
	Revision  uint64 `json:"revision"`
	Items     []Item `json:"items"`
	SyncState string `json:"syncState,omitempty"`
}
type SelectedServer struct {
	PackageID, Name, Root, Data string
	Server                      Server
	ConnectionRevision          uint64
	Connection                  *ConnectionSpec
}
type Selection struct {
	Revision   uint64
	SkillRoots []string
	Servers    []SelectedServer
	Issues     []Issue
}

func (s Selection) Clone() Selection {
	v := s
	v.SkillRoots = append([]string(nil), s.SkillRoots...)
	v.Issues = append([]Issue(nil), s.Issues...)
	v.Servers = make([]SelectedServer, len(s.Servers))
	for i, item := range s.Servers {
		v.Servers[i] = item
		if item.Connection != nil {
			spec := *item.Connection
			v.Servers[i].Connection = &spec
		}
		v.Servers[i].Server.Args = append([]string(nil), item.Server.Args...)
		v.Servers[i].Server.Env = map[string]string{}
		for k, value := range item.Server.Env {
			v.Servers[i].Server.Env[k] = value
		}
		v.Servers[i].Server.Headers = map[string]string{}
		for k, value := range item.Server.Headers {
			v.Servers[i].Server.Headers[k] = value
		}
	}
	return v
}

type Manager struct {
	mu            sync.Mutex
	root          string
	catalog       []Entry
	display       map[string]displayMetadata
	displayIssues map[string]bool
	sources       map[string]fs.FS
	state         state
	secrets       secretstore.Store
	oauthFlows    map[string]*oauthFlow
	oauthErrors   map[string]bool
	oauthClient   *http.Client
}

func Open(root string) (*Manager, error) {
	body, err := reviewed.ReadFile("catalog.json")
	if err != nil {
		return nil, err
	}
	var c catalog
	if json.Unmarshal(body, &c) != nil || c.Version != 1 {
		return nil, errors.New("invalid reviewed plugin catalog")
	}
	sources := make(map[string]fs.FS, len(c.Packages))
	for _, entry := range c.Packages {
		files, err := fs.Sub(reviewed, "packages/"+entry.ID)
		if err == nil {
			sources[entry.ID] = files
		}
	}
	return openCatalog(root, c.Packages, sources)
}

func openCatalog(root string, entries []Entry, sources map[string]fs.FS) (*Manager, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("plugin store path must be absolute")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("plugin store must be a real directory")
	}
	m := &Manager{root: root, catalog: entries, sources: sources, display: map[string]displayMetadata{}, displayIssues: map[string]bool{}, secrets: secretstore.Functions{SaveFunc: saveSecret, LoadFunc: loadSecret, DeleteFunc: deleteSecret}, state: state{Version: 1, Revision: 1, Installed: map[string]installed{}, Connections: map[string]connectionRecord{}}}
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.ID] || !pluginName.MatchString(e.ID) || e.Version == "" || e.Source == "" || len(e.Files) == 0 {
			return nil, errors.New("invalid reviewed plugin entry")
		}
		seen[e.ID] = true
		if spec := e.Connection; spec != nil {
			if !serviceName.MatchString(spec.Server) || !strings.Contains("|api-key|token|oauth|oauth-or-token|", "|"+spec.Kind+"|") || spec.Kind != "oauth" && (!strings.Contains("|env|header|query|", "|"+spec.Placement+"|") || spec.Name == "") || (spec.Kind == "oauth-or-token") != (spec.DeviceOAuth != nil) {
				return nil, errors.New("invalid reviewed plugin connection")
			}
			if spec.DeviceOAuth != nil && (spec.DeviceOAuth.DeviceCodeURL != "https://github.com/login/device/code" || spec.DeviceOAuth.TokenURL != "https://github.com/login/oauth/access_token" || spec.DeviceOAuth.VerifyURL != "https://github.com/login/device" || !githubAppInstallURL(spec.DeviceOAuth.InstallURL)) {
				return nil, errors.New("invalid reviewed OAuth provider")
			}
		}
		for name, hash := range e.Files {
			if !safeReviewedName(name) || len(hash) != 64 {
				return nil, errors.New("invalid reviewed plugin inventory")
			}
		}
		for _, name := range e.Executables {
			if _, ok := e.Files[name]; !ok || !safeReviewedName(name) {
				return nil, errors.New("invalid reviewed executable inventory")
			}
		}
		packageFiles := sources[e.ID]
		if packageFiles == nil {
			m.displayIssues[e.ID] = true
			continue
		}
		m.display[e.ID], err = reviewedDisplay(packageFiles, e)
		if err != nil {
			m.displayIssues[e.ID] = true
		}
	}
	statePath := filepath.Join(root, "state.json")
	if info, statErr := os.Lstat(statePath); statErr == nil && !info.Mode().IsRegular() {
		return nil, errors.New("plugin state must be a regular file")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	body, err := os.ReadFile(statePath)
	if err == nil {
		if json.Unmarshal(body, &m.state) != nil || m.state.Version != 1 || m.state.Installed == nil {
			return nil, errors.New("invalid plugin state; original file retained")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if m.state.Connections == nil {
		m.state.Connections = map[string]connectionRecord{}
	}
	return m, nil
}

func ensureStoreDirectory(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || !safeRelative(rel) {
		return errors.New("plugin path leaves private store")
	}
	path := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		path = filepath.Join(path, part)
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("plugin store directory is not a real directory")
		}
	}
	return nil
}
func (m *Manager) Root() string { return m.root }
func (m *Manager) entry(id string) (Entry, bool) {
	for _, e := range m.catalog {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}
func digest(e Entry) string {
	b, _ := json.Marshal(e.Files)
	if len(e.Executables) != 0 {
		names := append([]string(nil), e.Executables...)
		sort.Strings(names)
		modes, _ := json.Marshal(names)
		b = append(b, modes...)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func verifyExecutableModes(root string, e Entry) error {
	if runtime.GOOS == "windows" {
		return nil
	} // Windows executes by file type, not POSIX mode bits.
	for name := range e.Files {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if (info.Mode().Perm()&0111 != 0) != slices.Contains(e.Executables, name) {
			return errors.New("reviewed executable mode mismatch")
		}
	}
	return nil
}
func (m *Manager) packageRoot(e Entry) string {
	return filepath.Join(m.root, "versions", e.ID, e.Version+"-"+digest(e)[:16])
}
func (m *Manager) readInstalled(e Entry) (Package, error) {
	return m.readInstalledAt(e, "")
}
func (m *Manager) readInstalledAt(e Entry, rootName string) (Package, error) {
	base := filepath.Base(m.packageRoot(e))
	if rootName == "" {
		rootName = base
	}
	if rootName != base {
		suffix := strings.TrimPrefix(rootName, base+"-r")
		if suffix == rootName || len(suffix) != 16 {
			return Package{}, errors.New("invalid stored plugin generation")
		}
		for _, char := range suffix {
			if !strings.ContainsRune("0123456789abcdef", char) {
				return Package{}, errors.New("invalid stored plugin generation")
			}
		}
	}
	root := filepath.Join(m.root, "versions", e.ID, rootName)
	if err := Verify(root, e.Files); err != nil {
		return Package{}, err
	}
	if err := verifyExecutableModes(root, e); err != nil {
		return Package{}, err
	}
	p, err := Load(root)
	if err != nil {
		return p, err
	}
	if p.Manifest.Name != e.ID || p.Manifest.Version != e.Version {
		return p, errors.New("package identity differs from reviewed entry")
	}
	return p, nil
}
func (m *Manager) selectionLocked(next state) Selection {
	out := Selection{Revision: next.Revision, SkillRoots: []string{}, Servers: []SelectedServer{}, Issues: []Issue{}}
	for id, record := range next.Installed {
		if !record.Enabled {
			continue
		}
		e, ok := m.entry(id)
		if !ok || record.Version != e.Version || record.Digest != digest(e) {
			out.Issues = append(out.Issues, Issue{"package", id, "reviewed version unavailable"})
			continue
		}
		p, err := m.readInstalledAt(e, record.Root)
		if err != nil {
			out.Issues = append(out.Issues, Issue{"package", id, err.Error()})
			continue
		}
		out.SkillRoots = append(out.SkillRoots, p.Skills...)
		out.Issues = append(out.Issues, p.Issues...)
		for _, s := range p.Servers {
			conn := next.Connections[id]
			if e.Connection != nil && e.Connection.Server == s.Name && !conn.Configured {
				continue
			}
			out.Servers = append(out.Servers, SelectedServer{PackageID: id, Name: s.Name, Root: p.Root, Data: filepath.Join(m.root, "data", id), Server: s, ConnectionRevision: conn.Revision, Connection: e.Connection})
		}
	}
	sort.Strings(out.SkillRoots)
	sort.Slice(out.Servers, func(i, j int) bool {
		if out.Servers[i].PackageID != out.Servers[j].PackageID {
			return out.Servers[i].PackageID < out.Servers[j].PackageID
		}
		return out.Servers[i].Name < out.Servers[j].Name
	})
	return out
}
func (m *Manager) Selection() Selection {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selectionLocked(m.state)
}
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Snapshot{Revision: m.state.Revision, Items: []Item{}}
	for _, e := range m.catalog {
		rec, ok := m.state.Installed[e.ID]
		if e.Legacy && !ok {
			continue
		}
		item := Item{ID: e.ID, Title: e.Title, Version: e.Version, Description: e.Description, Source: e.Source, Installed: ok, Enabled: ok && rec.Enabled, Status: "available", Issues: []Issue{}}
		if ok {
			item.Status = "disabled"
			if rec.Enabled {
				item.Status = "enabled"
			}
			if rec.Version != e.Version || rec.Digest != digest(e) {
				item.Status = "update_available"
			} else if p, err := m.readInstalledAt(e, rec.Root); err != nil {
				item.Status = "failed"
				item.Issues = append(item.Issues, Issue{"package", e.ID, err.Error()})
			} else {
				item.Issues = append(item.Issues, p.Issues...)
				if rec.Enabled && len(item.Issues) > 0 {
					item.Status = "failed"
				}
			}
		}
		m.decorate(&item)
		m.decorateConnection(&item, e, m.state)
		if item.Connection != nil && item.Connection.State == "unsupported" {
			item.Status = "unavailable"
		}
		if item.Enabled && item.Status != "update_available" && item.Connection != nil && item.Connection.State != "configured" && item.Connection.State != "unsupported" && !(item.Connection.State == "pending" && item.Connection.Stored) {
			item.Status = "needs_connection"
		}
		out.Items = append(out.Items, item)
	}
	return out
}
func (m *Manager) decorate(item *Item) {
	detail := m.display[item.ID]
	item.Publisher, item.PublisherURL, item.SourceURL, item.Bundled = detail.Publisher, detail.PublisherURL, detail.SourceURL, detail.Bundled
	item.Skills = append([]Contribution{}, detail.Skills...)
	item.MCPServers = append([]Contribution{}, detail.Servers...)
	if m.displayIssues[item.ID] {
		item.Issues = append(item.Issues, Issue{Component: "metadata", Name: item.ID, Message: "package details unavailable"})
	}
}
func cloneState(s state) state {
	n := s
	n.Installed = make(map[string]installed, len(s.Installed))
	for k, v := range s.Installed {
		n.Installed[k] = v
	}
	n.Connections = make(map[string]connectionRecord, len(s.Connections))
	for k, v := range s.Connections {
		n.Connections[k] = v
	}
	return n
}
func (m *Manager) decorateConnection(item *Item, e Entry, st state) {
	if e.Connection == nil {
		return
	}
	if e.Connection.Kind == "oauth" && !oauthNativeSupported {
		item.Connection = &ConnectionView{Kind: "oauth", State: "unsupported", Stored: st.Connections[e.ID].Configured, HelpURL: e.Connection.HelpURL}
		return
	}
	state := "not_configured"
	if st.Connections[e.ID].Configured {
		state = "configured"
		if e.Connection.Kind == "oauth" || st.Connections[e.ID].Mode == "oauth" {
			key := secretKey(m.root, e.ID, st.Connections[e.ID].Revision)
			data, err := m.secrets.Load(key)
			var grant OAuthGrant
			if err != nil || json.Unmarshal([]byte(data), &grant) != nil || grant.AccessToken == "" {
				state = "authentication_required"
			}
		}
	}
	if e.Connection.Kind == "oauth" || e.Connection.Kind == "oauth-or-token" {
		if m.oauthFlows[e.ID] != nil {
			state = "pending"
		} else if m.oauthErrors[e.ID] && state != "configured" {
			state = "authentication_required"
		}
	}
	view := &ConnectionView{Kind: e.Connection.Kind, State: state, Stored: st.Connections[e.ID].Configured, HelpURL: e.Connection.HelpURL, TrustCA: e.Connection.TrustCA, HasCA: st.Connections[e.ID].HasCA, Mode: st.Connections[e.ID].Mode}
	if e.Connection.DeviceOAuth != nil {
		view.OAuthAvailable = deviceClientID(e.Connection.DeviceOAuth) != "" && oauthNativeSupported
		view.InstallURL = e.Connection.DeviceOAuth.InstallURL
		view.OAuthError = m.oauthErrors[e.ID]
		if flow := m.oauthFlows[e.ID]; flow != nil {
			view.UserCode = flow.userCode
		}
	}
	item.Connection = view
}
func (m *Manager) save(next state) error {
	return localstate.WriteConfirmed(filepath.Join(m.root, "state.json"), next)
}

// Mutate serializes confirmed package state. Installing also selects the
// reviewed package for automatic Runtime assembly; the host performs that
// assembly after the store commit. Legacy enable/disable actions remain for
// older callers and saved state, but are not part of the Settings flow.
func (m *Manager) Mutate(ctx context.Context, id, action string, apply func(context.Context, Selection) error) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entry(id)
	if !ok {
		return Snapshot{}, errors.New("plugin is not in the reviewed catalog")
	}
	current, has := m.state.Installed[id]
	currentConnection := m.state.Connections[id]
	next := cloneState(m.state)
	runtimeChange := false
	switch action {
	case "install":
		if e.Connection != nil && e.Connection.Kind == "oauth" && !oauthNativeSupported {
			return m.snapshotLocked(), errors.New("OAuth is unsupported on this platform")
		}
		if has {
			if current.Version != e.Version || current.Digest != digest(e) {
				return m.snapshotLocked(), errors.New("plugin is already installed; use update")
			}
			if _, err := m.readInstalledAt(e, current.Root); err != nil {
				return m.snapshotLocked(), fmt.Errorf("installed plugin verification failed: %w", err)
			}
			if current.Enabled {
				return m.snapshotLocked(), nil
			}
			current.Enabled = true
			next.Installed[id] = current
			break
		}
		rootName, err := m.stage(e)
		if err != nil {
			return m.snapshotLocked(), err
		}
		// Installation confirms the desired package immediately. Runtime
		// projection is reconciled by the Bot host after this store commit.
		next.Installed[id] = installed{Version: e.Version, Digest: digest(e), Root: rootName, Enabled: true}
	case "update":
		if !has {
			return m.snapshotLocked(), errors.New("plugin is not installed")
		}
		if current.Version == e.Version && current.Digest == digest(e) {
			return m.snapshotLocked(), nil
		}
		rootName, err := m.stage(e)
		if err != nil {
			return m.snapshotLocked(), err
		}
		next.Installed[id] = installed{Version: e.Version, Digest: digest(e), Enabled: true, Root: rootName}
		// The previous Obsidian package used an API key and a local CA.
		// The official CLI Skill has no connection, so clear that record.
		if id == "obsidian" && next.Connections[id].Configured {
			next.Connections[id] = connectionRecord{Revision: next.Revision + 1}
		}
	case "enable":
		if !has {
			return m.snapshotLocked(), errors.New("plugin is not installed")
		}
		if current.Enabled {
			return m.snapshotLocked(), nil
		}
		current.Enabled = true
		next.Installed[id] = current
		runtimeChange = true
	case "disable":
		if !has {
			return m.snapshotLocked(), nil
		}
		if !current.Enabled {
			return m.snapshotLocked(), nil
		}
		current.Enabled = false
		next.Installed[id] = current
		runtimeChange = true
	case "uninstall":
		if !has {
			return m.snapshotLocked(), nil
		}
		delete(next.Installed, id)
		runtimeChange = current.Enabled
		if next.Connections[id].Configured {
			next.Connections[id] = connectionRecord{Revision: next.Revision + 1}
		}
	default:
		return m.snapshotLocked(), errors.New("unsupported plugin action")
	}
	next.Revision++
	selection := m.selectionLocked(next)
	for _, issue := range selection.Issues {
		if issue.Component == "package" && issue.Name == id {
			return m.snapshotLocked(), errors.New(issue.Message)
		}
	}
	if err := m.save(next); err != nil {
		// A write may have reached rename before a later durability error.
		// Restore the last confirmed document before releasing turn admission.
		stateErr := m.save(m.state)
		failures := []error{err}
		if stateErr != nil {
			failures = append(failures, fmt.Errorf("plugin state restoration failed: %w", stateErr))
		}
		return m.snapshotLocked(), errors.Join(failures...)
	}
	// Codex may prewarm an MCP process during reload. The launcher reopens the
	// on-disk generation, so publish the candidate before asking Runtime to
	// reload. Readers of this Manager stay blocked on m.mu until confirmation.
	if apply != nil && runtimeChange {
		if err := apply(ctx, selection); err != nil {
			// The adapter owns Runtime rollback and unknown receipts. Never issue
			// another update under a fresh ID here.
			if restoreErr := m.save(m.state); restoreErr != nil {
				return m.snapshotLocked(), errors.Join(err, fmt.Errorf("plugin state restoration failed: %w", restoreErr))
			}
			return m.snapshotLocked(), err
		}
	}
	m.state = next
	if action == "disable" || action == "uninstall" {
		m.cancelOAuthLocked(id)
	}
	if (action == "uninstall" && e.Connection != nil) || ((action == "uninstall" || action == "update") && id == "obsidian") {
		old := currentConnection
		for _, revision := range old.RetiredOAuth {
			_ = m.revokeOAuthKey(id, revision)
		}
		if old.Configured {
			if (e.Connection != nil && e.Connection.Kind == "oauth") || old.Mode == "oauth" {
				_ = m.revokeOAuthKey(id, old.Revision)
			} else {
				_ = m.secrets.Delete(secretKey(m.root, id, old.Revision))
			}
			if old.HasCA {
				_ = m.secrets.Delete(secretKey(m.root, id, old.Revision) + "-ca")
			}
		}
	}
	return m.snapshotLocked(), nil
}
func (m *Manager) snapshotLocked() Snapshot {
	out := Snapshot{Revision: m.state.Revision, Items: []Item{}}
	for _, e := range m.catalog {
		rec, ok := m.state.Installed[e.ID]
		if e.Legacy && !ok {
			continue
		}
		status := "available"
		if ok {
			status = "disabled"
			if rec.Enabled {
				status = "enabled"
			}
		}
		item := Item{ID: e.ID, Title: e.Title, Version: e.Version, Description: e.Description, Source: e.Source, Installed: ok, Enabled: ok && rec.Enabled, Status: status, Issues: []Issue{}}
		m.decorate(&item)
		m.decorateConnection(&item, e, m.state)
		if item.Connection != nil && item.Connection.State == "unsupported" {
			item.Status = "unavailable"
		}
		if item.Enabled && item.Connection != nil && item.Connection.State != "configured" && item.Connection.State != "unsupported" && !(item.Connection.State == "pending" && item.Connection.Stored) {
			item.Status = "needs_connection"
		}
		out.Items = append(out.Items, item)
	}
	return out
}
func (m *Manager) stage(e Entry) (string, error) {
	if m.sources[e.ID] == nil {
		return "", errors.New("reviewed package source unavailable")
	}
	dest := m.packageRoot(e)
	if err := Verify(dest, e.Files); err == nil && verifyExecutableModes(dest, e) == nil {
		if _, err = m.readInstalled(e); err == nil {
			return filepath.Base(dest), nil
		}
	}
	if err := ensureStoreDirectory(m.root, filepath.Dir(dest)); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".staging-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for name := range e.Files {
		body, err := fs.ReadFile(m.sources[e.ID], name)
		if err != nil {
			return "", err
		}
		path := filepath.Join(tmp, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		mode := os.FileMode(0600)
		if slices.Contains(e.Executables, name) {
			mode = 0700
		}
		if err = os.WriteFile(path, body, mode); err != nil {
			return "", err
		}
	}
	if err := Verify(tmp, e.Files); err != nil {
		return "", err
	}
	if err := verifyExecutableModes(tmp, e); err != nil {
		return "", err
	}
	p, err := Load(tmp)
	if err != nil {
		return "", err
	}
	if p.Manifest.Name != e.ID || p.Manifest.Version != e.Version {
		return "", errors.New("reviewed package identity mismatch")
	}
	if _, err := os.Lstat(dest); err == nil {
		// A corrupt immutable root may still be held by an older Runtime
		// snapshot. Keep it in place and install reviewed bytes in a new root.
		var nonce [8]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", err
		}
		dest += "-r" + hex.EncodeToString(nonce[:])
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		if errors.Is(err, fs.ErrExist) {
			if verifyErr := Verify(dest, e.Files); verifyErr != nil {
				return "", verifyErr
			}
			return filepath.Base(dest), verifyExecutableModes(dest, e)
		}
		return "", err
	}
	return filepath.Base(dest), nil
}

func (m *Manager) DebugEntry(id string) (Entry, error) {
	e, ok := m.entry(id)
	if !ok {
		return e, fmt.Errorf("unknown package %s", id)
	}
	return e, nil
}
