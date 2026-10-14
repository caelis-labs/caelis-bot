package desktop

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const maxDraftFile = 20 << 20

var errClipboardTooLarge = errors.New("clipboard image too large")
var errClipboardInvalid = errors.New("clipboard image invalid")

// DraftFile is local selection metadata, not an upload or backend attachment.
// Paths remain in the host; the adapter copies and reads bytes only on send.
type DraftFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Type        string `json:"type"`
	Image       bool   `json:"image"`
	Unavailable bool   `json:"unavailable"`
}
type draftFile struct {
	DraftFile
	path  string
	owned bool
}

type PasteResult struct {
	Handled bool        `json:"handled"`
	Files   []DraftFile `json:"files"`
}

func (s *Service) DraftFiles() []DraftFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.draftFiles()
}
func (s *Service) draftFiles() []DraftFile {
	result := make([]DraftFile, len(s.files))
	for i, f := range s.files {
		result[i] = f.DraftFile
		info, err := os.Stat(f.path)
		result[i].Unavailable = err != nil || !info.Mode().IsRegular() || info.Size() > maxDraftFile
	}
	return result
}
func (s *Service) PickFiles() ([]DraftFile, error) {
	s.mu.Lock()
	if !s.started || s.stopped || s.pickFiles == nil || s.picking {
		s.mu.Unlock()
		return nil, errors.New(s.text("native.pickerUnavailable", nil))
	}
	s.picking = true
	pick := s.pickFiles
	s.mu.Unlock()
	// Never hold the lifecycle lock while the user interacts with a native sheet.
	paths, err := pick()
	s.mu.Lock()
	s.picking = false
	if s.stopped {
		s.mu.Unlock()
		return nil, errors.New(s.text("native.appExited", nil))
	}
	if err != nil {
		s.mu.Unlock()
		return nil, errors.New(s.text("native.pickFileFailed", nil))
	}
	files, err := s.stageFiles(paths)
	notify := s.filesChanged
	s.mu.Unlock()
	// A sheet can outlive a mounted composer. Notify the current view to read
	// the committed selection, even when the original caller has been closed.
	if err == nil && notify != nil {
		notify()
	}
	return files, err
}
func (s *Service) stageFiles(paths []string) ([]DraftFile, error) {
	return s.stageAttachments(paths, nil)
}

