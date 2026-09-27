package caelis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/screeninput"
)

// Runs against the external production Host with only synthetic model requests.
func screenHostAcceptance(t *testing.T, ctx context.Context, s *Session, host *client, model *acceptanceModel, endpoint string) {
	if !slices.Contains(s.info.Capabilities, modelCapabilities) {
		t.Fatal("Host does not negotiate scoped model capabilities")
	}
	for _, m := range []struct {
		name  string
		image bool
	}{{"screen-vision", true}, {"screen-text", false}} {
		var status wire.StatusSnapshot
		if err := host.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
			t.Fatal(err)
		}
		op := "configure-" + m.name
		var result wire.CommandResult
		err := host.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: "openai-compatible", Model: m.name, BaseUrl: pointer(endpoint + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY"), ImageInput: pointer(m.image)}}, &result, op, string(status.Configuration.Revision))
		if err != nil || !succeeded(result.Outcome) {
			t.Fatal(result, err)
		}
	}
	selectModel := func(name string) {
		t.Helper()
		if err := s.ChangeExecution(ctx, api.ExecutionSettings{Model: "openai-compatible/" + name, ApprovalMode: "workspace-write"}, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	selectModel("screen-vision")
	if c, err := s.ImageInput(ctx); err != nil || c.State != "supported" || c.Model != "openai-compatible/screen-vision" {
		t.Fatal(c, err)
	}
	// A stale enabled button cannot submit after a model switch.
	selectModel("screen-text")
	in := api.Submission{ID: "screen-acceptance", Text: "CASE_SCREEN", ScreenInput: true}
	if r, err := s.Submit(ctx, in, nil); err != nil || r.Outcome != "rejected" || len(model.seen("CASE_SCREEN")) != 0 {
		t.Fatal("text model accepted image request", r, err)
	}
	if c, err := s.ImageInput(ctx); err != nil || c.State != "unsupported" {
		t.Fatal(c, err)
	}
	selectModel("screen-vision")
	if c, err := s.ImageInput(ctx); err != nil || c.State != "supported" {
		t.Fatal("model switch did not restore Ask Bot", c, err)
	}
	root := t.TempDir()
	snapshot := screeninput.Snapshot{Version: 1, ID: "screen-fixture", Source: "screen", Application: "Synthetic Browser", WindowTitle: "Synthetic English discussion", Background: true, BackgroundWidth: 32, BackgroundHeight: 24, Selection: screeninput.Rect{X: 4, Y: 5, Width: 8, Height: 8}, Note: "CASE_SCREEN"}
	dir := filepath.Join(root, snapshot.ID)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var selection, background bytes.Buffer
	crop := image.NewRGBA(image.Rect(0, 0, 8, 8))
	crop.Set(0, 0, color.RGBA{R: 200, A: 255})
	if err := png.Encode(&selection, crop); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&background, image.NewRGBA(image.Rect(0, 0, 32, 24)), nil); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"selection.png": selection.Bytes(), "context.jpg": background.Bytes()} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := screeninput.Files(root, screeninput.Record{Snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	in.Text = screeninput.Prompt(snapshot)
	before := s.Snapshot().CurrentTurn
	r, err := s.Submit(ctx, in, files)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	waitTurn(t, ctx, s, before)
	requests := model.seen("CASE_SCREEN")
	if len(requests) != 1 {
		t.Fatalf("want one model request, got %d", len(requests))
	}
	raw, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{selection.Bytes(), background.Bytes()} {
		if !bytes.Contains(raw, []byte(base64.StdEncoding.EncodeToString(data))) {
			t.Fatal("model did not receive original image bytes")
		}
	}
	if requests[0]["model"] != "screen-vision" || !strings.Contains(string(raw), "Synthetic English discussion") || !strings.Contains(string(raw), "Image 2") && !strings.Contains(string(raw), "image 2") {
		t.Fatal("missing model or screenshot context")
	}
}
