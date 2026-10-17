package windowsupdate

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Length int64  `json:"length"`
}
type Portable struct {
	Schema   int    `json:"schema"`
	Identity string `json:"identity"`
	Version  string `json:"version"`
	Files    []File `json:"files"`
}

func safeName(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.TrimRight(part, ". ") != part {
			return false
		}
		base := strings.ToUpper(strings.Split(part, ".")[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9') {
			return false
		}
	}
	return true
}
func safeDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("absolute program directory required")
	}
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("redirected program directory refused")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func readPortable(path string) (Portable, error) {
	var p Portable
	b, e := os.ReadFile(path)
	if e != nil {
		return p, e
	}
	if len(b) > 2<<20 || json.Unmarshal(b, &p) != nil || p.Schema != 1 || (p.Identity != "dev.caelis.bot.preview" && p.Identity != "dev.caelis.bot.dev" && p.Identity != "dev.caelis.bot") {
		return p, errors.New("invalid portable ownership")
	}
	seen := map[string]bool{}
	for _, f := range p.Files {
		key := strings.ToLower(f.Name)
		if !safeName(f.Name) || f.Name == "portable.json" || seen[key] || !validHash(f.SHA256, 32) || f.Length < 0 || f.Length > 512<<20 {
			return p, errors.New("invalid portable inventory")
		}
		seen[key] = true
	}
	if !seen["caelis-bot.exe"] || !seen["build.json"] {
		return p, errors.New("portable payload incomplete")
	}
	return p, nil
}
func verifyFile(path string, f File) error {
	return VerifyArchive(path, Artifact{SHA256: f.SHA256, Length: f.Length})
}
func Unpack(path, stage, identity, version string) (Portable, error) {
	var none Portable
	if e := safeDirectory(stage); e != nil {
		return none, e
	}
	if e := os.Mkdir(stage, 0700); e != nil {
		return none, e
	}
	reader, e := zip.OpenReader(path)
	if e != nil {
		return none, e
	}
	defer reader.Close()
	seen := map[string]bool{}
	var total uint64
	for _, f := range reader.File {
		name := strings.TrimSuffix(f.Name, "/")
		key := strings.ToLower(name)
		if !safeName(name) || seen[key] || f.Mode()&os.ModeSymlink != 0 || f.Mode()&os.ModeType != 0 && !f.FileInfo().IsDir() {
			return none, errors.New("unsafe ZIP entry")
		}
		seen[key] = true
		total += f.UncompressedSize64
		if total > 512<<20 {
			return none, errors.New("ZIP payload too large")
		}
		target := filepath.Join(stage, filepath.FromSlash(name))
		if f.FileInfo().IsDir() {
			if e = os.MkdirAll(target, 0700); e != nil {
				return none, e
			}
			continue
		}
		if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			return none, e
		}
		in, e := f.Open()
		if e != nil {
			return none, e
		}
		out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			in.Close()
			return none, e
		}
		n, copyErr := io.Copy(out, io.LimitReader(in, int64(f.UncompressedSize64)+1))
		in.Close()
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil || uint64(n) != f.UncompressedSize64 {
			return none, errors.New("ZIP extraction incomplete")
		}
	}
	p, e := readPortable(filepath.Join(stage, "portable.json"))
	if e != nil || p.Identity != identity || p.Version != version {
		return none, errors.New("ZIP identity/version mismatch")
	}
	allowed := map[string]bool{"portable.json": true}
	for _, f := range p.Files {
		allowed[f.Name] = true
		if e = verifyFile(filepath.Join(stage, filepath.FromSlash(f.Name)), f); e != nil {
			return none, e
		}
	}
	for _, f := range reader.File {
		if !f.FileInfo().IsDir() && !allowed[f.Name] {
			return none, errors.New("unowned ZIP file")
		}
	}
	return p, nil
}

// ReplacePortable operates only on inventory-owned files. Foreign files,
// Notebook and credentials are never adopted. A refused/in-use image fails
// before replacement; later I/O failures roll back the files already moved.
func ReplacePortable(target, stage, backup, identity, version string) error {
	if e := safeDirectory(target); e != nil {
		return e
	}
	if e := safeDirectory(stage); e != nil {
		return e
	}
	if e := safeDirectory(backup); e != nil {
		return e
	}
	old, e := readPortable(filepath.Join(target, "portable.json"))
	if e != nil || old.Identity != identity {
		return errors.New("portable installation ownership mismatch")
	}
	next, e := readPortable(filepath.Join(stage, "portable.json"))
	if e != nil || next.Identity != identity || next.Version != version {
		return errors.New("portable update ownership mismatch")
	}
	oldMap := map[string]File{}
	for _, f := range old.Files {
		oldMap[strings.ToLower(f.Name)] = f
		if e = safeDirectory(filepath.Dir(filepath.Join(target, filepath.FromSlash(f.Name)))); e != nil {
			return e
		}
		if e = verifyFile(filepath.Join(target, filepath.FromSlash(f.Name)), f); e != nil {
			return errors.New("owned portable file changed; manual update required")
		}
	}
	for _, f := range next.Files {
		if e = verifyFile(filepath.Join(stage, filepath.FromSlash(f.Name)), f); e != nil {
			return e
		}
		if e = safeDirectory(filepath.Dir(filepath.Join(target, filepath.FromSlash(f.Name)))); e != nil {
			return e
		}
		if _, known := oldMap[strings.ToLower(f.Name)]; !known {
			if _, e = os.Lstat(filepath.Join(target, filepath.FromSlash(f.Name))); !os.IsNotExist(e) {
				return errors.New("update would overwrite a foreign file")
			}
		}
	}
	if e = os.Mkdir(backup, 0700); e != nil {
		return e
	}
	moved, installed := []string{}, []string{}
	rollback := func() {
		for i := len(installed) - 1; i >= 0; i-- {
			_ = os.Remove(filepath.Join(target, filepath.FromSlash(installed[i])))
		}
		for i := len(moved) - 1; i >= 0; i-- {
			name := moved[i]
			_ = os.MkdirAll(filepath.Dir(filepath.Join(target, filepath.FromSlash(name))), 0700)
			_ = os.Rename(filepath.Join(backup, filepath.FromSlash(name)), filepath.Join(target, filepath.FromSlash(name)))
		}
	}
	names := []string{"portable.json"}
	for _, f := range old.Files {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	// Move the executable first: a running Bot/MCP child refuses this write.
	sort.SliceStable(names, func(i, j int) bool { return names[i] == "caelis-bot.exe" && names[j] != "caelis-bot.exe" })
	for _, name := range names {
		from := filepath.Join(target, filepath.FromSlash(name))
		to := filepath.Join(backup, filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(to), 0700); e == nil {
			e = os.Rename(from, to)
		}
		if e != nil {
			rollback()
			return e
		}
		moved = append(moved, name)
	}
	names = []string{}
	for _, f := range next.Files {
		names = append(names, f.Name)
	}
	names = append(names, "portable.json")
	for _, name := range names {
		to := filepath.Join(target, filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(to), 0700); e == nil {
			e = os.Rename(filepath.Join(stage, filepath.FromSlash(name)), to)
		}
		if e != nil {
			rollback()
			return e
		}
		installed = append(installed, name)
	}
	return nil
}
func Digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