// PasteAttachments reads the macOS pasteboard in the native process. The
// renderer never supplies a path or image bytes. A file URL takes precedence
// over another image representation of that same pasteboard item.
func (s *Service) PasteAttachments() (PasteResult, error) {
	if s.readClipboard == nil {
		return PasteResult{}, errors.New(s.text("native.clipboardUnavailable", nil))
	}
	paths, image, err := s.readClipboard()
	if err != nil {
		if errors.Is(err, errClipboardTooLarge) {
			return PasteResult{}, errors.New(s.text("native.fileTooLarge", nil))
		}
		if errors.Is(err, errClipboardInvalid) {
			return PasteResult{}, errors.New(s.text("native.clipboardImageInvalid", nil))
		}
		return PasteResult{}, errors.New(s.text("native.clipboardUnavailable", nil))
	}
	if len(paths) > 0 {
		image = nil
	}
	if len(paths) == 0 && len(image) == 0 {
		return PasteResult{Handled: false}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	files, err := s.stageAttachments(paths, image)
	return PasteResult{Handled: true, Files: files}, err
}

func (s *Service) stageAttachments(paths []string, imageBytes []byte) ([]DraftFile, error) {
	if s.selectionError != nil {
		return nil, s.selectionError
	}
	if !s.started || s.stopped {
		return nil, errors.New(s.text("native.appNotReadyOrExited", nil))
	}
	// Validate the whole selection before changing the draft, including duplicates.
	files := slices.Clone(s.files)
	next := s.nextFile
	for _, path := range paths {
		path = filepath.Clean(path)
		if slices.ContainsFunc(files, func(f draftFile) bool { return f.path == path }) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New(s.text("native.selectRegularFile", nil))
		}
		if info.Size() > maxDraftFile {
			return nil, errors.New(s.text("native.fileTooLarge", nil))
		}
		if len(files) >= 8 {
			return nil, errors.New(s.text("native.maxFilesExceeded", nil))
		}
		next++
		kind, err := fileKind(path)
		if err != nil {
			return nil, errors.New(s.text("native.selectRegularFile", nil))
		}
		files = append(files, draftFile{DraftFile: DraftFile{ID: fmt.Sprintf("local-%d", next), Name: filepath.Base(path), Size: info.Size(), Type: kind, Image: strings.HasPrefix(kind, "image/")}, path: path})
	}
	var created string
	if len(imageBytes) > 0 {
		if len(imageBytes) > maxDraftFile {
			return nil, errors.New(s.text("native.fileTooLarge", nil))
		}
		if len(files) >= 8 {
			return nil, errors.New(s.text("native.maxFilesExceeded", nil))
		}
		if !validPNG(imageBytes) {
			return nil, errors.New(s.text("native.clipboardImageInvalid", nil))
		}
		var err error
		created, err = s.writeDraftImage(imageBytes)
		if err != nil {
			return nil, errors.New(s.text("native.clipboardImageNotSaved", nil))
		}
		next++
		files = append(files, draftFile{DraftFile: DraftFile{ID: fmt.Sprintf("local-%d", next), Name: s.text("native.pastedImageName", nil), Size: int64(len(imageBytes)), Type: "image/png", Image: true}, path: created, owned: true})
	}
	if err := s.persistSelection(files, next); err != nil {
		if created != "" {
			_ = os.Remove(created)
		}
		return nil, err
	}
	s.files, s.nextFile = files, next
	return s.draftFiles(), nil
}
func (s *Service) RemoveFile(id string) ([]DraftFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := slices.Clone(s.files)
	files := slices.DeleteFunc(slices.Clone(s.files), func(f draftFile) bool { return f.ID == id })
	if s.selectionError != nil {
		return nil, s.selectionError
	}
	if err := s.persistSelection(files, s.nextFile); err != nil {
		return nil, err
	}
	s.files = files
	s.removeOwnedFiles(removed, files)
	return s.draftFiles(), nil
}

func (s *Service) resolveDraftFiles(ids []string) ([]api.InputFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) > 8 {
		return nil, errors.New(s.text("native.maxSendFilesExceeded", nil))
	}
	files := make([]api.InputFile, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, errors.New(s.text("native.duplicateAttachment", nil))
		}
		seen[id] = true
		index := slices.IndexFunc(s.files, func(f draftFile) bool { return f.ID == id })
		if index < 0 {
			return nil, errors.New(s.text("native.attachmentExpired", nil))
		}
		f := s.files[index]
		files = append(files, api.InputFile{Name: f.Name, Path: f.path})
	}
	return files, nil
}
func (s *Service) consumeDraftFiles(ids []string) {
	_ = s.consumeDraftFilesChecked(ids)
}
func (s *Service) consumeDraftFilesChecked(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selectionFile == "" {
		return errors.New(s.text("native.selectionNotSaved", nil))
	}
	if s.selectionError != nil {
		return s.selectionError
	}
	files := slices.DeleteFunc(slices.Clone(s.files), func(f draftFile) bool { return slices.Contains(ids, f.ID) })
	if len(files) == len(s.files) {
		return nil
	} // A second accepted observer cannot consume again.
	if err := s.persistSelection(files, s.nextFile); err != nil {
		return err
	}
	old := s.files
	s.files = files
	s.removeOwnedFiles(old, files)
	return nil
}

type savedSelection struct {
	Version int         `json:"version"`
	Next    uint64      `json:"next"`
	Files   []savedFile `json:"files"`
}
type savedFile struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Name  string `json:"name,omitempty"`
	Type  string `json:"type,omitempty"`
	Owned bool   `json:"owned,omitempty"`
}

