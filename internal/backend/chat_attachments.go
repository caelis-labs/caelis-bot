package backend

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

// The retained upload manifest is shared by live submissions and older IM
// rows. Only remove the exact appended filename list from their display text.
func (s *Service) presentChatAttachments(item api.Item) api.Item {
	if item.Kind != "user" || item.Screen != nil || item.QuestionReply != nil {
		return item
	}
	s.mu.Lock()
	store := s.workerInputs
	s.mu.Unlock()
	refs := store.References(item.RequestID)
	if len(refs) == 0 {
		return item
	}
	images := map[string]int{}
	if item.Media != nil {
		for _, image := range item.Media.Images {
			images[image.Name]++
		}
	}
	names := make([]string, 0, len(refs))
	files := make([]api.Artifact, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
		if images[ref.Name] > 0 {
			images[ref.Name]--
			continue
		}
		files = append(files, api.Artifact{ID: ref.ID, Name: ref.Name})
	}
	if len(files) == 0 {
		return item
	}
	item.Artifacts = append(append([]api.Artifact(nil), item.Artifacts...), files...)
	if item.Status == "received" {
		return item // remote ingress has the original caption before dispatch
	}
	suffix := strings.TrimRightFunc(strings.Join(names, "\n"), unicode.IsSpace)
	if caption, ok := strings.CutSuffix(item.Text, "\n"+suffix); ok {
		item.Text = strings.TrimSpace(caption)
	} else if item.Text == strings.TrimSpace(suffix) {
		item.Text = ""
	}
	return item
}

func (s *Service) ConfigureUploadArtifactCopies(copies *workerfiles.Outputs) {
	s.uploadArtifactCopies = copies
}

func (s *Service) uploadArtifactCopy(id string) (api.TaskFileCopy, error) {
	if s.uploadArtifactCopies == nil {
		return api.TaskFileCopy{}, errors.New("attachment unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Validate the original bytes on each action. An application-openable copy
	// may be cleaned independently and cannot substitute for a missing original.
	file, _, err := ReadWorkerInput(ctx, s, id)
	if err != nil {
		return api.TaskFileCopy{}, err
	}
	copy, err := s.uploadArtifactCopies.Materialize(ctx, "resident-upload", id, true, func(context.Context) (api.WorkFileContent, error) {
		return file, nil
	})
	copy.ID = id
	return copy, err
}
