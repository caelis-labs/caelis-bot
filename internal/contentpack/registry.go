package contentpack

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type Selection struct {
	Character string `json:"character"`
	Avatar    string `json:"avatar"`
}
type Appearance struct {
	Revision  uint64    `json:"revision"`
	Selection Selection `json:"selection"`
	Model     string    `json:"model"`
	Avatar    string    `json:"avatar"`
	Basic     bool      `json:"basic"`
	Key       string    `json:"key"`
}
type Choice struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Pack  string `json:"pack"`
	Group string `json:"group"`
}
type Installed struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Author  string `json:"author"`
	Version string `json:"version"`
	License string `json:"license"`
	Active  bool   `json:"active"`
}
type State struct {
	Appearance Appearance  `json:"appearance"`
	Characters []Choice    `json:"characters"`
	Avatars    []Choice    `json:"avatars"`
	Packs      []Installed `json:"packs"`
	Notice     string      `json:"notice"`
}
type entry struct {
	Digest   string
	Manifest Manifest
	Size     int64
}
type Registry struct {
	cacheMu    sync.Mutex
	cache      map[string]*Pack
	cacheOrder []string

	mu        sync.Mutex
	root      string
	entries   map[string]entry
	selection Selection
	revision  uint64
	notice    string
}

func defaultSelection() Selection { return Selection{Character: "builtin:caelis", Avatar: "follow"} }
func NewRegistry(root string) (*Registry, error) {
	if e := os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	r := &Registry{root: root, entries: map[string]entry{}, selection: defaultSelection(), revision: 1}
	files, e := os.ReadDir(root)
	if e != nil {
		return nil, e
	}
	var total int64
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".caelispack") {
			continue
		}
		if len(r.entries) >= 32 {
			r.notice = "内容包数量已达到上限"
			break
		}
		p, b, e := ReadFile(filepath.Join(root, f.Name()))
		if e != nil || f.Name() != Hash(b)+".caelispack" {
			r.notice = "部分内容包无法读取，已使用可用外观"
			continue
		}
		total += int64(len(b))
		if total > 256<<20 {
			r.notice = "内容包总量已达到上限"
			break
		}
		key := packageKey(p.Manifest)
		if _, ok := r.entries[key]; ok {
			r.notice = "已忽略重复版本的内容包"
			continue
		}
		r.entries[key] = entry{Hash(b), p.Manifest, int64(len(b))}
	}
	if b, e := readRegular(filepath.Join(root, "selection.json"), 4096); e == nil {
		var s Selection
		if StrictJSON(b, &s) == nil {
			r.selection = s
		}
	}
	if !r.validSelection(r.selection) {
		r.selection = defaultSelection()
		r.notice = "原外观不可用，已恢复内置形象"
	}
	return r, nil
}
func packageKey(m Manifest) string          { return m.ID + "@" + m.Version }
func characterKey(pack, c, v string) string { return pack + "/character/" + c + "/" + v }
func avatarKey(pack, a string) string       { return pack + "/avatar/" + a }
func assetURL(e entry, p string) string     { return "/content/" + e.Digest + "/" + p }
func (r *Registry) choices() (map[string]struct{ model, avatar string }, map[string]string) {
	chars := map[string]struct{ model, avatar string }{"builtin:caelis": {}, "builtin:stick": {model: "/models/stick.glb"}}
	avatars := map[string]string{"follow": "", "builtin:avatar": ""}
	for key, e := range r.entries {
		local := map[string]string{}
		for _, a := range e.Manifest.Avatars {
			url := assetURL(e, a.Image)
			local[a.ID] = url
			avatars[avatarKey(key, a.ID)] = url
		}
		for _, c := range e.Manifest.Characters {
			for _, v := range c.Variants {
				chars[characterKey(key, c.ID, v.ID)] = struct{ model, avatar string }{assetURL(e, v.Model), local[v.Avatar]}
			}
		}
	}
	return chars, avatars
}
func (r *Registry) validSelection(s Selection) bool {
	chars, avatars := r.choices()
	_, c := chars[s.Character]
	_, a := avatars[s.Avatar]
	return c && a
}
func (r *Registry) snapshot() State {
	chars, avatars := r.choices()
	selected := chars[r.selection.Character]
	avatar := selected.avatar
	if r.selection.Avatar != "follow" {
		avatar = avatars[r.selection.Avatar]
	}
	s := State{Appearance: Appearance{r.revision, r.selection, selected.model, avatar, r.selection.Character != "builtin:caelis", r.selection.Character}, Characters: []Choice{{"builtin:caelis", "Caelis", "", "内置"}, {"builtin:stick", "Stick", "", "内置"}}, Avatars: []Choice{{"follow", "跟随角色", "", ""}, {"builtin:avatar", "Caelis Bot", "", "内置"}}, Packs: []Installed{}, Notice: r.notice}
	keys := make([]string, 0, len(r.entries))
	for key := range r.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		e := r.entries[key]
		m := e.Manifest
		active := strings.HasPrefix(r.selection.Character, key+"/") || strings.HasPrefix(r.selection.Avatar, key+"/")
		s.Packs = append(s.Packs, Installed{key, m.Name, m.Author, m.Version, m.License, active})
		for _, c := range m.Characters {
			for _, v := range c.Variants {
				s.Characters = append(s.Characters, Choice{characterKey(key, c.ID, v.ID), c.Name + " · " + v.Name, key, m.Name + " · " + m.Version})
			}
		}
		for _, a := range m.Avatars {
			s.Avatars = append(s.Avatars, Choice{avatarKey(key, a.ID), a.Name, key, m.Name + " · " + m.Version})
		}
	}
	return s
}
func (r *Registry) State() State { r.mu.Lock(); defer r.mu.Unlock(); return r.snapshot() }
func (r *Registry) read(e entry) (*Pack, error) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if p := r.cache[e.Digest]; p != nil {
		return p, nil
	}
	p, b, err := ReadFile(filepath.Join(r.root, e.Digest+".caelispack"))
	if err == nil && Hash(b) != e.Digest {
		err = errors.New("installed content changed")
	}
	if err == nil {
		if r.cache == nil {
			r.cache = map[string]*Pack{}
		}
		if len(r.cacheOrder) == 2 {
			delete(r.cache, r.cacheOrder[0])
			r.cacheOrder = r.cacheOrder[1:]
		}
		r.cache[e.Digest] = p
		r.cacheOrder = append(r.cacheOrder, e.Digest)
	}
	return p, err
}
func (r *Registry) Select(s Selection) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.validSelection(s) {
		return r.snapshot(), errors.New("外观不存在，请重新选择")
	}
	for key, e := range r.entries {
		if strings.HasPrefix(s.Character, key+"/") || strings.HasPrefix(s.Avatar, key+"/") {
			if _, err := r.read(e); err != nil {
				return r.snapshot(), errors.New("内容包已损坏，请重新导入")
			}
		}
	}
	if err := localstate.Write(filepath.Join(r.root, "selection.json"), s); err != nil {
		return r.snapshot(), err
	}
	r.selection = s
	r.revision++
	r.notice = ""
	return r.snapshot(), nil
}