func (s *Service) persistSelection(files []draftFile, next uint64) error {
	if err := saveSelection(s.selectionFile, files, next); err != nil {
		return errors.New(s.text("native.selectionNotSaved", nil))
	}
	return nil
}

func saveSelection(path string, files []draftFile, next uint64) error {
	d := savedSelection{Version: 2, Next: next, Files: []savedFile{}}
	for _, f := range files {
		d.Files = append(d.Files, savedFile{ID: f.ID, Path: f.path, Size: f.Size, Name: f.Name, Type: f.Type, Owned: f.owned})
	}
	return localstate.Write(path, d)
}
func (s *Service) configureSelection(path string) error {
	return s.switchSelection(path, false)
}

func (s *Service) switchSelection(path string, transfer bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if path == s.selectionFile {
		return nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if transfer && len(s.files) > 0 {
			return s.transferSelection(path)
		}
		s.selectionFile = path
		s.selectionError = nil
		s.files = nil
		s.nextFile = 0
		s.cleanupOrphanImages()
		return nil
	}
	var d savedSelection
	if err != nil || len(b) > 128*1024 || json.Unmarshal(b, &d) != nil || (d.Version != 1 && d.Version != 2) || len(d.Files) > 8 {
		return errors.New(s.text("native.selectionUnreadable", nil))
	}
	files := make([]draftFile, 0, len(d.Files))
	seen := map[string]bool{}
	for _, f := range d.Files {
		if f.ID == "" || seen[f.ID] || !filepath.IsAbs(f.Path) || (f.Owned && (filepath.Dir(f.Path) != filepath.Join(filepath.Dir(path), "draft-images") || !strings.HasPrefix(filepath.Base(f.Path), "paste-"))) {
			return errors.New(s.text("native.selectionUnreadable", nil))
		}
		seen[f.ID] = true
		name, kind := f.Name, f.Type
		if name == "" {
			name = filepath.Base(f.Path)
		}
		if kind == "" {
			kind, _ = fileKind(f.Path)
		}
		files = append(files, draftFile{DraftFile: DraftFile{ID: f.ID, Name: name, Size: f.Size, Type: kind, Image: strings.HasPrefix(kind, "image/")}, path: f.Path, owned: f.Owned})
	}
	if transfer && len(s.files) > 0 {
		if len(files) > 0 {
			return errors.New(s.text("native.selectionConflict", nil))
		}
		return s.transferSelection(path)
	}
	s.selectionFile = path
	s.selectionError = nil
	s.files, s.nextFile = files, d.Next
	s.cleanupOrphanImages()
	return nil
}

// The composer is one Bot surface even when its resident Runtime changes.
// Copy only Bot-owned clipboard images; external file paths remain references.
// The previous selection stays intact for rollback or a later switch back.
func (s *Service) transferSelection(path string) error {
	files := slices.Clone(s.files)
	created := []string{}
	cleanup := func() {
		for _, name := range created {
			_ = os.Remove(name)
		}
	}
	for i := range files {
		if !files[i].owned {
			continue
		}
		info, err := os.Stat(files[i].path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxDraftFile {
			cleanup()
			return errors.New(s.text("native.selectionUnreadable", nil))
		}
		data, err := os.ReadFile(files[i].path)
		if err != nil || !validPNG(data) {
			cleanup()
			return errors.New(s.text("native.selectionUnreadable", nil))
		}
		name, err := writeDraftImageAt(filepath.Join(filepath.Dir(path), "draft-images"), data)
		if err != nil {
			cleanup()
			return errors.New(s.text("native.selectionNotSaved", nil))
		}
		created = append(created, name)
		files[i].path = name
	}
	if err := saveSelection(path, files, s.nextFile); err != nil {
		cleanup()
		return errors.New(s.text("native.selectionNotSaved", nil))
	}
	s.selectionFile, s.selectionError, s.files = path, nil, files
	return nil
}

func fileKind(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var prefix [512]byte
	n, err := f.Read(prefix[:])
	if err != nil && err != io.EOF {
		return "", err
	}
	b := prefix[:n]
	if len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return "image/webp", nil
	}
	return http.DetectContentType(b), nil
}

