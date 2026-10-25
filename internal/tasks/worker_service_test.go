package tasks

import (
	"context"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type sharedOutputFixture struct {
	*fixtureRuntime
	items    []api.Item
	messages []api.TaskConversationMessage
}

func (f *sharedOutputFixture) ObserveWork(_ context.Context, id string) (api.WorkObservation, error) {
	return api.WorkObservation{State: f.states[id], Items: f.items}, nil
}
func (f *sharedOutputFixture) ReadWorkConversation(context.Context, string) ([]api.TaskConversationMessage, error) {
	return f.messages, nil
}

func TestWorkerFileProjectionDoesNotRewriteNativeReferences(t *testing.T) {
	f := &sharedOutputFixture{fixtureRuntime: newRuntime(), items: []api.Item{{ID: "live", Artifacts: []api.Artifact{{ID: "native-file", Name: "result.txt"}}}}, messages: []api.TaskConversationMessage{{ID: "message", Artifacts: []api.Artifact{{ID: "native-file", Name: "result.txt"}}}}}
	root := t.TempDir()
	m, err := Open(root+"/tasks.json", root+"/Tasks", "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	task := start(t, m, "original")
	for range 2 {
		observed, err := m.ObserveTask(t.Context(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		history, err := m.ReadTaskConversation(t.Context(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{observed.Items[0].Artifacts[0].ID, history.Messages[0].Artifacts[0].ID} {
			ref, ok := api.ParseTaskArtifactID(id)
			if !ok || ref.TaskID != task.ID || ref.ID != "native-file" {
				t.Fatal("native file reference was nested or rebound", ref)
			}
		}
	}
	if f.items[0].Artifacts[0].ID != "native-file" || f.messages[0].Artifacts[0].ID != "native-file" {
		t.Fatal("product projection mutated native authority")
	}
}
