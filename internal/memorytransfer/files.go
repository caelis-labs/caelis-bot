package memorytransfer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const maxFile = 512 << 20
const maxBundle = 1 << 30
const maxEntries = 10000

func digest(body []byte) string           { h := sha256.Sum256(body); return hex.EncodeToString(h[:]) }
func jsonBytes(value any) ([]byte, error) { return json.MarshalIndent(value, "", "  ") }
func decode(body []byte, value any, strict bool) error {
	d := json.NewDecoder(bytes.NewReader(body))
	if strict {
		d.DisallowUnknownFields()
	}
	if err := d.Decode(value); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("extra JSON data")
	}
	return nil
}

func safePath(p string) bool {
	if p == "" || p == "." || strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func requireDirectory(p string, private bool) error {
	i, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("directory cannot be redirected: %s", p)
	}
	if private && i.Mode().Perm()&0077 != 0 || i.Mode().Perm()&0022 != 0 {
		return errors.New("unsafe directory permissions")
	}
	return nil
}

func checkTree(root string, private bool) error {
	if err := requireDirectory(root, private); err != nil {
		return err
	}
	count := 0
	return filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > maxEntries {
			return errors.New("too many bundle entries")
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if e.Type()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("only real directories and regular files may be transferred")
		}
		if private && info.Mode().Perm()&0077 != 0 || info.Mode().Perm()&0022 != 0 {
			return errors.New("unsafe file permissions")
		}
		return nil
	})
}

func readRegular(p string, private bool) ([]byte, error) {
	i, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() || private && i.Mode().Perm()&0077 != 0 || i.Mode().Perm()&0022 != 0 {
		return nil, errors.New("unsafe regular file or permissions")
	}
	if i.Size() > maxFile {
		return nil, errors.New("file exceeds offline bundle limit")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if len(body) > maxFile {
		return nil, errors.New("file exceeds offline bundle limit")
	}
	return body, err
}

func createFile(root, p string) (*os.File, error) {
	if !safePath(p) {
		return nil, errors.New("unsafe bundle path")
	}
	dst := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return nil, err
	}
	return os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
}

func writeFile(root, p string, body []byte) error {
	f, err := createFile(root, p)
	if err != nil {
		return err
	}
	_, err = f.Write(body)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func writeJSON(root, p string, value any) error {
	body, err := jsonBytes(value)
	if err != nil {
		return err
	}
	return writeFile(root, p, body)
}

func listFiles(root string) ([]File, error) {
	files := []File{}
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		body, err := readRegular(p, true)
		if err != nil {
			return err
		}
		files = append(files, File{filepath.ToSlash(rel), int64(len(body)), digest(body)})
		return nil
	})
	return files, err
}

func distinctPaths(a, b string) error {
	if !filepath.IsAbs(a) || !filepath.IsAbs(b) || filepath.Clean(a) != a || filepath.Clean(b) != b {
		return errors.New("use clean absolute paths")
	}
	resolve := func(p string) (string, error) {
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			parent, err := filepath.EvalSymlinks(filepath.Dir(p))
			if err != nil {
				return "", err
			}
			return filepath.Join(parent, filepath.Base(p)), nil
		} else if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(p)
	}
	var err error
	if a, err = resolve(a); err != nil {
		return err
	}
	if b, err = resolve(b); err != nil {
		return err
	}
	if a == b || strings.HasPrefix(a, b+string(os.PathSeparator)) || strings.HasPrefix(b, a+string(os.PathSeparator)) {
		return errors.New("source and destination must be separate directory trees")
	}
	return nil
}

// reserve serializes offline importers targeting one path. It does not replace
// the operator's obligation to stop other processes/direct writers.
func reserve(destination, prefix string) (string, func(), error) {
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return "", nil, errors.New("destination must be absent; an existing profile is preserved")
	}
	parent := filepath.Dir(destination)
	if err := requireDirectory(parent, false); err != nil {
		return "", nil, err
	}
	lock := filepath.Join(parent, ".memory-transfer-"+digest([]byte(filepath.Base(destination)))+".lock")
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", nil, fmt.Errorf("another transfer targets this destination or its lock needs inspection: %w", err)
	}
	if err = f.Close(); err != nil {
		os.Remove(lock)
		return "", nil, err
	}
	release := func() { _ = os.Remove(lock) }
	stage, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		release()
		return "", nil, err
	}
	return stage, release, nil
}

