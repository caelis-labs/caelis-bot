package desktop

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const (
	artifactPreviewLimit = 256 << 10
	artifactCopyLimit    = 1 << 20
)

type ArtifactPreview struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

// The renderer receives only a display name and the already owned handle.
// The file path stays in the backend's artifact resolver.
type ArtifactPreviewSelection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func artifactDisplayName(path string) string {
	name := filepath.Base(path)
	if name == "." || len(name) > 255 || strings.IndexFunc(name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return "artifact"
	}
	return name
}

func (s *Service) ShowArtifactPreview(id string) error {
	s.mu.Lock()
	s.artifactPreviewRequest++
	request := s.artifactPreviewRequest
	s.mu.Unlock()
	copy, err := s.artifactPreviewCopy(id)
	s.mu.Lock()
	stale := request != s.artifactPreviewRequest
	s.mu.Unlock()
	if stale {
		return nil
	}
	if err != nil {
		return err
	}
	if !textArtifact(copy.Path) {
		return errors.New("此文件类型不支持文本预览")
	}
	selection := ArtifactPreviewSelection{ID: id, Name: artifactDisplayName(copy.Name)}
	s.mu.Lock()
	if request != s.artifactPreviewRequest {
		s.mu.Unlock()
		return nil
	}
	open := s.openArtifactPreview
	if open != nil {
		s.artifactPreview = selection
	}
	s.mu.Unlock()
	if open == nil {
		return errors.New("预览窗口暂不可用")
	}
	open()
	return nil
}

func (s *Service) artifactPreviewCopy(id string) (api.TaskFileCopy, error) {
	if s.resolveArtifactCopy == nil {
		path, err := s.artifactPath(id)
		return api.TaskFileCopy{Path: path, Name: filepath.Base(path)}, err
	}
	if id == "" {
		return api.TaskFileCopy{}, errors.New("文件已不可用")
	}
	copy, err := s.resolveArtifactCopy(id)
	if err != nil || copy.Path == "" {
		return api.TaskFileCopy{}, errors.New("文件已不可用")
	}
	info, err := os.Stat(copy.Path)
	if err != nil || !info.Mode().IsRegular() {
		return api.TaskFileCopy{}, errors.New("文件已不可用")
	}
	return copy, nil
}

func (s *Service) CurrentArtifactPreview() (ArtifactPreviewSelection, error) {
	s.mu.Lock()
	selection := s.artifactPreview
	s.mu.Unlock()
	if selection.ID == "" {
		return ArtifactPreviewSelection{}, errors.New("预览文件已不可用")
	}
	return selection, nil
}

func textArtifact(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md", ".markdown", ".json", ".jsonl", ".sql", ".log", ".csv", ".tsv", ".yaml", ".yml", ".xml", ".toml", ".ini", ".sh", ".py", ".go", ".swift", ".js", ".ts", ".tsx", ".css", ".html":
		return true
	}
	return false
}

func (s *Service) artifactPath(id string) (string, error) {
	if id == "" || s.resolveArtifact == nil {
		return "", errors.New("文件已不可用")
	}
	path, err := s.resolveArtifact(id)
	if err != nil || path == "" {
		return "", errors.New("文件已不可用")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("文件已不可用")
	}
	return path, nil
}

func readArtifactText(path string, limit int64, partial bool) (ArtifactPreview, error) {
	if !textArtifact(path) {
		return ArtifactPreview{}, errors.New("此文件类型不支持文本预览")
	}
	f, err := os.Open(path)
	if err != nil {
		return ArtifactPreview{}, errors.New("文件已不可用")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ArtifactPreview{}, errors.New("文件已不可用")
	}
	if !partial && info.Size() > limit {
		return ArtifactPreview{}, errors.New("文件过大，无法复制全文")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return ArtifactPreview{}, errors.New("文件读取失败")
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	// Only an incomplete code point at the read boundary may be discarded.
	// An invalid byte anywhere else is a corrupt file, even for a preview.
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			if truncated && !utf8.FullRune(data[i:]) {
				data = data[:i]
				break
			}
			return ArtifactPreview{}, errors.New("此文件不是可预览的 UTF-8 文本")
		}
		i += size
	}
	if strings.ContainsRune(string(data), 0) {
		return ArtifactPreview{}, errors.New("此文件不是可预览的 UTF-8 文本")
	}
	return ArtifactPreview{Text: string(data), Truncated: truncated}, nil
}

func (s *Service) PreviewArtifact(id string) (ArtifactPreview, error) {
	path, err := s.artifactPath(id)
	if err != nil {
		return ArtifactPreview{}, err
	}
	return readArtifactText(path, artifactPreviewLimit, true)
}

func (s *Service) OpenArtifact(id string) error {
	path, err := s.artifactPath(id)
	if err != nil {
		return err
	}
	if s.openArtifactFile == nil || s.openArtifactFile(path) != nil {
		return errors.New("无法用默认应用打开文件")
	}
	return nil
}

func (s *Service) CopyArtifactPath(id string) error {
	path, err := s.artifactPath(id)
	if err != nil {
		return err
	}
	return s.CopyText(path)
}

func (s *Service) CopyArtifactContent(id string) error {
	path, err := s.artifactPath(id)
	if err != nil {
		return err
	}
	preview, err := readArtifactText(path, artifactCopyLimit, false)
	if err != nil {
		return err
	}
	return s.CopyText(preview.Text)
}

func (s *Service) SaveArtifactAs(id, name string) error {
	path, err := s.artifactPath(id)
	if err != nil {
		return err
	}
	if s.saveArtifactPath == nil {
		return errors.New("另存为暂不可用")
	}
	name = filepath.Base(name)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = filepath.Base(path)
	}
	target, err := s.saveArtifactPath(name)
	if err != nil || target == "" {
		return err // empty selection is a normal cancellation
	}
	if filepath.Clean(target) == filepath.Clean(path) {
		return nil
	}
	source, err := os.Open(path)
	if err != nil {
		return errors.New("文件已不可用")
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("文件已不可用")
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".caelis-artifact-*")
	if err != nil {
		return errors.New("无法保存文件")
	}
	defer os.Remove(tmp.Name())
	if _, err = io.Copy(tmp, source); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil || os.Rename(tmp.Name(), target) != nil {
		return errors.New("无法保存文件")
	}
	return nil
}
