package screeninput

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Media retains presentation bytes independently of temporary capture exports.
// Only native acquisition may insert images. Renderer reads use opaque IDs.
type Media struct {
	root string
	mu   sync.Mutex
}
type mediaRecord struct {
	Version int               `json:"version"`
	Created time.Time         `json:"created"`
	Images  []api.ScreenImage `json:"images"`
	MIMEs   []string          `json:"mimes"`
}

var mediaID = regexp.MustCompile(`^([a-f0-9]{64})-([01])$`)

func mediaKey(request string) string {
	sum := sha256.Sum256([]byte(request))
	return hex.EncodeToString(sum[:])
}
func OpenMedia(root string) (*Media, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid screen media store")
	}
	return &Media{root: root}, nil
}
func (m *Media) read(name string, max int64) ([]byte, error) {
	root, err := os.OpenRoot(m.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > max {
		return nil, errors.New("screen image unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if len(data) > int(max) {
		return nil, errors.New("screen image too large")
	}
	return data, err
}
func (m *Media) record(key string) (mediaRecord, error) {
	data, err := m.read(key+"/media.json", 4096)
	if err != nil {
		return mediaRecord{}, err
	}
	var r mediaRecord
	if json.Unmarshal(data, &r) != nil || r.Version != 1 || len(r.Images) < 1 || len(r.Images) > 2 || len(r.MIMEs) != len(r.Images) {
		return r, errors.New("invalid screen media")
	}
	for i, v := range r.Images {
		if v.ID != fmt.Sprintf("%s-%d", key, i) || v.Width < 1 || v.Height < 1 || (r.MIMEs[i] != "image/png" && r.MIMEs[i] != "image/jpeg") {
			return r, errors.New("invalid screen media")
		}
	}
	return r, nil
}
func (m *Media) Images(request string) []api.ScreenImage {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.record(mediaKey(request))
	if err != nil {
		return nil
	}
	return r.Images
}

// Save is completed before dispatch; failure must leave the input unsent.
func (m *Media) Save(request string, files []api.InputFile) error {
	if m == nil {
		return errors.New("screen media storage unavailable")
	}
	if !identifier.MatchString(request) || len(files) < 1 || len(files) > 2 {
		return errors.New("invalid screen media input")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := mediaKey(request)
	if _, err := os.Lstat(filepath.Join(m.root, key)); !errors.Is(err, os.ErrNotExist) {
		return errors.New("screen media request already exists")
	}
	dir, err := os.MkdirTemp(m.root, ".pending-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	record := mediaRecord{Version: 1, Created: time.Now()}
	for i, file := range files {
		data, err := readFile(file.Path, 8<<20)
		if err != nil {
			return err
		}
		cfg, kind, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || (kind != "png" && kind != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 16384 || cfg.Height > 16384 || int64(cfg.Width)*int64(cfg.Height) > 64e6 {
			return errors.New("invalid screen image")
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return err
		}
		w, h := cfg.Width, cfg.Height
		if w > 420 || h > 280 {
			factor := min(420.0/float64(w), 280.0/float64(h))
			w = max(1, int(float64(w)*factor))
			h = max(1, int(float64(h)*factor))
		}
		thumb := image.NewNRGBA(image.Rect(0, 0, w, h))
		bounds := img.Bounds()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				thumb.Set(x, y, img.At(bounds.Min.X+x*cfg.Width/w, bounds.Min.Y+y*cfg.Height/h))
			}
		}
		var preview bytes.Buffer
		if err := jpeg.Encode(&preview, thumb, &jpeg.Options{Quality: 85}); err != nil {
			return err
		}
		name := strconv.Itoa(i)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name+".thumb"), preview.Bytes(), 0600); err != nil {
			return err
		}
		role := "selection"
		if i == 1 {
			role = "context"
		}
		record.Images = append(record.Images, api.ScreenImage{ID: fmt.Sprintf("%s-%d", key, i), Role: role, Width: cfg.Width, Height: cfg.Height})
		record.MIMEs = append(record.MIMEs, "image/"+kind)
	}
	if err := localstate.Write(filepath.Join(dir, "media.json"), record); err != nil {
		return err
	}
	return os.Rename(dir, filepath.Join(m.root, key))
}
func (m *Media) Bytes(id string, thumbnail bool) ([]byte, string, error) {
	if m == nil {
		return nil, "", errors.New("screen image unavailable")
	}
	match := mediaID.FindStringSubmatch(id)
	if match == nil {
		return nil, "", errors.New("invalid screen image identifier")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.record(match[1])
	i, _ := strconv.Atoi(match[2])
	if err != nil || i >= len(r.Images) {
		return nil, "", errors.New("screen image unavailable")
	}
	name := match[1] + "/" + match[2]
	mime := r.MIMEs[i]
	limit := int64(8 << 20)
	if thumbnail {
		name += ".thumb"
		mime = "image/jpeg"
		limit = 512 << 10
	}
	data, err := m.read(name, limit)
	return data, mime, err
}
func (m *Media) DataURL(id string, thumbnail bool) (string, error) {
	data, mime, err := m.Bytes(id, thumbnail)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// Storage joins the existing user-initiated 30-day attachment cleanup. There is
// no timer-driven deletion of conversation images or automatic history resend.
func (m *Media) Storage(clean bool, trash func(string) error) (api.AttachmentStorage, error) {
	var out api.AttachmentStorage
	if m == nil {
		return out, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	dirs, err := os.ReadDir(m.root)
	if err != nil {
		return out, err
	}
	for _, dir := range dirs {
		if !dir.IsDir() || !mediaID.MatchString(dir.Name()+"-0") {
			continue
		}
		r, err := m.record(dir.Name())
		if err != nil {
			continue
		}
		old := r.Created.Before(time.Now().Add(-30 * 24 * time.Hour))
		if clean && old && trash != nil {
			if err := trash(filepath.Join(m.root, dir.Name())); err != nil {
				return out, err
			}
			continue
		}
		entries, err := os.ReadDir(filepath.Join(m.root, dir.Name()))
		if err != nil {
			return out, err
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			out.Files++
			out.Bytes += info.Size()
			if old {
				out.EligibleFiles++
				out.EligibleBytes += info.Size()
			}
		}
	}
	out.CanClean = out.EligibleFiles > 0
	return out, nil
}