func activate(stage, destination string) error {
	// Files are synced when written. Sync the newly created directory entries
	// before installing the entire profile with one parent-directory rename.
	if err := filepath.WalkDir(stage, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		return errors.Join(f.Sync(), f.Close())
	}); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("destination appeared during transfer; inactive staging retained")
	}
	if err := os.Rename(stage, destination); err != nil {
		return err
	}
	// Sync the parent so the atomic installation is durable before reporting it.
	f, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return errors.Join(err, os.Rename(destination, stage))
	}
	defer f.Close()
	if err = f.Sync(); err != nil {
		return errors.Join(err, os.Rename(destination, stage))
	}
	return nil
}

func sourceFiles(root string, attachments []string) (map[string][]byte, Identity, personalIndex, error) {
	files := map[string][]byte{}
	var identity Identity
	var index personalIndex
	load := func(p string, value any) error {
		body, err := readRegular(filepath.Join(root, filepath.FromSlash(p)), false)
		if err != nil {
			return err
		}
		if len(body) > 2<<20 {
			return errors.New("state file exceeds limit")
		}
		return decode(body, value, false)
	}
	var state struct {
		Version         int    `json:"version"`
		PersonalVersion int    `json:"personalVersion"`
		ID              string `json:"id"`
		Wake            *struct {
			Status string `json:"status"`
		} `json:"wake"`
	}
	if err := load("bot.json", &state); err != nil {
		return nil, identity, index, err
	}
	if state.Version != 1 || state.PersonalVersion < 0 || state.PersonalVersion > 1 || strings.TrimSpace(state.ID) == "" || len(state.ID) > 256 {
		return nil, identity, index, errors.New("unsupported or missing Bot identity")
	}
	if state.Wake != nil && state.Wake.Status != "accepted" {
		return nil, identity, index, errors.New("resolve the source Bot's pending or unknown wake before export")
	}
	identity = Identity{state.Version, state.PersonalVersion, state.ID, []struct{}{}}
	body, err := jsonBytes(identity)
	if err != nil {
		return nil, identity, index, err
	}
	files["bot.json"] = body
	if err = load("personal/index.json", &index); err != nil {
		return nil, identity, index, err
	}
	if index.Version != 1 || index.BotID != identity.ID || index.Receipts == nil || len(index.Receipts) > maxEntries {
		return nil, identity, index, errors.New("personal index and Bot identity do not match")
	}
	body, err = jsonBytes(index)
	if err != nil {
		return nil, identity, index, err
	}
	files["personal/index.json"] = body
	marker := struct {
		Version int `json:"version"`
	}{1}
	if _, err = os.Lstat(filepath.Join(root, "notebook-migration.json")); err == nil {
		if err = load("notebook-migration.json", &marker); err != nil || marker.Version != 1 {
			return nil, identity, index, errors.New("unsupported Notebook migration marker")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, identity, index, err
	}
	files["notebook-migration.json"], _ = jsonBytes(marker)
	var intro struct {
		Version int    `json:"version"`
		ID      string `json:"id,omitempty"`
		Status  string `json:"status"`
	}
	if _, err = os.Lstat(filepath.Join(root, "bot-initialization.json")); err == nil {
		if err = load("bot-initialization.json", &intro); err != nil || intro.Version != 1 {
			return nil, identity, index, errors.New("unsupported initialization marker")
		}
		if intro.Status == "accepted" && intro.ID != "" {
			files["bot-initialization.json"], _ = jsonBytes(intro)
		} else if intro.Status != "required" {
			return nil, identity, index, errors.New("complete or reconcile the source introduction before export")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, identity, index, err
	}
	notes := filepath.Join(root, "Notebook")
	if err = checkTree(notes, false); err != nil {
		return nil, identity, index, err
	}
	err = filepath.WalkDir(notes, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(notes, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "INDEX.md" || !strings.EqualFold(path.Ext(rel), ".md") {
			return nil
		}
		if !safePath(rel) {
			return errors.New("authored Markdown has an unsupported hidden or unsafe path")
		}
		body, err := readRegular(p, false)
		if err != nil {
			return err
		}
		files["Notebook/"+rel] = body
		return nil
	})
	if err != nil {
		return nil, identity, index, err
	}
	for _, rel := range attachments {
		if !allowedAttachment(rel) {
			return nil, identity, index, errors.New("attachment is not on the nonsecret Notebook allowlist")
		}
		if _, exists := files["Notebook/"+rel]; exists {
			return nil, identity, index, errors.New("duplicate attachment")
		}
		body, err := readRegular(filepath.Join(notes, filepath.FromSlash(rel)), false)
		if err != nil {
			return nil, identity, index, err
		}
		files["Notebook/"+rel] = body
	}
	if err = checkReferences(files, attachments); err != nil {
		return nil, identity, index, err
	}
	return files, identity, index, nil
}

var markdownReference = regexp.MustCompile(`!?\[[^\]\n]*\]\(<?([^\s)>]+)>?(?:\s+"[^"\n]*")?\)`)
var markdownDefinition = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]\n]+\]:\s*<?([^\s>]+)>?`)
var htmlReference = regexp.MustCompile(`(?i)\b(?:src|href)\s*=\s*["']([^"']+)["']`)
var attachmentExtensions = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".pdf": true, ".txt": true, ".csv": true, ".mp3": true, ".mp4": true, ".wav": true}

func allowedAttachment(p string) bool {
	if !safePath(p) || !attachmentExtensions[strings.ToLower(path.Ext(p))] {
		return false
	}
	for _, component := range strings.Split(strings.ToLower(p), "/") {
		for _, secret := range []string{"credential", "token", "secret", "id_rsa", "id_ed25519", "authorized_keys", "known_hosts"} {
			if strings.Contains(component, secret) {
				return false
			}
		}
	}
	return true
}

func checkReferences(files map[string][]byte, attachments []string) error {
	if _, ok := files["Notebook/MEMORY.md"]; !ok {
		return errors.New("Notebook/MEMORY.md is required")
	}
	referenced := map[string]bool{}
	for p, body := range files {
		if !strings.HasPrefix(p, "Notebook/") || !strings.EqualFold(path.Ext(p), ".md") {
			continue
		}
		matches := append(markdownReference.FindAllSubmatch(body, -1), markdownDefinition.FindAllSubmatch(body, -1)...)
		matches = append(matches, htmlReference.FindAllSubmatch(body, -1)...)
		for _, match := range matches {
			u, err := url.Parse(string(match[1]))
			if err != nil {
				return errors.New("invalid Notebook reference")
			}
			if u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "mailto" || u.Path == "" {
				continue
			}
			if u.Scheme != "" || u.Host != "" || strings.HasPrefix(u.Path, "/") || strings.Contains(u.Path, "\\") {
				return errors.New("Notebook has an external local reference; place required nonsecret attachments inside Notebook")
			}
			target := path.Clean(path.Join(path.Dir(p), u.Path))
			if !strings.HasPrefix(target, "Notebook/") || !safePath(target) {
				return errors.New("Notebook reference escapes its directory")
			}
			if target == "Notebook/INDEX.md" {
				continue
			}
			if _, exists := files[target]; !exists {
				return fmt.Errorf("Notebook reference is missing from the allowlist: %s", target)
			}
			referenced[strings.TrimPrefix(target, "Notebook/")] = true
		}
	}
	for _, p := range attachments {
		if !referenced[p] {
			return errors.New("allowlisted attachment is not referenced by Notebook Markdown")
		}
	}
	return nil
}

func validateBundle(root string) (Manifest, map[string][]byte, personalIndex, error) {
	var m Manifest
	var index personalIndex
	if err := checkTree(root, true); err != nil {
		return m, nil, index, err
	}
	body, err := readRegular(filepath.Join(root, "manifest.json"), true)
	if err != nil {
		return m, nil, index, err
	}
	if len(body) > 2<<20 {
		return m, nil, index, errors.New("manifest exceeds limit")
	}
	if err = decode(body, &m, true); err != nil {
		return m, nil, index, err
	}
	if m.Format != Format || m.BotID == "" || m.Scope != scopeFor(m.BotID) || m.SchemaVersion < 1 || len(m.Files) > maxEntries {
		return m, nil, index, errors.New("unsupported bundle version or identity")
	}
	attachments := map[string]bool{}
	for _, p := range m.Attachments {
		if !allowedAttachment(p) || attachments[p] {
			return m, nil, index, errors.New("unsafe or duplicate attachment allowlist")
		}
		attachments[p] = true
	}
	files := map[string][]byte{}
	var total int64
	for _, file := range m.Files {
		if !safePath(file.Path) || file.Size < 0 || file.Size > maxFile || len(file.SHA256) != 64 {
			return m, nil, index, errors.New("invalid file manifest")
		}
		if _, duplicate := files[file.Path]; duplicate {
			return m, nil, index, errors.New("duplicate file manifest")
		}
		allowed := false
		switch file.Path {
		case "bot.json", "personal/index.json", "personal/memory/memory.db", "notebook-migration.json", "bot-initialization.json":
			allowed = true
		default:
			if strings.HasPrefix(file.Path, "Notebook/") && file.Path != "Notebook/INDEX.md" {
				rel := strings.TrimPrefix(file.Path, "Notebook/")
				allowed = strings.EqualFold(path.Ext(rel), ".md") || attachments[rel]
			}
		}
		if !allowed {
			return m, nil, index, errors.New("bundle contains a file outside the memory allowlist")
		}
		total += file.Size
		if total > maxBundle {
			return m, nil, index, errors.New("bundle exceeds offline import limit")
		}
		body, err := readRegular(filepath.Join(root, filepath.FromSlash(file.Path)), true)
		if err != nil {
			return m, nil, index, err
		}
		if int64(len(body)) != file.Size || digest(body) != file.SHA256 {
			return m, nil, index, errors.New("bundle file size or digest mismatch")
		}
		files[file.Path] = body
	}
	// No credential, cache or surprise file may hide outside the manifest.
	err = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != "manifest.json" {
			if _, ok := files[rel]; !ok {
				return errors.New("unlisted file in bundle")
			}
		}
		return nil
	})
	if err != nil {
		return m, nil, index, err
	}
	for _, p := range []string{"bot.json", "personal/index.json", "personal/memory/memory.db", "Notebook/MEMORY.md", "notebook-migration.json"} {
		if _, ok := files[p]; !ok {
			return m, nil, index, fmt.Errorf("necessary file missing: %s", p)
		}
	}
	var identity Identity
	if err = decode(files["bot.json"], &identity, true); err != nil || identity.Version != 1 || identity.PersonalVersion < 0 || identity.PersonalVersion > 1 || identity.ID != m.BotID || identity.Schedules == nil || len(identity.Schedules) != 0 {
		return m, nil, index, errors.New("bundle Bot identity is invalid or contains schedules")
	}
	if err = decode(files["personal/index.json"], &index, true); err != nil || index.Version != 1 || index.BotID != m.BotID || index.Receipts == nil || len(index.Receipts) > maxEntries {
		return m, nil, index, errors.New("bundle personal index does not match Bot identity")
	}
	if len(m.ReceiptDigests) != len(index.Receipts) {
		return m, nil, index, errors.New("bundle receipt manifest does not match personal index")
	}
	seen := map[string]bool{}
	for _, id := range index.Receipts {
		if id == "" || seen[id] || len(m.ReceiptDigests[id]) != 64 {
			return m, nil, index, errors.New("bundle receipt manifest is invalid")
		}
		seen[id] = true
	}
	var marker struct {
		Version int `json:"version"`
	}
	if err = decode(files["notebook-migration.json"], &marker, true); err != nil || marker.Version != 1 {
		return m, nil, index, errors.New("bundle migration marker is invalid")
	}
	if body, exists := files["bot-initialization.json"]; exists {
		var intro struct {
			Version int    `json:"version"`
			ID      string `json:"id"`
			Status  string `json:"status"`
		}
		if err = decode(body, &intro, true); err != nil || intro.Version != 1 || intro.ID == "" || intro.Status != "accepted" {
			return m, nil, index, errors.New("bundle must contain only a completed introduction")
		}
	}
	if err = checkReferences(files, m.Attachments); err != nil {
		return m, nil, index, err
	}
	return m, files, index, nil
}
