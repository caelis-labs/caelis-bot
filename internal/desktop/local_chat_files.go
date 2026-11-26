package desktop

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
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
		indices[decodedChatDestination(link.Destination)] = i
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
		key := decodedChatDestination(destination)
		index, bound := indices[key]
		if seen[key] {
			if link := &item.Links[index]; image && link.Image == nil && link.File.ID != "" {
				link.Image = &api.MediaImage{ID: link.File.ID, Name: link.File.Name}
			}
			return ast.WalkContinue, nil
		}
		seen[key] = true
		link := api.ChatLink{Destination: destination, File: api.Artifact{Name: artifactDisplayName(path)}}
		if real, err := filepath.EvalSymlinks(path); err == nil && localChatAbsolute(real) {
			if info, err := os.Lstat(real); err == nil && info.Mode().IsRegular() {
				identity := fmt.Sprintf("%s\x00%d\x00%d", real, info.Size(), info.ModTime().UnixNano())
				id := fmt.Sprintf("%s%x", localChatFilePrefix, sha256.Sum256([]byte(identity)))
				files[id] = localChatFile{path: real, info: info}
				link.File.ID = id
				if image || localChatImageExt(path) {
					link.Image = &api.MediaImage{ID: id, Name: link.File.Name}
				}
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

func decodedChatDestination(destination string) string {
	value, err := url.PathUnescape(destination)
	if err != nil {
		return destination
	}
	return value
}

func localChatPath(destination string) string {
	value := decodedChatDestination(destination)
	if strings.HasPrefix(strings.ToLower(destination), "file:") {
		u, err := url.Parse(destination)
		if err != nil || u.Opaque != "" || u.Host != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return ""
		}
		value = u.Path
		if len(value) > 1 && value[0] == '/' && filepath.VolumeName(value[1:]) != "" {
			value = value[1:]
		}
	}
	if !localChatAbsolute(value) {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(value))
}

func localChatAbsolute(value string) bool {
	volume := filepath.VolumeName(value)
	return filepath.IsAbs(value) && !strings.HasPrefix(value, "//") && !strings.HasPrefix(value, `\\`) &&
		!strings.ContainsAny(value, "\x00\r\n") && (volume == "" || !strings.Contains(value[len(volume):], ":"))
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
