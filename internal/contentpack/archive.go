package contentpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ReadArchive(b []byte) (*Pack, error) {
	if len(b) > MaxPack {
		return nil, errors.New("archive exceeds 64 MiB")
	}
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil {
		return nil, e
	}
	if len(z.File) > MaxFiles+1 {
		return nil, errors.New("too many archive entries")
	}
	p := &Pack{Files: map[string][]byte{}}
	seen := map[string]bool{}
	var total uint64
	var manifest []byte
	for _, f := range z.File {
		key := strings.ToLower(f.Name)
		if !validPath(f.Name) || seen[key] || !f.Mode().IsRegular() || (f.Method != zip.Store && f.Method != zip.Deflate) {
			return nil, errors.New("invalid, duplicate or non-regular archive entry")
		}
		seen[key] = true
		limit := uint64(MaxFile)
		if f.Name == "manifest.json" {
			limit = 65536
		}
		total += f.UncompressedSize64
		if f.UncompressedSize64 > limit || total > MaxPack {
			return nil, errors.New("unpacked content limit")
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		data, e := io.ReadAll(io.LimitReader(r, int64(limit)+1))
		r.Close()
		if e != nil {
			return nil, e
		}
		if uint64(len(data)) != f.UncompressedSize64 {
			return nil, errors.New("archive size mismatch")
		}
		if f.Name == "manifest.json" {
			manifest = data
		} else {
			p.Files[f.Name] = data
		}
	}
	if e = StrictJSON(manifest, &p.Manifest); e != nil {
		return nil, fmt.Errorf("manifest: %w", e)
	}
	return p, Verify(p)
}
func readRegular(name string, limit int64) ([]byte, error) {
	info, e := os.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("expected bounded regular file")
	}
	f, e := os.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	actual, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !os.SameFile(info, actual) {
		return nil, errors.New("file changed during open")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("file limit")
	}
	return b, e
}
func ReadFile(name string) (*Pack, []byte, error) {
	b, e := readRegular(name, MaxPack)
	if e != nil {
		return nil, nil, e
	}
	p, e := ReadArchive(b)
	return p, b, e
}

// ReadDirectory refreshes receipts, but still rejects everything not listed.
func ReadDirectory(dir string) (*Pack, error) {
	raw, e := readRegular(filepath.Join(dir, "manifest.json"), 65536)
	if e != nil {
		return nil, e
	}
	p := &Pack{Files: map[string][]byte{}}
	if e = StrictJSON(raw, &p.Manifest); e != nil {
		return nil, e
	}
	// Validate paths before opening any file. The other receipts are generated below.
	allowed := map[string]bool{"manifest.json": true}
	for i := range p.Manifest.Files {
		f := &p.Manifest.Files[i]
		if !validPath(f.Path) || allowed[f.Path] {
			return nil, errors.New("invalid file path")
		}
		allowed[f.Path] = true
	}
	e = filepath.WalkDir(dir, func(name string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not content")
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		if !allowed[filepath.ToSlash(rel)] {
			return fmt.Errorf("unlisted source file: %s", rel)
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	var total int64
	for i := range p.Manifest.Files {
		f := &p.Manifest.Files[i]
		b, e := readRegular(filepath.Join(dir, filepath.FromSlash(f.Path)), MaxFile)
		if e != nil {
			return nil, e
		}
		total += int64(len(b))
		if total > MaxPack {
			return nil, errors.New("content limit")
		}
		f.Size = int64(len(b))
		f.SHA256 = Hash(b)
		p.Files[f.Path] = b
	}
	return p, Verify(p)
}
func Encode(p *Pack) ([]byte, error) {
	if e := Verify(p); e != nil {
		return nil, e
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	add := func(name string, data []byte) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0600)
		h.SetModTime(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
		w, e := z.CreateHeader(h)
		if e != nil {
			return e
		}
		_, e = w.Write(data)
		return e
	}
	m, e := json.MarshalIndent(p.Manifest, "", "  ")
	if e != nil {
		return nil, e
	}
	if e = add("manifest.json", m); e != nil {
		return nil, e
	}
	for _, f := range p.Manifest.Files {
		if e = add(f.Path, p.Files[f.Path]); e != nil {
			return nil, e
		}
	}
	if e = z.Close(); e != nil {
		return nil, e
	}
	if b.Len() > MaxPack {
		return nil, errors.New("archive limit")
	}
	return b.Bytes(), nil
}
