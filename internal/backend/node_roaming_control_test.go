package backend

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type roamingBoundaryEngine struct {
	api.Engine
	commands atomic.Int32
}

func (*roamingBoundaryEngine) Snapshot() api.Snapshot {
	return api.Snapshot{Revision: 1, Connection: "ready", CanSend: true}
}
func (e *roamingBoundaryEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.commands.Add(1)
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}

type roamingDraftEngine struct {
	roamingBoundaryEngine
	draft api.Draft
}

func (e *roamingDraftEngine) Draft() api.Draft                         { return e.draft }
func (e *roamingDraftEngine) SaveDraft(d api.Draft) (api.Draft, error) { e.draft = d; return d, nil }

func TestNodeRoamingBoundaryPreservesLocalDraftAndSwitchesRemoteRouting(t *testing.T) {
	local := &roamingBoundaryEngine{}
	s := NewService(local, func([]string) ([]api.InputFile, error) { return nil, nil }, func([]string) {}, nil, nil)
	if err := s.ConfigureDraft(filepath.Join(t.TempDir(), "draft.json")); err != nil {
		t.Fatal(err)
	}
	proxy, err := s.PrepareNodeRoamingEngine()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "local draft"}); err != nil {
		t.Fatal(err)
	}
	if s.Draft().Text != "local draft" {
		t.Fatal("optional wrapper lost local draft")
	}
	if _, err := s.Submit(t.Context(), api.Submission{ID: "source", Text: "local draft"}); err != nil || local.commands.Load() != 1 {
		t.Fatal(err)
	}
	revision := s.Snapshot().Revision
	remote := &roamingDraftEngine{draft: api.Draft{Text: "remote draft"}}
	if err := s.ActivateNodeRoamingProduct(remote); err != nil {
		t.Fatal(err)
	}
	if proxy.Current() != remote || s.Snapshot().Revision == revision || s.Draft().Text != "remote draft" {
		t.Fatal("remote replacement did not bind new scope")
	}
	if _, err := s.Submit(t.Context(), api.Submission{ID: "remote", Text: "fixture"}); err != nil || remote.commands.Load() != 1 || local.commands.Load() != 1 {
		t.Fatal(err)
	}
	if len(s.outbox) != 1 {
		t.Fatal("thin observer created a second local outbox")
	}
}
func TestNodeRoamingBoundaryConcurrentReadersAndNativeReplacement(t *testing.T) {
	s := NewService(&roamingBoundaryEngine{}, nil, nil, nil, nil)
	proxy, err := s.PrepareNodeRoamingEngine()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Go(func() {
			for j := 0; j < 100; j++ {
				_ = s.Snapshot()
				_ = s.ComposerSnapshot()
				_ = s.PetSnapshot()
			}
		})
	}
	for i := 0; i < 100; i++ {
		if err := proxy.Replace(&roamingBoundaryEngine{}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
