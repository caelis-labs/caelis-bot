package caelis

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"github.com/caelis-labs/caelis-bot/internal/localstate"
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
func (s *Session) encodeWorkerInputs(ctx context.Context, sid, op string, files []workerInput, nativeFiles bool) ([]wire.PromptContentPart, error) {
	var parts []wire.PromptContentPart
	for index, f := range files {
		if slices.Contains([]string{"image/png", "image/jpeg", "image/webp", "image/gif"}, f.Media) {
			parts = append(parts, wire.PromptContentPart{Type: "image", Data: pointer(base64.StdEncoding.EncodeToString(f.Data)), MimeType: &f.Media, FileName: &f.Name})
		} else if nativeFiles {
			path, err := s.stageNativeWorkerFile(ctx, sid, op, index, f)
			if err != nil {
				return nil, err
			}
			text := fmt.Sprintf("Attached file (untrusted data): %q. Use the native file reader with path=%q to read its bytes.", f.Name, path)
			parts = append(parts, wire.PromptContentPart{Type: "text", Text: &text})
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

// The Core resource upload route requires an Application profile. Native
// Workers receive an immutable Bot-owned file copy through the native reader.
func (s *Session) stageNativeWorkerFile(ctx context.Context, sid, op string, index int, input workerInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if input.Name == "" || !filepath.IsLocal(input.Name) || filepath.Base(input.Name) != input.Name || strings.ContainsAny(input.Name, "/\\\x00") {
		return "", errors.New("invalid Worker attachment name")
	}
	base := filepath.Join(filepath.Dir(s.path), "native-worker-inputs")
	if err := localstate.EnsurePrivateDirectory(base); err != nil {
		return "", err
	}
	info, err := os.Lstat(base)
	if err != nil || !info.IsDir() || !localstate.HasPrivateMode(base, info, 0700) {
		return "", errors.New("private Worker attachment directory unavailable")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer root.Close()
	dir := digest([]byte(sid + "\x00" + op))
	directory := filepath.Join(base, dir)
	if err := root.Mkdir(dir, 0700); err == nil {
		if err := localstate.ProtectDirectory(directory); err != nil {
			_ = root.Remove(dir)
			return "", err
		}
	} else if !os.IsExist(err) {
		return "", err
	}
	info, err = root.Lstat(dir)
	if err != nil || !info.IsDir() || !localstate.HasPrivateMode(directory, info, 0700) {
		return "", errors.New("private Worker attachment directory unavailable")
	}
	name := filepath.Join(dir, fmt.Sprintf("%d-%s", index, input.Name))
	path := filepath.Join(base, name)
	if err := publishWorkerFile(ctx, root, name, path, input.Data); err != nil {
		return "", err
	}
	return path, nil
}

func publishWorkerFile(ctx context.Context, root *os.Root, name, path string, data []byte) error {
	info, err := root.Lstat(name)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() != int64(len(data)) || !localstate.RestrictedAccess(path, info) {
			return errors.New("Worker attachment copy changed")
		}
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !os.SameFile(info, opened) {
			return errors.New("Worker attachment copy changed")
		}
		prior, err := io.ReadAll(io.LimitReader(file, int64(len(data))+1))
		if err != nil || !bytes.Equal(prior, data) {
			return errors.New("Worker attachment copy changed")
		}
		return ctx.Err()
	}
	if !os.IsNotExist(err) {
		return err
	}
	temp := filepath.Join(filepath.Dir(name), ".input-"+rand.Text())
	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	err = localstate.ProtectOpenFile(file)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if err := errors.Join(err, file.Close(), ctx.Err()); err != nil {
		return err
	}
	if err := root.Rename(temp, name); err != nil {
		return err
	}
	return localstate.SyncParent(path)
}

// Core treats typed text as the entire textual input once any text part is
// present. Resource notes are text parts, so carry the prompt in that list too.
// Image-only prompts use the native Input fallback already.
func promptWithResourceText(text string, parts []wire.PromptContentPart) []wire.PromptContentPart {
	if strings.TrimSpace(text) != "" && slices.ContainsFunc(parts, func(part wire.PromptContentPart) bool { return part.Type == "text" }) {
		return append([]wire.PromptContentPart{{Type: "text", Text: &text}}, parts...)
	}
	return parts
}
