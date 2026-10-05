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