func validPNG(data []byte) bool {
	if len(data) == 0 || len(data) > maxDraftFile || http.DetectContentType(data) != "image/png" {
		return false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil && format == "png" && cfg.Width > 0 && cfg.Height > 0 && cfg.Width <= 16384 && cfg.Height <= 16384 && int64(cfg.Width)*int64(cfg.Height) <= 64e6
}

func (s *Service) imageDirectory() string {
	return filepath.Join(filepath.Dir(s.selectionFile), "draft-images")
}

func (s *Service) writeDraftImage(data []byte) (string, error) {
	if s.selectionFile == "" {
		return "", errors.New("draft storage unavailable")
	}
	return writeDraftImageAt(s.imageDirectory(), data)
}

func writeDraftImageAt(dir string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("draft storage unavailable")
	}
	f, err := os.CreateTemp(dir, "paste-*.png")
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer func() { _ = f.Close() }()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func (s *Service) removeOwnedFiles(old, current []draftFile) {
	for _, f := range old {
		if f.owned && !slices.ContainsFunc(current, func(v draftFile) bool { return v.ID == f.ID }) {
			_ = os.Remove(f.path)
		}
	}
}

func (s *Service) cleanupOrphanImages() {
	if s.selectionFile == "" {
		return
	}
	dir := s.imageDirectory()
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), "paste-") || !strings.HasSuffix(entry.Name(), ".png") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !slices.ContainsFunc(s.files, func(f draftFile) bool { return f.owned && f.path == path }) {
			_ = os.Remove(path)
		}
	}
}

// readDraftImage authorizes the current opaque selection ID and bounds the
// native file read. Both preview sizes use the same validation and never expose
// host paths. Reading a preview does not consume or submit the selection.
func (s *Service) readDraftImage(id string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := slices.IndexFunc(s.files, func(f draftFile) bool { return f.ID == id })
	if index < 0 || !s.files[index].Image {
		return nil, "", errors.New(s.text("native.attachmentExpired", nil))
	}
	f := s.files[index]
	file, err := os.Open(f.path)
	if err != nil {
		return nil, "", errors.New(s.text("native.selectRegularFile", nil))
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDraftFile {
		return nil, "", errors.New(s.text("native.selectRegularFile", nil))
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDraftFile+1))
	if err != nil || len(data) > maxDraftFile {
		return nil, "", errors.New(s.text("native.selectRegularFile", nil))
	}
	mime := http.DetectContentType(data)
	if f.Type == "image/webp" && len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return data, "image/webp", nil
	}
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" {
		return nil, "", errors.New(s.text("native.clipboardImageInvalid", nil))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 24e6 {
		return nil, "", errors.New(s.text("native.clipboardImageInvalid", nil))
	}
	return data, mime, nil
}

// DraftImage remains the small, inexpensive attachment-chip thumbnail.
func (s *Service) DraftImage(id string) (string, error) {
	data, mime, err := s.readDraftImage(id)
	if err != nil {
		return "", err
	}
	if mime == "image/webp" {
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	w, h := width, height
	if w > 160 || h > 120 {
		factor := min(160.0/float64(w), 120.0/float64(h))
		w, h = max(1, int(float64(w)*factor)), max(1, int(float64(h)*factor))
	}
	preview := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			preview.Set(x, y, decoded.At(bounds.Min.X+x*width/w, bounds.Min.Y+y*height/h))
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, preview, &jpeg.Options{Quality: 78}); err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()), nil
}

// DraftImagePreview is fetched only when the user opens a selected image. Its
// validated original bytes preserve screenshot text and transparent PNG detail.
func (s *Service) DraftImagePreview(id string) (string, error) {
	data, mime, err := s.readDraftImage(id)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
