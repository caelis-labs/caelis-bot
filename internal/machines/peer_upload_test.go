package machines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Only the resident/native model is a fixture. Backend upload retention,
// Manager routing and the authenticated peer HTTP file transfer are real.
type peerUploadResident struct {
	api.Engine
	inputs []api.Submission
}

func (*peerUploadResident) Snapshot() api.Snapshot {
	return api.Snapshot{Connection: "ready", Items: []api.Item{}}
}

func (e *peerUploadResident) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.inputs = append(e.inputs, in)
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}

func peerUploadPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 31, G: 97, B: 145, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 171, G: 63, B: 22, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func peerRetainUploads(t *testing.T, back *backend.Service, resident *peerUploadResident, request string, originals []dualPeerInputFile) []api.TaskFileReference {
	t.Helper()
	dir := t.TempDir()
	files := make([]api.InputFile, len(originals))
	for i, original := range originals {
		path := filepath.Join(dir, original.Name)
		if err := os.WriteFile(path, original.Data, 0600); err != nil {
			t.Fatal(err)
		}
		files[i] = api.InputFile{Name: original.Name, Path: path}
	}
	input := api.Submission{ID: request, Text: "Use my selected material", ModelInputOverride: "Full original resident model input\n<quoted>fixture context</quoted>"}
	receipt, err := backend.SubmitRemote(t.Context(), back, input, files)
	if err != nil || receipt.Outcome != "accepted" || len(resident.inputs) == 0 {
		t.Fatal("ordinary remote ingress rejected fixture uploads", receipt, err)
	}
	projected := resident.inputs[len(resident.inputs)-1]
	_, tail, opened := strings.Cut(projected.ModelInputOverride, "<bot_uploaded_files>\n")
	metadata, _, closed := strings.Cut(tail, "\n</bot_uploaded_files>")
	var refs []struct {
		Name       string                `json:"name"`
		Attachment api.TaskFileReference `json:"attachment"`
	}
	if !opened || !closed || json.Unmarshal([]byte(metadata), &refs) != nil || len(refs) != len(originals) || projected.Text != input.Text || !strings.HasPrefix(projected.ModelInputOverride, input.ModelInputOverride+"\n\n") {
		t.Fatal("ordinary ingress did not expose complete delegatable upload references")
	}
	out := make([]api.TaskFileReference, len(refs))
	for i, ref := range refs {
		if ref.Name != originals[i].Name || ref.Attachment.TaskID != "" || !strings.HasPrefix(ref.Attachment.ID, "upload-file:") || strings.Contains(projected.ModelInputOverride, files[i].Path) {
			t.Fatal("selected file metadata lost original order/name/scope or exposed a path", i)
		}
		out[i] = ref.Attachment
		if err := os.Remove(files[i].Path); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestBotPeerOrdinaryUploadsRetainBytesForCreateAndContinue(t *testing.T) {
	target := newDualPeerTarget(t)
	service, manager, local, machine, _ := dualPeerConnect(t, target)
	resident := &peerUploadResident{}
	back := backend.NewService(resident, nil, nil, nil, nil)
	root := t.TempDir()
	backend.ConfigureWorkerInputs(back, filepath.Join(root, "uploads"))
	if err := backend.ConfigureMessageMedia(back, filepath.Join(root, "message-media")); err != nil {
		t.Fatal(err)
	}
	manager.ConfigureInputResolver(func(ctx context.Context, id string) (api.WorkFileContent, error) {
		value, handled, err := backend.ReadWorkerInput(ctx, back, id)
		if !handled {
			return api.WorkFileContent{}, errors.New("fixture accepts only retained user uploads")
		}
		return value, err
	})
	originals := []dualPeerInputFile{
		{Name: "资料.csv", Data: []byte("name,value\r\n甲,7\r\n")},
		{Name: "reference.pdf", Data: []byte("%PDF-1.4\n%fixture\x00original bytes\n%%EOF\n")},
		{Name: "chart.png", Data: peerUploadPNG(t)},
	}
	refs := peerRetainUploads(t, back, resident, "ordinary-upload-original", originals)
	// B chooses its actual default during preparation; A's cached environment
	// runtime may be older. The native fixture also rejects non-B model settings.
	target.setDefault("caelis")
	start := api.TaskStart{Machine: machine, RequestID: "create-with-ordinary-uploads", Title: "Read selected materials", Prompt: "Inspect all original materials", Attachments: refs}
	target.dropStart.Store(true)
	first, err := manager.StartTask(t.Context(), start)
	if err == nil || first.ID == "" {
		t.Fatal("lost create response was not retained under its original task", first, err)
	}
	receipt, err := manager.ReconcileTaskRequest(t.Context(), first.ID, start.RequestID)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal("original create could not be reconciled after transferring uploads", receipt, err)
	}
	again, err := manager.StartTask(t.Context(), start)
	if err != nil || again.ID != first.ID {
		t.Fatal("original create request was replaced", again, err)
	}
	owner := target.owner(service.state.ProfileID, "caelis")
	if owner == nil || service.state.Runtimes[first.ID] != "caelis" || service.state.Routes[first.ID] != machine {
		t.Fatal("task did not bind B's original native owner/configuration")
	}
	owner.mu.Lock()
	starts, initial := owner.starts, append([]dualPeerInputBatch(nil), owner.inputBatches[first.ID]...)
	owner.mu.Unlock()
	if starts != 1 || len(initial) != 1 || initial[0].Kind != "start" || initial[0].RequestID != start.RequestID || !reflect.DeepEqual(initial[0].Files, originals) {
		t.Fatal("B did not receive exactly one ordered, original-byte upload batch", starts, len(initial))
	}
	// Append a different file with the same visible name, then reuse earlier
	// image/PDF references in a new order. All original selections are gone.
	additional := dualPeerInputFile{Name: originals[0].Name, Data: []byte("name,value\r\n乙,19\r\n")}
	added := peerRetainUploads(t, back, resident, "ordinary-upload-additional", []dualPeerInputFile{additional})
	if added[0].ID == refs[0].ID {
		t.Fatal("same visible filename replaced an earlier upload's identity")
	}
	target.setDefault("codex")
	continuation := api.TaskMessage{ID: first.ID, RequestID: "continue-with-additional-material", Prompt: "Apply the added table to the original image and PDF", Attachments: []api.TaskFileReference{added[0], refs[2], refs[1]}}
	next, err := manager.SendTask(t.Context(), continuation)
	if err != nil || next.ID != first.ID {
		t.Fatal("additional material did not continue the original task", next, err)
	}
	if repeated, err := manager.SendTask(t.Context(), continuation); err != nil || repeated.ID != first.ID {
		t.Fatal("original continue receipt was not reusable", repeated, err)
	}
	changed := continuation
	changed.Attachments = []api.TaskFileReference{refs[0], refs[2], refs[1]}
	if _, err := manager.SendTask(t.Context(), changed); err == nil {
		t.Fatal("same original continuation ID accepted different same-name bytes")
	}
	owner.mu.Lock()
	starts, sends := owner.starts, owner.sends
	batches := append([]dualPeerInputBatch(nil), owner.inputBatches[first.ID]...)
	owner.mu.Unlock()
	want := []dualPeerInputFile{additional, originals[2], originals[1]}
	if starts != 1 || sends != 1 || len(batches) != 2 || batches[1].Kind != "send" || batches[1].RequestID != continuation.RequestID || !reflect.DeepEqual(batches[1].Files, want) {
		t.Fatal("continuation reordered/replaced original bytes or replayed native work", starts, sends, len(batches))
	}
	if service.state.Runtimes[first.ID] != "caelis" || service.state.Routes[first.ID] != machine || target.owner(service.state.ProfileID, "caelis") != owner {
		t.Fatal("B default change rebound original task/model ownership")
	}
	otherOwner := target.owner(service.state.ProfileID, "codex")
	if otherOwner != nil {
		otherOwner.mu.Lock()
		otherStarts, otherSends := otherOwner.starts, otherOwner.sends
		otherOwner.mu.Unlock()
		if otherStarts != 0 || otherSends != 0 {
			t.Fatal("new B default received an existing task or its input bytes")
		}
	}
	if local.mutations.Load() != 0 {
		t.Fatal("A executed a remote task locally")
	}
}
