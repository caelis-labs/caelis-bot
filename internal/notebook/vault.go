package notebook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const IndexNotice = "<!-- 此文件由 Caelis Bot 自动生成，请勿编辑；重新生成时会覆盖此文件。 -->\n"

// Vault owns only generated navigation, never the user's editable note bodies.
type Vault struct {
	mu   sync.Mutex
	root *os.Root
	path string
}

func OpenVault(path string) (*Vault, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("笔记本需要完整路径")
	}
	_, err := os.Lstat(path)
	fresh := errors.Is(err, os.ErrNotExist)
	if err != nil && !fresh {
		return nil, err
	}
	if err = os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("笔记本目录不可重定向")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	v := &Vault{root: root, path: path}
	if fresh {
		if err = v.create("MEMORY.md", []byte("# Memory\n")); err != nil {
			root.Close()
			return nil, err
		}
	}
	if err = v.Refresh(context.Background(), time.Now()); err != nil {
		root.Close()
		return nil, err
	}
	return v, nil
}
func (v *Vault) Path() string { return v.path }
func (v *Vault) Close() error { v.mu.Lock(); defer v.mu.Unlock(); return v.root.Close() }
func (v *Vault) create(path string, body []byte) error {
	f, err := v.root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(body)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	return errors.Join(err, ce)
}
func markdownLink(path string) string { return (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath() }
func label(s string) string {
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\r", " ", "\n", " ", "<", "&lt;", ">", "&gt;").Replace(s)
}
func (v *Vault) Refresh(ctx context.Context, now time.Time) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.root.MkdirAll(now.Format("2006/01/02"), 0700); err != nil {
		return err
	}
	paths := []string{}
	err := fs.WalkDir(v.root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if path != "." && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !entry.IsDir() && entry.Type().IsRegular() && strings.HasSuffix(strings.ToLower(path), ".md") && path != "INDEX.md" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	var out strings.Builder
	out.WriteString(IndexNotice + "\n# Notebook\n\n")
	if info, e := v.root.Lstat("MEMORY.md"); e == nil && info.Mode().IsRegular() {
		out.WriteString("[核心记忆](MEMORY.md)\n\n")
	}
	for _, path := range paths {
		if path == "MEMORY.md" {
			continue
		}
		f, err := v.root.Open(path)
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(f, 8192))
		f.Close()
		if err != nil {
			return err
		}
		title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		for line := range strings.SplitSeq(string(b), "\n") {
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
				break
			}
		}
		if chars := []rune(title); len(chars) > 160 {
			title = string(chars[:160]) + "…"
		}
		fmt.Fprintf(&out, "- [%s](%s) — %s\n", label(title), markdownLink(path), label(filepath.ToSlash(path)))
	}
	body := []byte(out.String())
	if info, err := v.root.Lstat("INDEX.md"); err == nil && info.Mode().IsRegular() {
		if old, err := v.root.ReadFile("INDEX.md"); err == nil && string(old) == string(body) {
			return nil
		}
	}
	// Index is replaceable. Renaming replaces a symlink itself rather than its target.
	f, err := os.CreateTemp(v.path, ".index-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return v.root.Rename(filepath.Base(f.Name()), "INDEX.md")
}