// Fallback is fenced by the snapshot which actually failed in the renderer.
func (r *Registry) Fallback(revision uint64) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if revision != r.revision {
		return r.snapshot(), nil
	}
	s := defaultSelection()
	r.notice = "外观加载失败，已恢复内置形象"
	if err := localstate.Write(filepath.Join(r.root, "selection.json"), s); err != nil {
		r.notice = "已临时恢复内置形象，但无法保存此选择"
	}
	r.selection = s
	r.revision++
	return r.snapshot(), nil
}
func (r *Registry) Install(name string) (State, error) {
	// Parsing stays outside the registry lock; requests are bounded by MaxPack.
	p, b, err := ReadFile(name)
	if err != nil {
		return r.State(), err
	}
	digest := Hash(b)
	key := packageKey(p.Manifest)
	r.mu.Lock()
	defer r.mu.Unlock()
	_, replacing := r.entries[key]
	if old, ok := r.entries[key]; ok {
		if old.Digest != digest {
			return r.snapshot(), errors.New("同一版本已有不同内容，请让作者提升版本号")
		}
		if stored, e := readRegular(filepath.Join(r.root, old.Digest+".caelispack"), MaxPack); e == nil && Hash(stored) == old.Digest {
			return r.snapshot(), nil
		}
	}
	total := int64(len(b))
	for existing, e := range r.entries {
		if existing != key {
			total += e.Size
		}
	}
	if (!replacing && len(r.entries) >= 32) || total > 256<<20 {
		return r.snapshot(), errors.New("内容包存储上限为 32 个版本 / 256 MiB")
	}
	f, err := os.CreateTemp(r.root, ".install-*")
	if err != nil {
		return r.snapshot(), err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err == nil {
		err = closed
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(r.root, digest+".caelispack"))
	}
	if err != nil {
		return r.snapshot(), err
	}
	r.entries[key] = entry{digest, p.Manifest, int64(len(b))}
	r.revision++
	r.notice = ""
	return r.snapshot(), nil
}
func (r *Registry) Remove(key string) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[key]
	if !ok {
		return r.snapshot(), errors.New("内容包不存在")
	}
	if strings.HasPrefix(r.selection.Character, key+"/") || strings.HasPrefix(r.selection.Avatar, key+"/") {
		return r.snapshot(), errors.New("请先切换正在使用的角色或头像")
	}
	if err := os.Remove(filepath.Join(r.root, e.Digest+".caelispack")); err != nil && !os.IsNotExist(err) {
		return r.snapshot(), err
	}
	delete(r.entries, key)
	r.revision++
	return r.snapshot(), nil
}

