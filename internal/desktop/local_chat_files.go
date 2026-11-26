package desktop

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/markdownfile"
	"github.com/caelis-labs/caelis-bot/internal/messageimage"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const localChatFilePrefix = "local-chat-file:"

var errLocalChatFile = errors.New("文件已不可用")

type localChatFile struct {
	path string
	info os.FileInfo
}

type localChatFiles struct {
	snapshot func(uint64, string, string) api.ChatUpdate
	mu       sync.Mutex
	files    map[string]localChatFile
}

// LocalChatSnapshot adds local references at the native desktop boundary.
// The IM, shared backend snapshot and channel artifact exports never receive them.
func (s *Service) LocalChatSnapshot(revision uint64, botStatus, processEpoch string) api.ChatUpdate {
	if s.localChat.snapshot == nil {
		return api.ChatUpdate{}
	}
	s.localChat.mu.Lock()
	defer s.localChat.mu.Unlock()
	update := s.localChat.snapshot(revision, botStatus, processEpoch)
	if !update.Changed {
		return update
	}
	update.Snapshot.Items = slices.Clone(update.Snapshot.Items)
	files := make(map[string]localChatFile)
	for i := range update.Snapshot.Items {
		projectLocalChatFiles(&update.Snapshot.Items[i], files)
	}
	s.localChat.files = files
	return update
}

func projectLocalChatFiles(item *api.Item, files map[string]localChatFile) {
	if item.Kind != "assistant" || (item.Status != "completed" && item.Status != "accepted") || !strings.Contains(item.Text, "]") || !strings.ContainsAny(item.Text, "/\\") {
		return
	}
	source := []byte(item.Text)
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	indices := make(map[string]int, len(item.Links))
	for i, link := range item.Links {
		indices[markdownfile.Decode(link.Destination)] = i
	}
	seen := make(map[string]bool)
	item.Links = slices.Clone(item.Links)
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var target []byte
		var image bool
		switch link := node.(type) {
		case *ast.Link:
			target = link.Destination
		case *ast.Image:
			target, image = link.Destination, true
		default:
			return ast.WalkContinue, nil
		}
		destination := string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(target))))
		path := localChatPath(destination)
		if path == "" {
			return ast.WalkContinue, nil
		}
		key := markdownfile.Decode(destination)
		index, bound := indices[key]
		if seen[key] {
			if link := &item.Links[index]; image && link.Image == nil && link.File.ID != "" {
				link.Image = &api.MediaImage{ID: link.File.ID, Name: link.File.Name}
			}
			return ast.WalkContinue, nil
		}
		seen[key] = true
		link := api.ChatLink{Destination: destination, File: api.Artifact{Name: artifactDisplayName(path)}}
		if file, ok := resolveLocalChatFile(path); ok {
			identity := fmt.Sprintf("%s\x00%d\x00%d", file.path, file.info.Size(), file.info.ModTime().UnixNano())
			id := fmt.Sprintf("%s%x", localChatFilePrefix, sha256.Sum256([]byte(identity)))
			files[id] = file
			link.File.ID = id
			if image || localChatImageExt(file.path) {
				link.Image = &api.MediaImage{ID: id, Name: link.File.Name}
			}
		}
		if bound {
			item.Links[index] = link
		} else {
			indices[key] = len(item.Links)
			item.Links = append(item.Links, link)
		}
		return ast.WalkContinue, nil
	})
}

func localChatPath(destination string) string {
	value := markdownfile.Path(destination)
	if !localChatAbsolute(value) {
		return ""
	}
	return filepath.Clean(value)
}

func localChatAbsolute(value string) bool {
	return filepath.IsAbs(value) && !strings.HasPrefix(value, "//") && !strings.HasPrefix(value, `\\`) &&
		!strings.ContainsAny(value, "\x00\r\n") && markdownfile.ScopePath(value) != ""
}

func resolveLocalChatFile(path string) (localChatFile, bool) {
	name, ok := markdownfile.RegularFile(path)
	if !ok {
		return localChatFile{}, false
	}
	real, err := filepath.EvalSymlinks(name)
	if err != nil || !localChatAbsolute(real) || markdownfile.ScopePath(real) != real {
		return localChatFile{}, false
	}
	info, err := os.Lstat(real)
	return localChatFile{path: real, info: info}, err == nil && info.Mode().IsRegular()
}

func localChatImageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func (s *Service) openLocalChatFile(id string) (*os.File, error) {
	s.localChat.mu.Lock()
	file, found := s.localChat.files[id]
	s.localChat.mu.Unlock()
	if !found {
		return nil, errLocalChatFile
	}
	real, err := filepath.EvalSymlinks(file.path)
	if err != nil || real != file.path {
		return nil, errLocalChatFile
	}
	opened, err := openLocalRegularFile(file.path)
	if err != nil {
		return nil, errLocalChatFile
	}
	info, err := opened.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, file.info) ||
		info.Size() != file.info.Size() || !info.ModTime().Equal(file.info.ModTime()) {
		opened.Close()
		return nil, errLocalChatFile
	}
	return opened, nil
}

// LocalChatImage reads the currently cited file through its desktop handle.
func (s *Service) LocalChatImage(id string, thumbnail bool) (string, error) {
	f, err := s.openLocalChatFile(id)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return messageimage.FileDataURL(f, thumbnail)
}

func (s *Service) OpenLocalChatFile(id string) error {
	f, err := s.openLocalChatFile(id)
	if err != nil {
		return err
	}
	path := f.Name()
	_ = f.Close()
	if s.openArtifactFile == nil || s.openArtifactFile(path) != nil {
		return errLocalChatFile
	}
	return nil
}
