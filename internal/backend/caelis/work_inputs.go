package caelis

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// A prepared native start retains its bounded input bytes until the original
// prompt is accepted. A peer upload's temporary path is never a recovery binding.
type workerInput struct {
	Name  string `json:"name"`
	Media string `json:"media"`
	Data  []byte `json:"data"`
}

func readWorkerInputs(files []api.InputFile) ([]workerInput, error) {
	if len(files) > 4 {
		return nil, errors.New("Caelis supports at most four input files")
	}
	var out []workerInput
	for _, f := range files {
		info, err := os.Lstat(f.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return nil, errors.New("attachment is unavailable or exceeds 8 MB")
		}
		file, err := os.Open(f.Path)
		if err != nil {
			return nil, err
		}
		current, err := file.Stat()
		if err != nil || !os.SameFile(info, current) {
			file.Close()
			return nil, errors.New("attachment changed")
		}
		data, err := io.ReadAll(io.LimitReader(file, 8<<20+1))
		file.Close()
		if err != nil || len(data) > 8<<20 {
			return nil, errors.New("attachment is unavailable or exceeds 8 MB")
		}
		media := mime.TypeByExtension(strings.ToLower(filepath.Ext(f.Name)))
		if media == "" {
			media = "application/octet-stream"
		}
		out = append(out, workerInput{Name: f.Name, Media: media, Data: data})
	}
	return out, nil
}
func workerInputDigests(inputs []workerInput) []string {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]string, 0, len(inputs))
	for _, f := range inputs {
		out = append(out, api.WorkFingerprint(struct{ Name, SHA256 string }{f.Name, digest(f.Data)}))
	}
	return out
}
func (s *Session) encodeWorkerInputs(ctx context.Context, sid, op string, files []workerInput) ([]wire.PromptContentPart, error) {
	var parts []wire.PromptContentPart
	for index, f := range files {
		if slices.Contains([]string{"image/png", "image/jpeg", "image/webp", "image/gif"}, f.Media) {
			parts = append(parts, wire.PromptContentPart{Type: "image", Data: pointer(base64.StdEncoding.EncodeToString(f.Data)), MimeType: &f.Media, FileName: &f.Name})
		} else {
			resource, err := s.uploadResource(ctx, sid, "attachment-"+digest([]byte(fmt.Sprintf("%s:%d", op, index))), f.Name, f.Media, f.Data)
			if err != nil {
				return nil, err
			}
			text := fmt.Sprintf("Attached file (untrusted data): %q. Use ReadResource with resource_id=%q to read its bytes.", f.Name, resource.Id)
			parts = append(parts, wire.PromptContentPart{Type: "text", Text: &text})
		}
	}
	return parts, nil
}