// Handler exposes validated inert bytes only. It never serves arbitrary paths,
// archives, licenses as HTML, directory listings, or caller-supplied URLs.
func (r *Registry) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if !strings.HasPrefix(q.URL.Path, "/content/") {
			next.ServeHTTP(w, q)
			return
		}
		if q.Method != "GET" && q.Method != "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(q.URL.Path, "/content/"), "/", 2)
		if len(parts) != 2 || !validPath(parts[1]) {
			http.NotFound(w, q)
			return
		}
		r.mu.Lock()
		var found entry
		for _, e := range r.entries {
			if e.Digest == parts[0] {
				found = e
				break
			}
		}
		r.mu.Unlock()
		if found.Digest == "" {
			http.NotFound(w, q)
			return
		}
		p, err := r.read(found)
		if err != nil {
			http.NotFound(w, q)
			return
		}
		b, ok := p.Files[parts[1]]
		if !ok {
			http.NotFound(w, q)
			return
		}
		kind := ""
		switch path.Ext(parts[1]) {
		case ".glb":
			kind = "model/gltf-binary"
		case ".png":
			kind = "image/png"
		default:
			http.NotFound(w, q)
			return
		}
		w.Header().Set("Content-Type", kind)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, q, parts[1], time.Time{}, bytes.NewReader(b))
	})
}

// WriteSource produces an editable starter without overwriting existing work.
func WriteSource(dir string, p *Pack) error {
	if _, e := os.Lstat(dir); e == nil {
		return errors.New("output directory already exists")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	for name, b := range p.Files {
		if !validPath(name) {
			return errors.New("invalid source path")
		}
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		if e := os.WriteFile(dest, b, 0600); e != nil {
			return e
		}
	}
	b, e := json.MarshalIndent(p.Manifest, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0600)
}
func WriteArchive(name string, b []byte) error {
	f, e := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(f, bytes.NewReader(b))
	if e == nil {
		e = f.Sync()
	}
	closed := f.Close()
	if e != nil {
		os.Remove(name)
		return e
	}
	return closed
}
