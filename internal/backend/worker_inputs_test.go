package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type selectedUploadEngine struct {
	api.Engine
	submissions []api.Submission
	files       [][]api.InputFile
	before      func([]api.InputFile)
}

func (e *selectedUploadEngine) Snapshot() api.Snapshot { return api.Snapshot{} }
func (e *selectedUploadEngine) Submit(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	if e.before != nil {
		e.before(files)
	}
	e.submissions = append(e.submissions, in)
	e.files = append(e.files, files)
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}

func selectedUploadFixture(t *testing.T, name string, data []byte) api.InputFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "selection")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return api.InputFile{Name: name, Path: path}
}

func uploadedReference(t *testing.T, in api.Submission) string {
	t.Helper()
	_, tail, ok := strings.Cut(in.ModelInputOverride, "<bot_uploaded_files>\n")
	metadata, _, closed := strings.Cut(tail, "\n</bot_uploaded_files>")
	var refs []struct {
		Name       string                `json:"name"`
		Attachment api.TaskFileReference `json:"attachment"`
	}
	if !ok || !closed || json.Unmarshal([]byte(metadata), &refs) != nil || len(refs) != 1 || refs[0].Attachment.TaskID != "" || refs[0].Attachment.ID == "" {
		t.Fatal("missing bounded upload reference metadata", in.ModelInputOverride)
	}
	return refs[0].Attachment.ID
}

func TestWorkerInputsDesktopSubmissionSnapshotsBeforeDispatchAndKeepsVisibleText(t *testing.T) {
	original := []byte("name,value\r\n甲,7\r\n")
	file := selectedUploadFixture(t, "原始 \"材料\" <tag>.csv", original)
	engine := &selectedUploadEngine{}
	consumed := false
	s := NewService(engine, func(ids []string) ([]api.InputFile, error) {
		if len(ids) != 1 || ids[0] != "local-selected" {
			t.Fatal("selection identity changed", ids)
		}
		return []api.InputFile{file}, nil
	}, func([]string) error { consumed = true; return os.Remove(file.Path) }, nil, nil)
	ConfigureWorkerInputs(s, filepath.Join(t.TempDir(), "uploads"))
	if err := s.ConfigureDraft(filepath.Join(t.TempDir(), "draft.json")); err != nil {
		t.Fatal(err)
	}
	s.ConfigureChat(filepath.Join(t.TempDir(), "chat.sqlite"))
	defer s.chat.Close()
	engine.before = func(files []api.InputFile) {
		if len(files) != 1 || files[0].Path == file.Path || files[0].Name != file.Name {
			t.Fatal("native dispatch reread the user-selected source", files)
		}
		if err := os.WriteFile(file.Path, []byte("mutable source changed before native read"), 0600); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(files[0].Path)
		if err != nil || !bytes.Equal(data, original) {
			t.Fatal("native encoder did not receive original snapshot", err)
		}
	}
	input := api.Submission{ID: "desktop-original-upload", Text: "请把原材料交给 Worker", FileIDs: []string{"local-selected"}, Quoted: &api.QuotedMessage{Role: "user", Text: "完整引用 <quote>"}}
	receipt, err := s.Submit(t.Context(), input)
	if err != nil || receipt.Outcome != "accepted" || !consumed || len(engine.submissions) != 1 {
		t.Fatal("submission failed", receipt, err, consumed)
	}
	projected := engine.submissions[0]
	if projected.Text != input.Text || !strings.HasPrefix(projected.ModelInputOverride, input.ModelInputText()+"\n\n") || strings.Contains(projected.ModelInputOverride, file.Path) || strings.Contains(projected.ModelInputOverride, engine.files[0][0].Path) {
		t.Fatal("model references lost original context or leaked a path", projected)
	}
	if !strings.Contains(projected.ModelInputOverride, `\u003ctag\u003e`) {
		t.Fatal("untrusted name was not JSON escaped")
	}
	id := uploadedReference(t, projected)
	v, handled, err := ReadWorkerInput(t.Context(), s, id)
	if err != nil || !handled || v.Name != file.Name || !bytes.Equal(v.Data, original) {
		t.Fatal("consuming the draft lost the retained upload", v, handled, err)
	}
	for _, item := range s.ChatSnapshot(0, "").Snapshot.Items {
		if strings.Contains(item.Text, "bot_uploaded_files") || strings.Contains(item.Text, "upload-file:") || strings.Contains(item.Text, file.Path) || strings.Contains(item.Text, "<reference>") {
			t.Fatal("host-only upload context entered visible chat", item.Text)
		}
	}
}

