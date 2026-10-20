package desktop

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/contentpack"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

// NewHeadlessProduct reuses the toolkit-independent product assembly and its
// preference/attachment services. It never constructs or impersonates a native
// driver. Only the explicit web allowlist may expose methods of this Service.
func NewHeadlessProduct(root string) (*app.Application, *Service, error) {
	p, err := newProductAssembly(root, []string{"zh-CN"}, func(_ *Service, _ *diagnosticlog.Logger) app.Host {
		return app.Host{
			OpenURL:    func(string) error { return errors.New("open this link in your browser") },
			RevealFile: func(string) error { return errors.New("native windows are unavailable in the web surface") },
		}
	})
	if err != nil {
		return nil, nil, err
	}
	// Attachment staging needs a live surface, not an OS window. Native features
	// remain unsupported because native/readClipboard are deliberately absent.
	p.Service.mu.Lock()
	p.Service.started = true
	close(p.Service.ready)
	p.Service.mu.Unlock()
	return p.Core, p.Service, nil
}

// StageWebUpload only accepts bytes already bounded by the authenticated web
// transport. The returned ID goes through the existing draft/send receipt path.
// The caller owns cleanup of its private upload directory; paths never come
// from a browser or from model text.
func (s *Service) StageWebUpload(directory, name string, input io.Reader) ([]DraftFile, error) {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, "/\\\x00") || len(name) > 240 {
		return nil, errors.New("invalid attachment name")
	}
	dir, err := os.MkdirTemp(directory, "upload-")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		_ = os.Remove(dir)
		return nil, err
	}
	n, err := io.Copy(f, io.LimitReader(input, maxDraftFile+1))
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil || n > maxDraftFile {
		_ = os.Remove(path)
		_ = os.Remove(dir)
		return nil, errors.New("attachment could not be saved")
	}
	s.mu.Lock()
	files, err := s.stageOwnedAttachments([]string{path}, nil, true)
	s.mu.Unlock()
	if err != nil {
		_ = os.Remove(path)
		_ = os.Remove(dir)
	}
	return files, err
}

// WebContentHandler uses the same checked content-pack resolver as the App.
func (s *Service) WebContentHandler(next http.Handler) http.Handler { return s.content.Handler(next) }

func (s *Service) ImportWebContent(path string) (contentpack.State, error) {
	if !s.contentImportMu.TryLock() {
		return s.content.BotState(), errors.New("import in progress")
	}
	defer s.contentImportMu.Unlock()
	return s.publishContent(s.content.Install(path))
}
