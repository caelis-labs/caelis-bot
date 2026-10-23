//go:build webfixture

package desktop

import (
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"path/filepath"
)

// NewWebFixture reuses real attachment, preference, projection and draft code.
// It exists only in explicitly tagged fixture builds, never the shipped binary.
func NewWebFixture(root string, engine api.Engine) (*backend.Service, *Service, error) {
	core, s, err := NewHeadlessProduct(root)
	if err != nil {
		return nil, nil, err
	}
	core.Close()
	b := backend.NewService(engine, s.resolveDraftFiles, s.consumeDraftFilesChecked, nil, nil)
	b.ConfigureChat(filepath.Join(root, "fixture-chat.sqlite"))
	if err = b.ConfigureDraft(filepath.Join(root, "fixture-draft.json")); err != nil {
		return nil, nil, err
	}
	s.resolveArtifact = func(id string) (string, error) { return backend.ResolveRemoteArtifact(b, id) }
	s.resolveArtifactCopy = func(id string) (api.TaskFileCopy, error) { return backend.ResolveRemoteArtifactCopy(b, id) }
	return b, s, nil
}
