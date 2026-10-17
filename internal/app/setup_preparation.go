package app

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

// This short-lived snapshot is a UI/preflight optimization, never permission to
// submit work. New adapters still perform their normal authoritative handshake.
// Explicit recheck and mutations discard it; nothing is persisted with secrets.
const setupPreparationLifetime = 30 * time.Second

type setupFile struct {
	path string
	info os.FileInfo
}
type setupPreparation struct {
	state    api.SetupState
	until    time.Time
	files    []setupFile
	revision string
}

func (s *runtimeSetup) preparationFiles(v api.SetupState) ([]setupFile, bool) {
	directory, _ := providerDirectory(s.app.root, v.Settings.Runtime)
	paths := []string{v.Installation.Path, filepath.Join(directory, "execution.json"), filepath.Join(directory, "work-execution.json")}
	if v.Settings.Runtime == "codex" {
		home, live := s.codex.PreparedHome(v.Installation.Path)
		if !live || home == "" {
			return nil, false
		}
		paths = append(paths, filepath.Join(home, "auth.json"), filepath.Join(home, "config.toml"))
	}
	var files []setupFile
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, false
		}
		files = append(files, setupFile{path, info})
	}
	return files, len(files) > 0 && files[0].info != nil
}

func sameSetupFiles(a, b []setupFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		y := b[i]
		if x.path != y.path {
			return false
		}
		if x.info == nil || y.info == nil {
			if x.info != nil || y.info != nil {
				return false
			}
			continue
		}
		if !os.SameFile(x.info, y.info) || x.info.Size() != y.info.Size() || !x.info.ModTime().Equal(y.info.ModTime()) {
			return false
		}
	}
	return true
}

func (s *runtimeSetup) rememberPreparation(ctx context.Context, state api.SetupState) {
	delete(s.prepared, state.Settings.Runtime)
	if state.LoginPending || (state.State != "ready" && state.State != "models") {
		delete(s.prepared, state.Settings.Runtime)
		return
	}
	files, ok := s.preparationFiles(state)
	if !ok {
		return
	}
	revision := ""
	if state.Settings.Runtime == "caelis" {
		var err error
		revision, err = caelis.SetupRevision(ctx, state.Settings)
		if err != nil {
			return
		}
	}
	if s.prepared == nil {
		s.prepared = map[string]*setupPreparation{}
	}
	s.prepared[state.Settings.Runtime] = &setupPreparation{state: state, until: time.Now().Add(setupPreparationLifetime), files: files, revision: revision}
}

func (s *runtimeSetup) preparationValid(ctx context.Context, p *setupPreparation, v api.RuntimeSettings) bool {
	if p == nil || p.state.Settings != v || time.Now().After(p.until) {
		return false
	}
	files, ok := s.preparationFiles(p.state)
	if !ok || !sameSetupFiles(files, p.files) {
		return false
	}
	if v.Runtime == "caelis" {
		revision, err := caelis.SetupRevision(ctx, v)
		if err != nil || revision != p.revision {
			return false
		}
	}
	return true
}

func (s *runtimeSetup) preparedState(ctx context.Context, v api.RuntimeSettings) (api.SetupState, error) {
	if p := s.prepared[v.Runtime]; s.preparationValid(ctx, p, v) {
		return p.state, nil
	}
	delete(s.prepared, v.Runtime)
	return s.inspect(ctx, v)
}

func (s *runtimeSetup) prepare(ctx context.Context, v api.RuntimeSettings) (api.SetupState, error) {
	if p := s.prepared[v.Runtime]; s.preparationValid(ctx, p, v) {
		return p.state, nil
	}
	// The same coordinator is used by the subsequent resident handshake. Do not
	// pause a working resident or query a not-yet-started Host's model catalog.
	if v.Runtime == "caelis" {
		if err := caelisruntime.PrepareBundled(ctx, v.CLIPath, v.CaelisStore); err != nil {
			return api.SetupState{}, err
		}
	}
	return s.inspect(ctx, v)
}

func (s *runtimeSetup) canReuseRuntime(v api.RuntimeSettings) bool {
	if !s.app.HasRuntimeChoice() || s.app.Backend.RuntimeSettings() != v || s.activeEngine != s.app.currentEngine() || s.app.currentEngine().Snapshot().Connection != "ready" {
		return false
	}
	// Only reuse the exact resident we activated, with unchanged files and Host
	// revision. Expired catalog snapshots do not imply a broken resident.
	old, next := s.activePreparation, s.prepared[v.Runtime]
	return old != nil && next != nil && old.state.Settings == v && old.revision == next.revision && sameSetupFiles(old.files, next.files)
}
