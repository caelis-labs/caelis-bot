package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// OpenReviewedMarketplace adapts the official local Codex marketplace index
// to Bot's portable package catalog. approved is a separately reviewed byte
// inventory; the marketplace file itself never grants trust or activation.
// The production Bot still uses its build-owned catalog until a curated source
// distribution and update channel is configured.
func OpenReviewedMarketplace(store string, source fs.FS, index []byte, approved map[string]map[string]string) (*Manager, error) {
	entries, sources, err := NormalizeMarketplace(source, index, approved)
	if err != nil {
		return nil, err
	}
	return openCatalog(store, entries, sources)
}

// NormalizeMarketplace supports the documented local source.path shape only.
// Git sources need a separate pinned fetch, review and update owner.
func NormalizeMarketplace(source fs.FS, index []byte, approved map[string]map[string]string) ([]Entry, map[string]fs.FS, error) {
	var raw struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Kind string `json:"source"`
				Path string `json:"path"`
			} `json:"source"`
			Interface struct {
				DisplayName string `json:"displayName"`
			} `json:"interface"`
			Policy struct {
				Installation   string `json:"installation"`
				Authentication string `json:"authentication"`
			} `json:"policy"`
		} `json:"plugins"`
	}
	if len(index) > 1<<20 || json.Unmarshal(index, &raw) != nil || !pluginName.MatchString(raw.Name) || len(raw.Plugins) > 128 {
		return nil, nil, errors.New("invalid marketplace index")
	}
	entries := make([]Entry, 0, len(raw.Plugins))
	sources := make(map[string]fs.FS, len(raw.Plugins))
	seenIDs := make(map[string]bool, len(raw.Plugins))
	for _, item := range raw.Plugins {
		if !pluginName.MatchString(item.Name) || item.Source.Kind != "local" || !strings.HasPrefix(item.Source.Path, "./") || item.Policy.Installation == "" || item.Policy.Authentication == "" {
			return nil, nil, errors.New("unsupported marketplace entry")
		}
		if seenIDs[item.Name] {
			return nil, nil, errors.New("duplicate marketplace package")
		}
		seenIDs[item.Name] = true
		if item.Policy.Installation == "NOT_AVAILABLE" {
			continue
		}
		folder := strings.TrimPrefix(item.Source.Path, "./")
		if !safeReviewedName(folder) || path.Clean(folder) != folder || sources[item.Name] != nil {
			return nil, nil, errors.New("unsafe marketplace source")
		}
		packageFS, err := reviewedSubdir(source, folder)
		if err != nil {
			return nil, nil, err
		}
		inventory := approved[item.Name]
		if len(inventory) == 0 {
			return nil, nil, fmt.Errorf("package %s has no reviewed inventory", item.Name)
		}
		var total int64
		seen := map[string]bool{}
		executables := []string{}
		err = fs.WalkDir(packageFS, ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if name == "." {
				return nil
			}
			if !safeReviewedName(name) || d.Type()&fs.ModeSymlink != 0 {
				return errors.New("unsafe package path")
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return errors.New("unsupported package file")
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode().Perm()&0111 != 0 {
				executables = append(executables, name)
			}
			body, err := fs.ReadFile(packageFS, name)
			if err != nil {
				return err
			}
			total += int64(len(body))
			if len(body) > 16<<20 || total > 64<<20 {
				return errors.New("package exceeds size limit")
			}
			want, ok := inventory[name]
			sum := sha256.Sum256(body)
			if !ok || !strings.EqualFold(hex.EncodeToString(sum[:]), want) {
				return errors.New("marketplace package hash mismatch")
			}
			seen[name] = true
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		if len(seen) != len(inventory) {
			return nil, nil, errors.New("marketplace package inventory mismatch")
		}
		confirmed := make(map[string]string, len(inventory))
		for name, hash := range inventory {
			confirmed[name] = hash
		}
		sort.Strings(executables)
		body, err := fs.ReadFile(packageFS, "plugin.json")
		if err != nil {
			return nil, nil, err
		}
		var manifest Manifest
		if json.Unmarshal(body, &manifest) != nil || manifest.Schema != manifestSchema || manifest.Name != item.Name || manifest.Version == "" {
			return nil, nil, errors.New("marketplace package identity mismatch")
		}
		title := displayText(item.Interface.DisplayName)
		if title == "" {
			title = manifest.Name
		}
		entries = append(entries, Entry{ID: item.Name, Title: title, Version: manifest.Version, Description: displayText(manifest.Description), Source: "marketplace:" + raw.Name + "/" + item.Name, Files: confirmed, Executables: executables})
		sources[item.Name] = packageFS
	}
	return entries, sources, nil
}

// Check every path component before fs.Sub: os.DirFS otherwise follows a
// symlink used as the package root, outside the WalkDir visibility boundary.
func reviewedSubdir(source fs.FS, folder string) (fs.FS, error) {
	parent := "."
	for _, component := range strings.Split(folder, "/") {
		children, err := fs.ReadDir(source, parent)
		if err != nil {
			return nil, err
		}
		found := false
		for _, child := range children {
			if child.Name() == component {
				if !child.IsDir() || child.Type()&fs.ModeSymlink != 0 {
					return nil, errors.New("unsafe marketplace source")
				}
				found = true
				break
			}
		}
		if !found {
			return nil, fs.ErrNotExist
		}
		parent = path.Join(parent, component)
	}
	return fs.Sub(source, folder)
}
