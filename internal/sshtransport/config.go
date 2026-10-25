// Package sshtransport owns only OpenSSH configuration and owned network
// channels. It has no Worker, task, receipt or Bot authorization authority.
package sshtransport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const maxConfig = 128 << 10

type Transport struct {
	mu                                 sync.Mutex
	configMu                           sync.Mutex
	root, home, system, binary, helper string
	tunnels                            map[string]*tunnel
	closed                             bool
}

func New(root string) *Transport {
	home, _ := os.UserHomeDir()
	binary, _ := exec.LookPath("ssh")
	helper, _ := os.Executable()
	system := "/etc/ssh/ssh_config"
	if os.PathSeparator == '\\' {
		system = filepath.Join(os.Getenv("PROGRAMDATA"), "ssh", "ssh_config")
	}
	return &Transport{root: root, home: home, system: system, binary: binary, helper: helper, tunnels: map[string]*tunnel{}}
}
func (t *Transport) path() string { return filepath.Join(t.root, "config") }
func revision(b []byte) string    { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func (t *Transport) Configuration() (api.SSHConfiguration, error) {
	t.configMu.Lock()
	defer t.configMu.Unlock()
	return t.configuration()
}
func (t *Transport) configuration() (api.SSHConfiguration, error) {
	text, err := readConfig(t.path())
	if os.IsNotExist(err) {
		text = nil
		err = nil
	}
	if err != nil {
		return api.SSHConfiguration{}, errors.New("ssh_config_unreadable")
	}
	v := api.SSHConfiguration{Path: t.path(), UserPath: filepath.Join(t.home, ".ssh", "config"), SystemPath: t.system, Text: string(text), Revision: revision(text), Hosts: []string{}}
	g := graph{home: t.home, aliases: map[string]bool{}}
	for _, p := range []struct{ path, base string }{{t.path(), filepath.Join(t.home, ".ssh")}, {v.UserPath, filepath.Join(t.home, ".ssh")}, {t.system, filepath.Dir(t.system)}} {
		g.scan(p.path, p.base, 0)
	}
	for host := range g.aliases {
		v.Hosts = append(v.Hosts, host)
	}
	sort.Strings(v.Hosts)
	if g.incomplete {
		v.Issue = "ssh_config_partial_index"
	}
	return v, nil
}
func readConfig(path string) ([]byte, error) {
	f, e := openConfig(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > maxConfig {
		return nil, errors.New("ssh_config_size")
	}
	header := make([]byte, min(info.Size(), 256))
	_, _ = f.ReadAt(header, 0)
	if trimmed := bytes.TrimSpace(header); bytes.HasPrefix(trimmed, []byte("-----BEGIN ")) && bytes.Contains(trimmed, []byte("PRIVATE KEY-----")) {
		return nil, errors.New("ssh_config_contains_key")
	}
	b := make([]byte, info.Size())
	_, e = f.ReadAt(b, 0)
	if len(b) == 0 {
		e = nil
	}
	return b, e
}

// tokens is for inert Include traversal and literal Host suggestions only.
// It never evaluates Host/Match or resolves effective options; ssh does that.
func tokens(line string) ([]string, error) {
	var out []string
	var b strings.Builder
	var quote rune
	escape, word := false, false
	flush := func() {
		if word {
			out = append(out, b.String())
			b.Reset()
			word = false
		}
	}
	for _, r := range line {
		if escape {
			b.WriteRune(r)
			word = true
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			word = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			word = true
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			word = true
			continue
		}
		if r == '#' {
			break
		}
		if unicode.IsSpace(r) || r == '=' && len(out) == 0 {
			flush()
			continue
		}
		b.WriteRune(r)
		word = true
	}
	if escape || quote != 0 {
		return nil, errors.New("ssh_config_syntax")
	}
	flush()
	return out, nil
}
func validHost(host string) bool {
	if len(host) == 0 || len(host) > 253 || host[0] == '-' {
		return false
	}
	for _, r := range host {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func ValidHost(host string) bool { return validHost(host) }

// Include paths are expanded only for an inert, bounded file index/syntax copy.
// Dynamic includes stay with OpenSSH at actual connect time.
func (g *graph) includes(values []string, base string) []string {
	var out []string
	for _, v := range values {
		if strings.ContainsAny(v, "%$") {
			g.incomplete = true
			continue
		}
		if strings.HasPrefix(v, "~/") {
			v = filepath.Join(g.home, v[2:])
		}
		if !filepath.IsAbs(v) {
			v = filepath.Join(base, v)
		}
		matches, e := filepath.Glob(v)
		if e != nil {
			g.incomplete = true
			continue
		}
		out = append(out, matches...)
	}
	return out
}

type graph struct {
	home       string
	aliases    map[string]bool
	count      int
	incomplete bool
}

func (g *graph) scan(path, base string, depth int) {
	if depth > 16 || g.count >= 64 {
		g.incomplete = true
		return
	}
	g.count++
	b, e := readConfig(path)
	if os.IsNotExist(e) {
		return
	}
	if e != nil {
		g.incomplete = true
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		v, e := tokens(line)
		if e != nil {
			g.incomplete = true
			continue
		}
		if len(v) < 2 {
			continue
		}
		switch strings.ToLower(v[0]) {
		case "host":
			for _, h := range v[1:] {
				if validHost(h) {
					if len(g.aliases) >= 256 && !g.aliases[h] {
						g.incomplete = true
						continue
					}
					g.aliases[h] = true
				}
			}
		case "include":
			for _, p := range g.includes(v[1:], base) {
				g.scan(p, base, depth+1)
			}
		}
	}
}
func quote(path string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(filepath.ToSlash(path), `\`, `\\`), `"`, `\"`) + `"`
}

// A private -F alone drops system configuration. Build a standard invocation
// file with explicit layers. Mirroring system Includes retains /etc/ssh-relative
// resolution even though the outer -F file has user-config semantics.
func (t *Transport) compose(dir string, preview bool, private string) (string, error) {
	g := graph{home: t.home}
	var visit func(string, string, int) (string, error)
	visit = func(path, base string, depth int) (string, error) {
		if depth > 16 || g.count >= 64 {
			return "", errors.New("ssh_config_include_limit")
		}
		g.count++
		b, e := readConfig(path)
		if os.IsNotExist(e) {
			return "", nil
		}
		if e != nil {
			return "", e
		}
		if path == t.path() && private != "" {
			b = []byte(private)
		}
		lines := strings.Split(string(b), "\n")
		for i, line := range lines {
			v, err := tokens(line)
			if err != nil {
				if preview {
					return "", errors.New("ssh_config_syntax")
				}
				continue
			}
			if len(v) == 0 {
				continue
			}
			if strings.EqualFold(v[0], "Include") {
				// In validation copies no unexamined Include can execute Match exec.
				if !preview && base == filepath.Dir(t.system) {
					for _, value := range v[1:] {
						if strings.ContainsAny(value, "%$") {
							return "", errors.New("ssh_system_include_dynamic")
						}
					}
				}
				if preview || base == filepath.Dir(t.system) {
					var names []string
					for _, p := range g.includes(v[1:], base) {
						name, err := visit(p, base, depth+1)
						if err != nil {
							return "", err
						}
						if name != "" {
							names = append(names, quote(name))
						}
					}
					if len(names) == 0 {
						lines[i] = "# Include evaluated by ssh when connecting"
					} else {
						lines[i] = "Include " + strings.Join(names, " ")
					}
				}
			}
			if preview && strings.EqualFold(v[0], "Match") {
				for _, word := range v[1:] {
					if strings.EqualFold(strings.TrimPrefix(word, "!"), "exec") {
						lines[i] = "Match all"
						break
					}
				}
			}
		}
		name := filepath.Join(dir, fmt.Sprintf("layer-%d", g.count)) // reserve distinct name after recursive children
		for {
			if _, err := os.Stat(name); os.IsNotExist(err) {
				break
			}
			g.count++
			name = filepath.Join(dir, fmt.Sprintf("layer-%d", g.count))
		}
		if err := os.WriteFile(name, []byte(strings.Join(lines, "\n")), 0600); err != nil {
			return "", err
		}
		return name, nil
	}
	layers := []string{"Host *\n ControlMaster no\n ControlPath none\n ControlPersist no\n ForkAfterAuthentication no\n PermitLocalCommand no\n StrictHostKeyChecking ask\n ForwardAgent no\n ClearAllForwardings yes"}
	// For runtime keep private/user files native, including dynamic Include. Only
	// system paths need a mirror for their distinct relative Include base.
	for i, p := range []string{t.path(), filepath.Join(t.home, ".ssh", "config"), t.system} {
		if i == 0 && preview {
			p = filepath.Join(dir, "draft")
			if err := os.WriteFile(p, []byte(private), 0600); err != nil {
				return "", err
			}
		}
		if !preview && i < 2 {
			layers = append(layers, "Host *\nInclude "+quote(p))
			continue
		}
		base := filepath.Join(t.home, ".ssh")
		if i == 2 {
			base = filepath.Dir(t.system)
		}
		name, err := visit(p, base, 0)
		if err != nil {
			return "", err
		}
		if name != "" {
			layers = append(layers, "Host *\nInclude "+quote(name))
		}
	}
	name := filepath.Join(dir, "invocation")
	return name, os.WriteFile(name, []byte(strings.Join(layers, "\n")+"\n"), 0600)
}
func (t *Transport) Save(ctx context.Context, in api.SSHConfigurationSave) (api.SSHConfiguration, error) {
	if len(in.Text) > maxConfig || strings.IndexByte(in.Text, 0) >= 0 {
		return api.SSHConfiguration{}, errors.New("ssh_config_size")
	}
	t.configMu.Lock()
	defer t.configMu.Unlock()
	old, e := t.configuration()
	if e != nil {
		return old, e
	}
	if old.Revision != in.Revision {
		return old, errors.New("ssh_config_changed")
	}
	if t.binary == "" {
		return old, errors.New("ssh_not_installed")
	}
	if e = localstate.EnsurePrivateDirectory(t.root); e != nil {
		return old, errors.New("ssh_config_save_failed")
	}
	dir, e := os.MkdirTemp(t.root, "validate-")
	if e != nil {
		return old, errors.New("ssh_config_save_failed")
	}
	defer os.RemoveAll(dir)
	path, e := t.compose(dir, true, in.Text)
	if e != nil {
		return old, errors.New("ssh_config_syntax")
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// Preview does not run Match exec, DNS canonicalization, ProxyCommand or local commands.
	cmd := exec.CommandContext(c, t.binary, "-G", "-F", path, "-o", "CanonicalizeHostname=no", "-o", "PermitLocalCommand=no", "caelis-config-validation.invalid")
	if e = cmd.Run(); e != nil {
		return old, errors.New("ssh_config_syntax")
	}
	// External edits while validating must not be silently overwritten.
	current, e := t.configuration()
	if e != nil || current.Revision != in.Revision {
		return current, errors.New("ssh_config_changed")
	}
	f, e := os.CreateTemp(t.root, ".config-")
	if e != nil {
		return old, errors.New("ssh_config_save_failed")
	}
	name := f.Name()
	defer os.Remove(name)
	if e = localstate.ProtectOpenFile(f); e == nil {
		_, e = f.WriteString(in.Text)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(name, t.path())
	}
	if e != nil {
		return old, errors.New("ssh_config_save_failed")
	}
	if err := localstate.SyncParent(t.path()); err != nil {
		return old, errors.New("ssh_config_save_unconfirmed")
	}
	return t.configuration()
}
