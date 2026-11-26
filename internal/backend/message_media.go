package backend

import (
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/messageimage"
)

func ConfigureMessageMedia(s *Service, root string) error {
	s.messageMedia, s.messageMediaError = messageimage.Open(root)
	if s.messageMediaError == nil {
		s.mu.Lock()
		for _, item := range s.messageMedia.Pending() {
			s.outbox = append(s.outbox, outgoingMessage{item: item, after: restoredOutgoingTail})
		}
		s.mu.Unlock()
	}
	return s.messageMediaError
}

const restoredOutgoingTail = "__restored_outgoing_tail__"

// MediaImage is an opaque, read-only, App-owned byte lookup for ordinary chat
// images. It cannot be used to address an arbitrary local file.
func (s *Service) MediaImage(id string, thumbnail bool) (string, error) {
	return s.messageMedia.DataURL(id, thumbnail)
}

func (s *Service) retainMessageMedia(in api.Submission, files []api.InputFile) error {
	if in.ScreenInput || len(files) == 0 || s.messageMedia == nil {
		return s.messageMediaError
	}
	if err := s.messageMedia.Save(in.ID, in.Text, files); err != nil {
		return err
	}
	text := in.Text
	for _, file := range files {
		text += "\n" + file.Name
	}
	return s.messageMedia.Mark(in.ID, strings.TrimSpace(text), "sending")
}

// Only sources admitted by the owning adapter reach the private media store.
func (s *Service) retainChatImages(item api.Item) api.Item {
	if item.ImageOutput != nil {
		item.Media = s.messageMedia.RetainResult(item.ID, item.ImageOutput)
		item.ImageOutput = nil
	}
	if len(item.Links) > 0 {
		item.Links = append([]api.ChatLink(nil), item.Links...)
		for i := range item.Links {
			link := &item.Links[i]
			if link.Source != nil {
				if retained := s.messageMedia.RetainResult(link.File.ID, link.Source); retained != nil {
					link.Image = &retained.Images[0]
				}
				link.Source = nil
			}
		}
	}
	return item
}

// The native item's reply boundary also applies when the IM already held that
// reply before the image arrived, such as during recovery from an earlier build.
func positionChatResults(items []api.Item) []api.Item {
	var positions map[string][]api.Item
	for _, item := range items {
		if item.BeforeItem != "" && item.HasToolResult() {
			if positions == nil {
				positions = make(map[string][]api.Item)
			}
			positions[item.BeforeItem] = append(positions[item.BeforeItem], item)
		}
	}
	if len(positions) == 0 {
		return items
	}
	found := make(map[string]api.Item)
	for _, item := range items {
		if item.Kind == "assistant" && len(positions[item.ID]) > 0 {
			found[item.ID] = item
		}
	}
	ordered := make([]api.Item, 0, len(items))
	for _, item := range items {
		if reply, ok := found[item.BeforeItem]; ok && item.TurnKey == reply.TurnKey && item.HasToolResult() {
			continue
		}
		for _, result := range positions[item.ID] {
			if item.Kind == "assistant" && item.TurnKey == result.TurnKey {
				result.SeenAt = item.SeenAt
				ordered = append(ordered, result)
			}
		}
		ordered = append(ordered, item)
	}
	return ordered
}

// The same file handle remains available to the text channels, while the
// desktop renders its action at the original Markdown position.
func presentInlineChatLinks(item api.Item) api.Item {
	if item.ChatText == "" {
		return item
	}
	item.Text, item.ChatText = item.ChatText, ""
	inline := make(map[string]bool, len(item.Links))
	for _, link := range item.Links {
		inline[link.File.ID] = true
	}
	if len(item.Artifacts) > 0 {
		files := make([]api.Artifact, 0, len(item.Artifacts))
		for _, file := range item.Artifacts {
			if !inline[file.ID] {
				files = append(files, file)
			}
		}
		item.Artifacts = files
	}
	return item
}
