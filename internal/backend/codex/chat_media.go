package codex

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/markdownfile"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const citationPrefix = "chat-citation-"

func chatItemText(item api.Item) string {
	if item.ChatText != "" {
		return item.ChatText
	}
	return item.Text
}

func (s *Session) projectChatLinks(view *api.Item) {
	if s.opts.Directory == "" || !strings.Contains(view.Text, "]") {
		return
	}
	document := goldmark.DefaultParser().Parse(text.NewReader([]byte(view.Text)))
	seen := make(map[string]bool)
	artifacts := make(map[string]bool)
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var target []byte
		switch n := node.(type) {
		case *ast.Link:
			target = n.Destination
		case *ast.Image:
			target = n.Destination
		default:
			return ast.WalkContinue, nil
		}
		destination := string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(target))))
		value := markdownfile.Decode(destination)
		if !seen[value] {
			seen[value] = true
			if link, ok := s.chatLink(destination); ok {
				view.Links = append(view.Links, link)
				if link.File.ID != "" && !artifacts[link.File.ID] {
					view.Artifacts = append(view.Artifacts, link.File)
					artifacts[link.File.ID] = true
				}
			}
		}
		return ast.WalkContinue, nil
	})
	if len(view.Links) == 0 {
		return
	}
	view.ChatText = view.Text
	destinations := make(map[string]bool, len(view.Links))
	for _, link := range view.Links {
		destinations[link.Destination] = true
	}
	// Keep the established text-only projection for the channels. Only the
	// parsed links admitted above participate in this compatibility path.
	view.Text = artifactLink.ReplaceAllStringFunc(view.Text, func(match string) string {
		parts := artifactLink.FindStringSubmatch(match)
		if destinations[strings.Trim(parts[2], "<>")] {
			return parts[1]
		}
		return match
	})
}

func (s *Session) chatLink(destination string) (api.ChatLink, bool) {
	path := markdownfile.Path(destination)
	if path == "" || strings.HasPrefix(destination, "#") {
		return api.ChatLink{}, false
	}
	if !filepath.IsAbs(path) {
		u, err := url.Parse(destination)
		if err != nil || u.Scheme != "" || u.Host != "" || filepath.VolumeName(path) != "" {
			return api.ChatLink{}, false
		}
		path = filepath.Join(s.opts.Directory, path)
	}
	path = filepath.Clean(path)
	scope := markdownfile.ScopePath(path)
	// Only Notebook paths (except private uploads) or already-bound temporary
	// results can become file citations. A missing Notebook path has no handle.
	if scope == "" || privateChatAttachment(s.opts.Directory, scope) ||
		!notebookRelative(s.opts.Directory, scope) && !s.boundTempArtifact(path) {
		return api.ChatLink{}, false
	}
	link := api.ChatLink{Destination: destination, File: api.Artifact{Name: filepath.Base(path)}}
	file, root, ok := s.citationFile(path)
	if !ok {
		return link, true
	}
	id := opaque("artifact", path)
	if !s.registerArtifact(citationPrefix+id, path) {
		return link, true
	}
	link.File.ID = id
	switch strings.ToLower(filepath.Ext(file)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		if root != "" {
			link.Source = chatImageSource(root, file)
		}
	}
	return link, true
}

func notebookRelative(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return false
	}
	return !privateChatAttachment(root, path)
}

func privateChatAttachment(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	return strings.EqualFold(first, ".attachments")
}

func (s *Session) citationFile(path string) (file, root string, ok bool) {
	file, ok = markdownfile.RegularFile(path)
	if !ok || privateChatAttachment(s.opts.Directory, file) {
		return "", "", false
	}
	if !notebookRelative(s.opts.Directory, file) {
		if s.boundTempArtifact(path) {
			return file, "", true
		}
		return "", "", false
	}
	root, err := filepath.EvalSymlinks(s.opts.Directory)
	real, realErr := filepath.EvalSymlinks(file)
	if err == nil && realErr == nil && notebookRelative(root, real) {
		return real, root, true
	}
	// The installed bot-core link is a specific, existing file citation.
	if file == filepath.Join(s.opts.Directory, ".agents", "skills", "bot-core", "SKILL.md") && realErr == nil {
		want, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(s.opts.Directory), "app-skills", "bot-core", "SKILL.md"))
		if err == nil && real == want {
			return file, "", true
		}
	}
	return "", "", false
}

