// Package messageimage retains ordinary user image presentation independently
// of the native model input and of screen capture. Only native submission may
// insert bytes; the renderer can read only opaque IDs found in saved records.
package messageimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const maxImage = 20 << 20
const maxImages = 8

type Store struct {
	root string
	mu   sync.Mutex
}
type record struct {
	Version int              `json:"version"`
	Created time.Time        `json:"created"`
	Request string           `json:"request,omitempty"`
	Text    string           `json:"text,omitempty"`
	Caption string           `json:"caption,omitempty"`
	Status  string           `json:"status,omitempty"`
	Images  []api.MediaImage `json:"images"`
	MIMEs   []string         `json:"mimes"`
}

var imageID = regexp.MustCompile(`^([a-f0-9]{64})-([0-7])$`)

func key(request string) string {
	hash := sha256.Sum256([]byte(request))
	return hex.EncodeToString(hash[:])
}
func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("image presentation storage unavailable")
	}
	return &Store{root: root}, nil
}
func candidate(name, mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}
func supported(data []byte, mime string) (int, int, bool) {
	if len(data) == 0 || len(data) > maxImage {
		return 0, 0, false
	}
	if mime == "image/webp" {
		// The WebKit renderer decodes WebP; the RIFF signature is checked here.
		return 0, 0, len(data) >= 16 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	}
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" {
		return 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 16384 || cfg.Height > 16384 || int64(cfg.Width)*int64(cfg.Height) > 64e6 {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}
func thumbnail(data []byte, width, height int) ([]byte, error) {
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	w, h := width, height
	if w > 420 || h > 280 {
		factor := min(420.0/float64(w), 280.0/float64(h))
		w = max(1, int(float64(w)*factor))
		h = max(1, int(float64(h)*factor))
	}
	preview := image.NewNRGBA(image.Rect(0, 0, w, h))
	bounds := decoded.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			preview.Set(x, y, decoded.At(bounds.Min.X+x*width/w, bounds.Min.Y+y*height/h))
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, preview, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}
func (s *Store) read(name string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(s.root)
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
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("image unavailable")
	}
	return io.ReadAll(io.LimitReader(f, limit+1))
}
func (s *Store) record(k string) (record, error) {
	data, err := s.read(k+"/media.json", 256<<10)
	if err != nil {
		return record{}, err
	}
	var r record
	if json.Unmarshal(data, &r) != nil || r.Version != 1 || len(r.Images) == 0 || len(r.Images) > maxImages || len(r.MIMEs) != len(r.Images) {
		return record{}, errors.New("invalid image presentation record")
	}
	if len(r.Text) > 128<<10 || len(r.Caption) > 128<<10 || r.Request != "" && key(r.Request) != k || r.Status != "" && r.Status != "sending" && r.Status != "accepted" && r.Status != "rejected" && r.Status != "unknown" {
		return record{}, errors.New("invalid image presentation record")
	}
	for i, img := range r.Images {
		if img.ID != k+"-"+string(rune('0'+i)) || len(img.Name) > 512 || !candidate(img.Name, r.MIMEs[i]) {
			return record{}, errors.New("invalid image presentation record")
		}
	}
	return r, nil
}

// Save is completed before native dispatch. Repeated reads of an existing
// request return its original display record and never rewrite or resend it.
func (s *Store) Save(request, caption string, files []api.InputFile) error {
	if s == nil || request == "" || len(request) > 256 || len(caption) > 128<<10 {
		return errors.New("image presentation storage unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(request)
	if _, err := s.record(k); err == nil {
		return nil
	}
	dir, err := os.MkdirTemp(s.root, ".pending-")
	if err != nil {
		return errors.New("image presentation storage unavailable")
	}
	defer os.RemoveAll(dir)
	r := record{Version: 1, Created: time.Now(), Caption: caption}
	for _, file := range files {
		if len(r.Images) == maxImages {
			break
		}
		f, err := os.Open(file.Path)
		if err != nil {
			return errors.New("image attachment unavailable")
		}
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			f.Close()
			return errors.New("image attachment unavailable")
		}
		prefix := make([]byte, 512)
		n, _ := f.Read(prefix)
		mime := http.DetectContentType(prefix[:n])
		if !candidate(file.Name, mime) {
			f.Close()
			continue
		}
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return errors.New("image attachment unavailable")
		}
		img := api.MediaImage{ID: k + "-" + string(rune('0'+len(r.Images))), Name: filepath.Base(file.Name)}
		if len(img.Name) > 512 {
			img.Name = img.Name[:512]
		}
		data, readErr := io.ReadAll(io.LimitReader(f, maxImage+1))
		f.Close()
		if readErr != nil {
			return errors.New("image attachment unavailable")
		}
		var valid bool
		img.Width, img.Height, valid = supported(data, mime)
		img.Unavailable = !valid
		if valid {
			if mime != "image/webp" {
				preview, err := thumbnail(data, img.Width, img.Height)
				if err != nil {
					img.Unavailable = true
				} else if err := os.WriteFile(filepath.Join(dir, string(rune('0'+len(r.Images)))+".thumb"), preview, 0600); err != nil {
					return errors.New("image presentation storage unavailable")
				}
			}
		}
		if valid && !img.Unavailable {
			if err := os.WriteFile(filepath.Join(dir, string(rune('0'+len(r.Images)))), data, 0600); err != nil {
				return errors.New("image presentation storage unavailable")
			}
		}
		r.Images = append(r.Images, img)
		r.MIMEs = append(r.MIMEs, mime)
	}
	if len(r.Images) == 0 {
		return nil
	}
	if err := localstate.Write(filepath.Join(dir, "media.json"), r); err != nil {
		return errors.New("image presentation storage unavailable")
	}
	if err := os.Rename(dir, filepath.Join(s.root, k)); err != nil {
		return errors.New("image presentation storage unavailable")
	}
	return nil
}

