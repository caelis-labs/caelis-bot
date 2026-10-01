package backend

import (
	"context"
	"errors"
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

type roamingAuthoritativeEngine struct{ roamingBoundaryEngine }

func (*roamingAuthoritativeEngine) CurrentExecutionSettings(context.Context) (api.ExecutionSettings, error) {
	return api.ExecutionSettings{Model: "native-authoritative"}, nil
}

func TestNodeRoamingBoundaryKeepsOptionalCapabilitiesAndNativeSettings(t *testing.T) {
	s := NewService(&roamingBoundaryEngine{}, nil, nil, nil, nil)
	if _, err := s.PrepareNodeRoamingEngine(); err != nil {
		t.Fatal(err)
	}
	if s.ModelSettingsAvailable() || s.ExactInterruptAvailable() {
		t.Fatal("wrapper invented optional capability")
	}
	if _, err := s.ExecutionOptions(); err == nil {
		t.Fatal("wrapper invented execution configuration")
	}
	if _, err := s.Models(t.Context()); err == nil {
		t.Fatal("wrapper invented model catalog")
	}
	if err := s.Login(t.Context()); err == nil {
		t.Fatal("wrapper invented authentication")
	}
	if err := s.CancelLogin(t.Context()); err == nil {
		t.Fatal("wrapper invented cancel authentication")
	}
	if input, err := s.ImageInput(t.Context()); err != nil || input.State != "unknown" {
		t.Fatal(input, err)
	}
	native := NewService(&roamingAuthoritativeEngine{}, nil, nil, nil, nil)
	native.ConfigureExecution("unused", api.ExecutionSettings{Model: "stale-cache"})
	if _, err := native.PrepareNodeRoamingEngine(); err != nil {
		t.Fatal(err)
	}
	v, err := native.ExecutionSettings()
	if err != nil || v.Model != "native-authoritative" {
		t.Fatal("native settings lost through wrapper", v, err)
	}
}

func TestNodeRoamingBoundaryLocalRestoreCopiesFreshConfigurationAndGuard(t *testing.T) {
	s := NewService(&roamingBoundaryEngine{}, nil, nil, nil, nil)
	if _, err := s.PrepareNodeRoamingEngine(); err != nil {
		t.Fatal(err)
	}
	s.ConfigureRuntime("old-runtime", api.RuntimeSettings{Runtime: "old"})
	other := NewService(&roamingBoundaryEngine{}, nil, nil, nil, nil)
	other.ConfigureRuntime("fresh-runtime", api.RuntimeSettings{Runtime: "codex", CLIPath: "/fixture/fresh-codex"})
	other.ConfigureExecution("fresh-execution", api.ExecutionSettings{Model: "fresh-model"})
	other.ConfigureWorkExecution("fresh-work", api.WorkExecutionSettings{Model: "fresh-worker"})
	guardCalls := 0
	other.ConfigureRuntimeManagement(nil, nil, func(context.Context, string, api.RuntimeSettings) (api.RuntimeStatus, error) {
		t.Error("busy local guard bypassed")
		return api.RuntimeStatus{}, nil
	}, func() error { guardCalls++; return errors.New("fresh owner busy") })
	if err := s.ActivateNodeRoamingLocal(other); err != nil {
		t.Fatal(err)
	}
	if s.RuntimeSettings().CLIPath != "/fixture/fresh-codex" || s.runtimeFile != "fresh-runtime" || s.executionFile != "fresh-execution" || s.workExecutionFile != "fresh-work" {
		t.Fatal("source configuration reused")
	}
	if v, err := s.ExecutionSettings(); err != nil || v.Model != "fresh-model" {
		t.Fatal(v, err)
	}
	if s.WorkExecutionSettings().Model != "fresh-worker" {
		t.Fatal("fresh Worker settings lost")
	}
	if _, err := s.ManageRuntime(t.Context(), "stop", s.RuntimeSettings()); err == nil || guardCalls != 1 {
		t.Fatal("fresh native guard lost", err)
	}
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

type roamingBoundedEngine struct{ roamingBoundaryEngine }

func (*roamingBoundedEngine) Snapshot() api.Snapshot {
	panic("bounded projection visited full history")
}
func (*roamingBoundedEngine) Revision() uint64 { return 7 }
func (*roamingBoundedEngine) RecentSnapshot() api.Snapshot {
	return api.Snapshot{Revision: 7, Connection: "recent"}
}
func (*roamingBoundedEngine) ComposerSnapshot() api.Snapshot {
	return api.Snapshot{Revision: 7, Connection: "composer"}
}
func TestNodeRoamingBoundaryPreservesBoundedNativeProjections(t *testing.T) {
	s := NewService(&roamingBoundedEngine{}, nil, nil, nil, nil)
	proxy, err := s.PrepareNodeRoamingEngine()
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Revision() != 7 || proxy.RecentSnapshot().Connection != "recent" || proxy.ComposerSnapshot().Connection != "composer" {
		t.Fatal("native bounded port lost")
	}
	if err := proxy.Replace(&roamingBoundedEngine{}); err != nil {
		t.Fatal(err)
	}
	revision := uint64(7) + (1 << 32)
	if proxy.Revision() != revision || proxy.RecentSnapshot().Revision != revision || proxy.ComposerSnapshot().Revision != revision {
		t.Fatal("bounded swap revision lost")
	}
	if s.ComposerSnapshot().Revision != revision {
		t.Fatal("service lost bounded native composer")
	}
}