func chatImageSource(root, file string) *api.ChatImageSource {
	rel, err := filepath.Rel(root, file)
	if err != nil || !filepath.IsLocal(rel) {
		return nil
	}
	return &api.ChatImageSource{Name: filepath.Base(file), Open: func() (*os.File, error) {
		if real, err := filepath.EvalSymlinks(root); err != nil || real != root {
			return nil, errors.New("chat image unavailable")
		}
		scope, err := os.OpenRoot(root)
		if err != nil {
			return nil, err
		}
		defer scope.Close()
		info, err := scope.Lstat(rel)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("chat image unavailable")
		}
		f, err := scope.Open(rel)
		if err != nil {
			return nil, err
		}
		current, err := f.Stat()
		if err != nil || !os.SameFile(info, current) {
			f.Close()
			return nil, errors.New("chat image unavailable")
		}
		return f, nil
	}}
}

func (s *Session) projectImageOutput(thread string, view *api.Item, file string) {
	if s.client == nil || thread == "" || thread != s.binding.ThreadID || filepath.Base(thread) != thread || !filepath.IsLocal(thread) ||
		!filepath.IsAbs(s.client.home) || !filepath.IsAbs(file) {
		return
	}
	root := filepath.Join(s.client.home, "generated_images", thread)
	file = filepath.Clean(file)
	if filepath.Dir(file) != root || !strings.EqualFold(filepath.Ext(file), ".png") {
		return
	}
	home, err := filepath.EvalSymlinks(s.client.home)
	canonical := filepath.Join(home, "generated_images", thread)
	real, realErr := filepath.EvalSymlinks(root)
	if err != nil || realErr == nil && real != canonical || realErr != nil && !errors.Is(realErr, os.ErrNotExist) {
		return
	}
	view.Kind, view.Text = "tool", ""
	view.ImageOutput = chatImageSource(canonical, filepath.Join(canonical, filepath.Base(file)))
}

// The native summary omits ImageGen items and older Markdown. Restore missing
// displays from one recent page after admission. This read does not apply
// historical turn or tool lifecycle.
func (s *Session) observeRecentChatMedia(c *Client, epoch uint64, thread string) {
	if !filepath.IsAbs(c.home) || !filepath.IsLocal(thread) || filepath.Base(thread) != thread {
		return
	}
	dir := filepath.Join(c.home, "generated_images", thread)
	s.mu.Lock()
	hasFiles := len(s.binding.Artifacts) > 0
	s.mu.Unlock()
	if !hasFiles {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return
		}
	}
	ctx, cancel := s.operation(context.Background(), 5*time.Second)
	defer cancel()
	const limit = 5
	var page turnPage
	if err := callDecode(ctx, c, "thread/turns/list", map[string]any{
		"threadId": thread, "limit": limit, "sortDirection": "desc", "itemsView": "full",
	}, &page); err != nil || len(page.Data) > limit {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != c || s.epoch != epoch || s.binding.ThreadID != thread || s.closed || s.closing {
		return
	}
	changed := false
	var repairs []api.Item
	for _, turn := range slices.Backward(page.Data) {
		if turn.ID == "" {
			continue
		}
		anchors := imageReplyAnchors(turn)
		for _, item := range turn.Items {
			if item.ID == "" {
				continue
			}
			key := opaque(turn.ID, item.ID)
			switch {
			case item.Type == "agentMessage" && terminal(turn.Status) && len(item.Questions) == 0 && len(repairs) < 100:
				reply := api.Item{ID: key, TurnKey: opaque(turn.ID), Text: item.Text}
				s.projectChatLinks(&reply)
				if len(reply.Links) > 0 {
					repairs = append(repairs, reply)
				}
			case item.Type == "imageGeneration" && item.Status == "completed":
				s.applyItem(thread, turn.ID, item, true)
				if index, ok := s.items[key]; ok && s.state.Items[index].ImageOutput != nil {
					if before := anchors[item.ID]; before != "" {
						s.state.Items[index].BeforeItem = before
					}
					changed = true
				}
			}
		}
	}
	s.state.ChatLinkRepairs = repairs
	if changed || len(repairs) > 0 {
		s.update()
	}
}

func imageReplyAnchors(turn nativeTurn) map[string]string {
	var nextReply string
	anchors := make(map[string]string)
	for _, item := range slices.Backward(turn.Items) {
		switch item.Type {
		case "agentMessage":
			if item.ID != "" && strings.TrimSpace(item.Text) != "" {
				nextReply = opaque(turn.ID, item.ID)
			}
		case "imageGeneration":
			anchors[item.ID] = nextReply
		}
	}
	return anchors
}

// A delayed completion can arrive after an already visible final reply.
func (s *Session) positionLateImage(turn, id string) {
	index, ok := s.items[opaque(turn, id)]
	if !ok || s.state.Items[index].ImageOutput == nil || s.state.Items[index].BeforeItem != "" {
		return
	}
	result := &s.state.Items[index]
	for _, reply := range slices.Backward(s.state.Items) {
		if reply.TurnKey == result.TurnKey && reply.Kind == "assistant" && strings.TrimSpace(reply.Text) != "" {
			result.BeforeItem = reply.ID
			return
		}
	}
}
