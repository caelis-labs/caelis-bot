package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"os"
	"path/filepath"
	"slices"
)

// DraftFile is local selection metadata, not an upload or backend attachment.
// Paths remain in the host; the adapter copies and reads bytes only on send.
type DraftFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Unavailable bool   `json:"unavailable"`
}
type draftFile struct {
	DraftFile
	path string
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
		result[i].Unavailable = err != nil || !info.Mode().IsRegular() || info.Size() > 20*1024*1024
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
	defer s.mu.Unlock()
	s.picking = false
	if s.stopped {
		return nil, errors.New(s.text("native.appExited", nil))
	}
	if err != nil {
		return nil, errors.New(s.text("native.pickFileFailed", nil))
	}
	return s.stageFiles(paths)
}
func (s *Service) stageFiles(paths []string) ([]DraftFile, error) {
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
		if info.Size() > 20*1024*1024 {
			return nil, errors.New(s.text("native.fileTooLarge", nil))
		}
		if len(files) >= 8 {
			return nil, errors.New(s.text("native.maxFilesExceeded", nil))
		}
		next++
		files = append(files, draftFile{DraftFile: DraftFile{ID: fmt.Sprintf("local-%d", next), Name: filepath.Base(path), Size: info.Size()}, path: path})
	}
	if err := s.persistSelection(files, next); err != nil {
		return nil, err
	}
	s.files, s.nextFile = files, next
	return s.draftFiles(), nil
}
func (s *Service) RemoveFile(id string) ([]DraftFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files := slices.DeleteFunc(slices.Clone(s.files), func(f draftFile) bool { return f.ID == id })
	if s.selectionError != nil {
		return nil, s.selectionError
	}
	if err := s.persistSelection(files, s.nextFile); err != nil {
		return nil, err
	}
	s.files = files
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selectionError != nil {
		return
	}
	files := slices.DeleteFunc(slices.Clone(s.files), func(f draftFile) bool { return slices.Contains(ids, f.ID) })
	if err := s.persistSelection(files, s.nextFile); err != nil {
		s.selectionError = err
	}
	s.files = files
}

type savedSelection struct {
	Version int         `json:"version"`
	Next    uint64      `json:"next"`
	Files   []savedFile `json:"files"`
}
type savedFile struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func (s *Service) persistSelection(files []draftFile, next uint64) error {
	d := savedSelection{Version: 1, Next: next, Files: []savedFile{}}
	for _, f := range files {
		d.Files = append(d.Files, savedFile{f.ID, f.path, f.Size})
	}
	if err := localstate.Write(s.selectionFile, d); err != nil {
		return errors.New(s.text("native.selectionNotSaved", nil))
	}
	return nil
}
func (s *Service) configureSelection(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selectionFile = path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var d savedSelection
	if err != nil || len(b) > 128*1024 || json.Unmarshal(b, &d) != nil || d.Version != 1 || len(d.Files) > 8 {
		s.selectionError = errors.New(s.text("native.selectionUnreadable", nil))
		return s.selectionError
	}
	files := make([]draftFile, 0, len(d.Files))
	seen := map[string]bool{}
	for _, f := range d.Files {
		if f.ID == "" || seen[f.ID] || !filepath.IsAbs(f.Path) {
			s.selectionError = errors.New(s.text("native.selectionUnreadable", nil))
			return s.selectionError
		}
		seen[f.ID] = true
		files = append(files, draftFile{DraftFile: DraftFile{ID: f.ID, Name: filepath.Base(f.Path), Size: f.Size}, path: f.Path})
	}
	s.files, s.nextFile = files, d.Next
	return nil
}
