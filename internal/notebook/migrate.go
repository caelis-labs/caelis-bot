package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Migrate copies the retired private-header format once. Originals remain intact;
// the marker outside the Vault prevents deleted imports from being resurrected.
func (v *Vault) Migrate(oldDir, marker, profile string, now time.Time) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if b, err := os.ReadFile(marker); err == nil {
		var state struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(b, &state) != nil || state.Version != 1 {
			return errors.New("笔记迁移记录无法读取，请保留数据")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	type imported struct {
		path string
		body []byte
	}
	items := []imported{}
	if info, err := os.Lstat(oldDir); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("旧笔记目录不可重定向")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir(oldDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if !entry.Type().IsRegular() {
			return errors.New("旧笔记不是普通文件，请保留数据")
		}
		b, err := os.ReadFile(filepath.Join(oldDir, entry.Name()))
		if err != nil {
			return err
		}
		head, body, ok := strings.Cut(string(b), "\n\n")
		var meta struct {
			Version                int
			Title, Updated, Source string
			Deleted                bool
		}
		if !ok || !strings.HasPrefix(head, "<!-- caelis-note ") || !strings.HasSuffix(head, " -->") || json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(head, "<!-- caelis-note "), " -->")), &meta) != nil || meta.Version != 1 {
			return errors.New("旧笔记无法迁移，原文件已保留")
		}
		if meta.Deleted {
			continue
		}
		date, err := time.Parse(time.RFC3339Nano, meta.Updated)
		if err != nil {
			date = now
		}
		path := filepath.Join(date.In(now.Location()).Format("2006/01/02"), "imported-"+entry.Name())
		items = append(items, imported{path, []byte(fmt.Sprintf("# %s\n\n%s\n\n---\n原记录来源：%s\n", meta.Title, body, meta.Source))})
	}
	if profile != "" {
		items = append(items, imported{filepath.Join(now.Format("2006/01/02"), "imported-profile.md"), []byte(profile)})
	}
	for _, item := range items {
		if err := v.root.MkdirAll(filepath.Dir(item.path), 0700); err != nil {
			return err
		}
		if err := v.create(item.path, item.body); err != nil {
			if errors.Is(err, os.ErrExist) {
				b, readErr := v.root.ReadFile(item.path)
				if readErr == nil && string(b) == string(item.body) {
					continue
				}
			}
			return errors.New("迁移笔记与已有文件冲突，双方内容均已保留")
		}
	}
	return localstate.Write(marker, struct {
		Version int `json:"version"`
	}{1})
}
