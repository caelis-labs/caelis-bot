// Package contentpack owns inert appearance content, independently of Bot state.
package contentpack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxFile = 16 << 20
const MaxPack = 64 << 20
const MaxFiles = 128

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var packPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}\.[a-z][a-z0-9-]{0,47}$`)
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)
var pathPattern = regexp.MustCompile(`^[a-zA-Z0-9_./-]+$`)

type Manifest struct {
	Format        string      `json:"format"`
	FormatVersion int         `json:"formatVersion"`
	ID            string      `json:"id"`
	Version       string      `json:"version"`
	Name          string      `json:"name"`
	Author        string      `json:"author"`
	License       string      `json:"license"`
	LicenseFile   string      `json:"licenseFile"`
	Characters    []Character `json:"characters,omitempty"`
	Avatars       []Avatar    `json:"avatars,omitempty"`
	Files         []File      `json:"files"`
}
type Character struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Variants []Variant `json:"variants"`
}
type Variant struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Model      string `json:"model"`
	Avatar     string `json:"avatar,omitempty"`
	Capability string `json:"capability"`
}
type Avatar struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	Capability string `json:"capability"`
}
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Pack struct {
	Manifest Manifest
	Files    map[string][]byte
}

func Hash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func textOK(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}
func validPath(s string) bool {
	if !pathPattern.MatchString(s) || len(s) > 160 || path.Clean(s) != s || strings.HasPrefix(s, "/") {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if p == "" || p == "." || p == ".." || strings.HasSuffix(p, ".") {
			return false
		}
		base := strings.ToUpper(strings.Split(p, ".")[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || regexp.MustCompile(`^(COM|LPT)[0-9]$`).MatchString(base) {
			return false
		}
	}
	return true
}

// StrictJSON rejects duplicate object keys in addition to unknown typed fields.
func StrictJSON(b []byte, out any) error {
	if !utf8.Valid(b) {
		return errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errors.New("JSON nesting limit")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] || key == "__proto__" {
					return errors.New("duplicate or unsafe JSON key")
				}
				seen[key] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		case json.Delim('['):
			for d.More() {
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		}
		return nil
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func validateManifest(m Manifest) error {
	if m.Format != "caelis-content" || m.FormatVersion != 1 {
		return errors.New("unsupported content format (requires caelis-content v1)")
	}
	if !packPattern.MatchString(m.ID) || !versionPattern.MatchString(m.Version) || !textOK(m.Name, 100) || !textOK(m.Author, 100) || !textOK(m.License, 120) {
		return errors.New("invalid package identity or attribution")
	}
	if len(m.Files) == 0 || len(m.Files) > MaxFiles || len(m.Characters) > 16 || len(m.Avatars) > 32 || len(m.Characters)+len(m.Avatars) == 0 {
		return errors.New("content count limit")
	}
	files := map[string]bool{}
	var total int64
	for _, f := range m.Files {
		lower := strings.ToLower(f.Path)
		if !validPath(f.Path) || lower == "manifest.json" || files[lower] || len(f.SHA256) != 64 || f.SHA256 != strings.ToLower(f.SHA256) || f.Size <= 0 || f.Size > MaxFile {
			return fmt.Errorf("invalid file: %s", f.Path)
		}
		if _, e := hex.DecodeString(f.SHA256); e != nil {
			return e
		}
		switch path.Ext(f.Path) {
		case ".glb", ".png", ".txt":
		default:
			return errors.New("only GLB, PNG and TXT content is supported")
		}
		files[lower] = true
		total += f.Size
	}
	if total > MaxPack {
		return errors.New("unpacked content exceeds 64 MiB")
	}
	// References are case-sensitive even on case-insensitive filesystems.
	refs := map[string]bool{}
	require := func(p, ext string) bool {
		for _, f := range m.Files {
			if f.Path == p && path.Ext(p) == ext {
				refs[p] = true
				return true
			}
		}
		return false
	}
	if !require(m.LicenseFile, ".txt") {
		return errors.New("licenseFile must reference a bundled TXT license")
	}
	avatarIDs := map[string]bool{}
	for _, a := range m.Avatars {
		if !idPattern.MatchString(a.ID) || avatarIDs[a.ID] || !textOK(a.Name, 100) || a.Capability != "png-v1" || !require(a.Image, ".png") {
			return errors.New("invalid avatar or unsupported capability")
		}
		avatarIDs[a.ID] = true
	}
	charIDs := map[string]bool{}
	for _, c := range m.Characters {
		if !idPattern.MatchString(c.ID) || charIDs[c.ID] || !textOK(c.Name, 100) || len(c.Variants) == 0 || len(c.Variants) > 16 {
			return errors.New("invalid character")
		}
		charIDs[c.ID] = true
		variants := map[string]bool{}
		for _, v := range c.Variants {
			if !idPattern.MatchString(v.ID) || variants[v.ID] || !textOK(v.Name, 100) || v.Capability != "basic-3d-v1" || !require(v.Model, ".glb") || (v.Avatar != "" && !avatarIDs[v.Avatar]) {
				return errors.New("invalid variant or unsupported capability")
			}
			variants[v.ID] = true
		}
	}
	for _, f := range m.Files {
		if !refs[f.Path] {
			return fmt.Errorf("unreferenced file: %s", f.Path)
		}
	}
	return nil
}
func Verify(p *Pack) error {
	if e := validateManifest(p.Manifest); e != nil {
		return e
	}
	if len(p.Files) != len(p.Manifest.Files) {
		return errors.New("unlisted or missing files")
	}
	for _, f := range p.Manifest.Files {
		b, ok := p.Files[f.Path]
		if !ok || int64(len(b)) != f.Size || Hash(b) != f.SHA256 {
			return fmt.Errorf("file integrity mismatch: %s", f.Path)
		}
		var e error
		switch path.Ext(f.Path) {
		case ".glb":
			e = validateGLB(b)
		case ".png":
			e = validateImage(b, "png")
		case ".txt":
			if len(b) > 65536 || !utf8.Valid(b) {
				e = errors.New("invalid license text")
			}
		}
		if e != nil {
			return fmt.Errorf("%s: %w", f.Path, e)
		}
	}
	return nil
}
