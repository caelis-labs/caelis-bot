package backend

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

func TestSentAttachmentsUseRetainedMetadataAfterReload(t *testing.T) {
	root := t.TempDir()
	engine := &selectedUploadEngine{}
	files := []api.InputFile{
		selectedUploadFixture(t, strings.Repeat("年度合规补充说明", 6)+".txt", []byte("original body")),
		selectedUploadFixture(t, "costs.csv", []byte("region,cost\nwest,10")),
	}
	open := func() *Service {
		s := NewService(engine, nil, nil, nil, nil)
		ConfigureWorkerInputs(s, filepath.Join(root, "uploads"))
		s.ConfigureUploadArtifactCopies(workerfiles.OpenOutputs(filepath.Join(root, "copies")))
		s.ConfigureChat(filepath.Join(root, "chat.sqlite"))
		return s
	}
	read := func(s *Service) []api.Item {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if items := s.ChatSnapshot(0, "", "").Snapshot.Items; len(items) == 2 {
				return items
			}
		}
		t.Fatal("expected both original IM rows")
		return nil
	}
	s := open()
	for i, input := range []api.Submission{{ID: "multiple-files", Text: "I already mentioned " + files[1].Name}, {ID: "file-only"}} {
		if receipt, err := SubmitRemote(t.Context(), s, input, files[i:]); err != nil || receipt.Outcome != "accepted" {
			t.Fatal(receipt, err)
		}
	}
	live := read(s)
	s.chat.Close()
	for _, f := range files {
		if err := os.Remove(f.Path); err != nil {
			t.Fatal(err)
		}
	}
	reopened := open()
	defer reopened.chat.Close()
	history := read(reopened)
	for i, want := range []string{"I already mentioned " + files[1].Name, ""} {
		if history[i].Text != want || live[i].Text != want || len(live[i].Artifacts) != len(files)-i || !reflect.DeepEqual(live[i].Artifacts, history[i].Artifacts) {
			t.Fatalf("original caption or artifacts changed on reload: %+v / %+v", live[i], history[i])
		}
		for j, file := range history[i].Artifacts {
			if file.Name != files[i+j].Name || !workerfiles.IsInputReference(file.ID) {
				t.Fatalf("unexpected attachment metadata: %+v", file)
			}
		}
	}
	first := history[0].Artifacts[0]
	copy, err := ResolveRemoteArtifactCopy(reopened, first.ID)
	if err != nil || copy.Name != first.Name || filepath.Ext(copy.Path) != ".txt" {
		t.Fatalf("preview/open lost the verified name or extension: %+v, %v", copy, err)
	}
	if data, err := os.ReadFile(copy.Path); err != nil || string(data) != "original body" {
		t.Fatalf("opened copy does not contain the selected bytes: %v", err)
	}
	if err := os.WriteFile(engine.files[0][0].Path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRemoteArtifactCopy(reopened, first.ID); err == nil {
		t.Fatal("stale openable copy masked a corrupt original")
	}
}
