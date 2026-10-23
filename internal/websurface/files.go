package websurface

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
)

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	s.composerMu.Lock()
	defer s.composerMu.Unlock()
	if !s.ownsComposer(r) {
		fail(w, 409, "composer_owned_elsewhere")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 21<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		fail(w, 413, "file_too_large")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "file_required")
		return
	}
	defer file.Close()
	if header.Size > 20<<20 {
		fail(w, 413, "file_too_large")
		return
	}
	var used int64
	root := filepath.Join(s.config.Directory, "uploads")
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.Type().IsRegular() {
			info, e := entry.Info()
			if e != nil {
				return e
			}
			used += info.Size()
		}
		return nil
	})
	if err != nil || used+header.Size > 512<<20 {
		fail(w, 413, "upload_capacity")
		return
	}
	// Uploads stage bytes only; they cannot dispatch a message. Keep even an
	// interrupted response inspectable through DraftFiles; never auto-resend.
	files, err := s.services.Surface.StageWebUpload(root, header.Filename, file)
	if err != nil {
		fail(w, 400, "file_unavailable")
		return
	}
	jsonOut(w, 200, map[string]any{"value": files})
}

func (s *Server) artifact(w http.ResponseWriter, r *http.Request) {
	path, err := backend.ResolveRemoteArtifact(s.services.Backend, r.PathValue("id"))
	if err != nil {
		fail(w, 404, "file_unavailable")
		return
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > 256<<20 {
		fail(w, 404, "file_unavailable")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		fail(w, 404, "file_unavailable")
		return
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		fail(w, 409, "file_changed")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(path)}))
	if r.URL.Query().Get("preview") == "1" {
		var prefix [512]byte
		n, _ := f.ReadAt(prefix[:], 0)
		kind := http.DetectContentType(prefix[:n])
		switch kind {
		case "image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf":
			w.Header().Set("Content-Type", kind)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filepath.Base(path)}))
		}
	}
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	http.ServeContent(w, r, filepath.Base(path), time.Time{}, io.NewSectionReader(f, 0, after.Size()))
}

// The uploaded archive stays in a private temporary directory and goes through
// the existing registry's validation. Never accept a host path from the browser.
func (s *Server) importContent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 65<<20)
	if r.ParseMultipartForm(1<<20) != nil {
		fail(w, 413, "file_too_large")
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, _, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "file_required")
		return
	}
	defer f.Close()
	tmp, err := os.CreateTemp(s.config.Directory, "pack-*.zip")
	if err != nil {
		fail(w, 503, "file_unavailable")
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, err = io.Copy(tmp, f)
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		fail(w, 503, "file_unavailable")
		return
	}
	state, err := s.services.Surface.ImportWebContent(name)
	if err != nil {
		fail(w, 400, "invalid_content_pack")
		return
	}
	jsonOut(w, 200, map[string]any{"value": state})
}