func TestWorkerInputsRemoteIngressPreservesFullOverrideAndSurvivesReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	file := selectedUploadFixture(t, "material.pdf", []byte("%PDF-fixture-original\x00binary"))
	engine := &selectedUploadEngine{}
	s := NewService(engine, nil, nil, nil, nil)
	ConfigureWorkerInputs(s, root)
	input := api.Submission{ID: "remote-original-upload", Text: "visible user body", ModelInputOverride: "<private_context>complete original model input</private_context>"}
	receipt, err := SubmitRemote(t.Context(), s, input, []api.InputFile{file})
	if err != nil || receipt.Outcome != "accepted" || !strings.HasPrefix(engine.submissions[0].ModelInputOverride, input.ModelInputOverride+"\n\n") || engine.submissions[0].Text != input.Text {
		t.Fatal("remote ingress bypassed retained input or lost an existing override", receipt, err)
	}
	id := uploadedReference(t, engine.submissions[0])
	reopened := NewService(&selectedUploadEngine{}, nil, nil, nil, nil)
	ConfigureWorkerInputs(reopened, root)
	if err := os.Remove(file.Path); err != nil {
		t.Fatal(err)
	}
	v, handled, err := ReadWorkerInput(t.Context(), reopened, id)
	if err != nil || !handled || v.Name != file.Name || string(v.Data) != "%PDF-fixture-original\x00binary" {
		t.Fatal("new resident/store could not read original uploaded bytes", handled, err)
	}
}

func TestWorkerInputsConflictsAndStorageFailureRejectBeforeNativeDispatch(t *testing.T) {
	file := selectedUploadFixture(t, "selected.csv", []byte("a,b\n1,2"))
	engine := &selectedUploadEngine{}
	s := NewService(engine, nil, nil, nil, nil)
	ConfigureWorkerInputs(s, filepath.Join(t.TempDir(), "uploads"))
	input := api.Submission{ID: "unchanged-original-upload", Text: "original text"}
	if receipt, err := SubmitRemote(t.Context(), s, input, []api.InputFile{file}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	input.Text = "different text"
	if receipt, err := SubmitRemote(t.Context(), s, input, []api.InputFile{file}); err != nil || receipt.Outcome != "rejected" || len(engine.submissions) != 1 {
		t.Fatal("changed original intent reached native owner", receipt, err)
	}
	broken := filepath.Join(t.TempDir(), "not-directory")
	if err := os.WriteFile(broken, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ConfigureWorkerInputs(s, broken)
	if receipt, err := SubmitRemote(t.Context(), s, api.Submission{ID: "broken-upload", Text: "with file"}, []api.InputFile{file}); err != nil || receipt.Outcome != "rejected" || len(engine.submissions) != 1 {
		t.Fatal("broken upload cache did not contain attachment failure", receipt, err)
	}
	if receipt, err := SubmitRemote(t.Context(), s, api.Submission{ID: "plain-unaffected", Text: "plain chat"}, nil); err != nil || receipt.Outcome != "accepted" || len(engine.submissions) != 2 {
		t.Fatal("upload failure blocked unrelated plain chat", receipt, err)
	}
	if _, handled, err := ReadWorkerInput(t.Context(), s, "upload-file:forged"); !handled || err == nil {
		t.Fatal("malformed upload reference escaped its owner", handled, err)
	}
}

func TestWorkerInputsUnconfiguredAndScreenInputsKeepExistingBehavior(t *testing.T) {
	file := selectedUploadFixture(t, "source.txt", []byte("original"))
	s := NewService(nil, nil, nil, nil, nil)
	input := api.Submission{ID: "old-embedder", Text: "plain"}
	projected, files, err := s.retainWorkerInputs(t.Context(), input, []api.InputFile{file})
	if err != nil || projected.ModelInputOverride != "" || files[0] != file {
		t.Fatal("unconfigured embedder changed", projected, files, err)
	}
	ConfigureWorkerInputs(s, filepath.Join(t.TempDir(), "uploads"))
	input.ScreenInput = true
	projected, files, err = s.retainWorkerInputs(t.Context(), input, []api.InputFile{file})
	if err != nil || projected.ModelInputOverride != "" || files[0] != file {
		t.Fatal("screen capture acquired ordinary-upload metadata", projected, files, err)
	}
}