// Mark records only display state. A sending marker restored after a crash is
// uncertain; neither this file nor its bytes authorize a new submission.
func (s *Store) Mark(request, text, status string) error {
	if s == nil || request == "" || len(text) > 128<<10 || status != "sending" && status != "accepted" && status != "rejected" && status != "unknown" {
		return errors.New("image presentation unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(request)
	r, err := s.record(k)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} // No image in this message.
	if err != nil {
		return err
	}
	if r.Status != "" && status == "sending" {
		return nil
	}
	r.Request, r.Text, r.Status = request, text, status
	return localstate.Write(filepath.Join(s.root, k, "media.json"), r)
}
func (s *Store) Resolve(request string) {
	if s == nil || request == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(request)
	r, err := s.record(k)
	if err != nil || r.Request != "" && r.Request != request {
		return
	}
	r.Request, r.Text, r.Status = "", "", ""
	_ = localstate.Write(filepath.Join(s.root, k, "media.json"), r)
}

// ReceiptStatus is evidence from the original native submission that the
// presentation store durably observed. It never creates or replays an input.
func (s *Store) ReceiptStatus(request string) string {
	if s == nil || request == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.record(key(request))
	if err != nil || r.Request != request {
		return ""
	}
	if r.Status == "accepted" || r.Status == "rejected" {
		return r.Status
	}
	return ""
}
func (s *Store) Pending() []api.Item {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dirs, err := os.ReadDir(s.root)
	if err != nil {
		return nil
	}
	type entry struct {
		created time.Time
		item    api.Item
	}
	var rows []entry
	for _, dir := range dirs {
		if !dir.IsDir() || len(dir.Name()) != 64 {
			continue
		}
		r, err := s.record(dir.Name())
		// Only uncertain input needs a restored presentation. Accepted and
		// rejected are terminal receipts, not new conversation input after a
		// Runtime reconnect or Bot upgrade.
		if err != nil || r.Request == "" || r.Status != "sending" && r.Status != "unknown" {
			continue
		}
		status := r.Status
		if status == "sending" {
			status = "unknown"
		}
		rows = append(rows, entry{r.Created, api.Item{ID: "outgoing:" + r.Request, RequestID: r.Request, Kind: "user", Text: r.Text, Status: status, Media: &api.MediaPresentation{Images: r.Images, Caption: r.Caption}}})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].created.Before(rows[j].created) })
	out := make([]api.Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.item)
	}
	return out
}
func (s *Store) Images(request string) []api.MediaImage {
	presentation := s.Presentation(request)
	if presentation == nil {
		return nil
	}
	return presentation.Images
}
func (s *Store) Presentation(request string) *api.MediaPresentation {
	if s == nil || request == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.record(key(request))
	if err != nil {
		return nil
	}
	return &api.MediaPresentation{Images: append([]api.MediaImage(nil), r.Images...), Caption: r.Caption}
}
func (s *Store) DataURL(id string, thumbnail bool) (string, error) {
	if s == nil {
		return "", errors.New("image unavailable")
	}
	match := imageID.FindStringSubmatch(id)
	if match == nil {
		return "", errors.New("image unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.record(match[1])
	i := int(match[2][0] - '0')
	if err != nil || i >= len(r.Images) || r.Images[i].Unavailable {
		return "", errors.New("image unavailable")
	}
	name := match[1] + "/" + match[2]
	mime := r.MIMEs[i]
	limit := int64(maxImage)
	if thumbnail && mime != "image/webp" {
		name += ".thumb"
		mime = "image/jpeg"
		limit = 512 << 10
	}
	data, err := s.read(name, limit)
	if err != nil || int64(len(data)) > limit {
		return "", errors.New("image unavailable")
	}
	if _, _, ok := supported(data, mime); !ok {
		return "", errors.New("image unavailable")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
