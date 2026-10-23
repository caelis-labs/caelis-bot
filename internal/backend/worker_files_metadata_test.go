package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type metadataArtifactFixture struct {
	api.Engine
	path  string
	calls int
}

func (f *metadataArtifactFixture) Artifact(id string) (string, error) {
	f.calls++
	if id != "legacy-native-file" {
		return "", errors.New("not owned")
	}
	return f.path, nil
}

type metadataWorkerReader struct {
	name string
}

func (f metadataWorkerReader) ReadTaskFile(_ context.Context, task, ref string) (api.WorkFileContent, error) {
	if task != "owned-task" || ref != "native-file" {
		return api.WorkFileContent{}, errors.New("file not owned")
	}
	return api.WorkFileContent{Name: f.name, Data: []byte("owned worker bytes")}, nil
}

func TestArtifactMetadataKeepsLegacyPathAndWorkerOwnership(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "legacy-report.md")
	if err := os.WriteFile(legacy, []byte("legacy bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &metadataArtifactFixture{path: legacy}
	s := NewService(engine, nil, nil, nil, nil)
	copy, err := ResolveRemoteArtifactCopy(s, "legacy-native-file")
	if err != nil || copy.Path != legacy || copy.Name != "legacy-report.md" {
		t.Fatal("legacy native artifact route changed", copy, err)
	}
	if path, err := ResolveRemoteArtifact(s, "legacy-native-file"); err != nil || path != legacy {
		t.Fatal("path-only compatibility was lost", err)
	}
	s.ConfigureWorkerFiles(t.TempDir(), metadataWorkerReader{name: "worker-report.csv"})
	id := api.TaskArtifactID("owned-task", "native-file")
	copy, err = ResolveRemoteArtifactCopy(s, id)
	if err != nil || copy.Name != "worker-report.csv" || filepath.Base(copy.Path) != copy.Name {
		t.Fatal("legacy Worker reader lost name or path compatibility", copy, err)
	}
	encoded, err := json.Marshal(copy)
	if err != nil || strings.Contains(string(encoded), copy.Path) || strings.Contains(string(encoded), `"path"`) {
		t.Fatal("host-only artifact path entered renderer metadata")
	}
	if _, err := ResolveRemoteArtifactCopy(s, api.TaskArtifactID("another-task", "native-file")); err == nil || engine.calls != 2 {
		t.Fatal("Worker ownership failure fell back to the resident artifact resolver")
	}
	for _, name := range []string{"../unsafe.csv", "unsafe\r\nheader.csv", ".", "..", ""} {
		s.ConfigureWorkerFiles(t.TempDir(), metadataWorkerReader{name: name})
		if _, err := ResolveRemoteArtifactCopy(s, id); err == nil {
			t.Fatal("unverified Worker filename became export metadata")
		}
	}
}
