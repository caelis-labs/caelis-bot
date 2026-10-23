package codex

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestValidateWorkFilesReadsPreparedInputsWithoutNativeWork(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(SessionOptions{Directory: filepath.Join(dir, "workspace"), StateFile: filepath.Join(dir, "binding.json")})
	t.Cleanup(s.cancelLife)
	owner := &WorkOwner{engine: s}
	before, err := json.Marshal(s.binding)
	if err != nil {
		t.Fatal(err)
	}
	csv := filepath.Join(dir, "table.csv")
	if err := os.WriteFile(csv, []byte("name,value\nexample,42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	photo := filepath.Join(dir, "image.png")
	writeValidationPNG(t, photo)
	files := []api.InputFile{{Name: "table.csv", Path: csv}, {Name: "image.png", Path: photo}}
	originalBytes, err := api.WorkInputDigests(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, validator := range []api.WorkInputValidator{s, owner} {
		if err := validator.ValidateWorkFiles(t.Context(), "host-route-not-a-native-task", files); err != nil {
			t.Fatal("valid CSV and PNG", err)
		}
		if err := validator.ValidateWorkFiles(t.Context(), "", nil); err != nil {
			t.Fatal("empty inputs", err)
		}
	}
	after, _ := json.Marshal(s.binding)
	if string(after) != string(before) || s.client != nil || s.retainedOwner != nil || len(s.binding.Tasks) != 0 {
		t.Fatal("read-only validation created native state", string(after))
	}
	if _, err := os.Stat(s.opts.StateFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validation persisted a binding", err)
	}
	if _, err := os.Stat(filepath.Join(s.opts.Directory, ".attachments")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validation staged native attachments", err)
	}
	afterBytes, err := api.WorkInputDigests(files)
	if err != nil || !reflect.DeepEqual(originalBytes, afterBytes) {
		t.Fatal("validation changed input names or bytes", err)
	}
}

func TestValidateWorkFilesEnforcesCodexBoundariesBeforeDispatch(t *testing.T) {
	s := &Session{}
	dir := t.TempDir()
	file := filepath.Join(dir, "input.csv")
	if err := os.WriteFile(file, []byte("value\n1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files := make([]api.InputFile, 8)
	for i := range files {
		files[i] = api.InputFile{Name: "input.csv", Path: file}
	}
	if err := s.ValidateWorkFiles(t.Context(), "", files); err != nil {
		t.Fatal("eight files must be supported", err)
	}
	if err := s.ValidateWorkFiles(t.Context(), "", append(files, api.InputFile{})); err == nil || err.Error() != "too_many_worker_files" {
		t.Fatal("ninth input must be rejected before reading it", err)
	}
	large := filepath.Join(dir, "boundary.bin")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(api.MaxWorkFileBytes); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateWorkFiles(t.Context(), "", []api.InputFile{{Name: "boundary.bin", Path: large}}); err != nil {
		t.Fatal("exactly 20 MiB must remain supported", err)
	}
	if err := os.Truncate(large, api.MaxWorkFileBytes+1); err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateWorkFiles(t.Context(), "", []api.InputFile{{Name: "boundary.bin", Path: large}}); err == nil {
		t.Fatal("20 MiB plus one byte accepted")
	}
	for _, path := range []string{dir, filepath.Join(dir, "missing")} {
		if err := s.ValidateWorkFiles(t.Context(), "", []api.InputFile{{Path: path}}); err == nil {
			t.Fatal("non-regular input accepted", path)
		}
	}
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(dir, "link.csv")
		if err := os.Symlink(file, link); err != nil {
			t.Skip("symlink fixture not available:", err)
		}
		if err := s.ValidateWorkFiles(t.Context(), "", []api.InputFile{{Name: "link.csv", Path: link}}); err == nil {
			t.Fatal("symlink input accepted")
		}
	})
}

func TestValidateWorkFilesHonorsCancellationBeforeAndAfterRead(t *testing.T) {
	s := &Session{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.ValidateWorkFiles(ctx, "", []api.InputFile{{Path: "must-not-be-read"}}); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-canceled validation must not read files", err)
	}
	file := filepath.Join(t.TempDir(), "input.csv")
	if err := os.WriteFile(file, []byte("a,b\n1,2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	checks := &inputValidationCancelContext{Context: ctx, cancel: cancel}
	if err := s.ValidateWorkFiles(checks, "", []api.InputFile{{Name: "input.csv", Path: file}}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation observed after the bounded read", err)
	}
}

func TestValidateWorkFilesRejectsReplacedPreparedBytes(t *testing.T) {
	s := &Session{}
	dir := t.TempDir()
	path, replacement := filepath.Join(dir, "input.csv"), filepath.Join(dir, "replacement.csv")
	for _, name := range []string{path, replacement} {
		if err := os.WriteFile(name, []byte("a,b\n1,2\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Replace the path after its bytes were read, keeping size and mtime equal.
	// The final snapshot check must prove file identity rather than just size.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	ctx := &inputValidationReadBoundaryContext{Context: t.Context(), afterRead: func() {
		if err := os.Rename(path, path+".previous"); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, path); err != nil {
			t.Fatal(err)
		}
	}}
	if err := s.ValidateWorkFiles(ctx, "", []api.InputFile{{Name: "input.csv", Path: path}}); err == nil || err.Error() != "worker_file_changed" {
		t.Fatal("prepared input path replacement was accepted", err)
	}
}

type inputValidationReadBoundaryContext struct {
	context.Context
	checks    int
	afterRead func()
}

func (c *inputValidationReadBoundaryContext) Err() error {
	c.checks++
	if c.checks == 3 {
		c.afterRead()
	}
	return c.Context.Err()
}

type inputValidationCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *inputValidationCancelContext) Err() error {
	c.checks++
	if c.checks == 3 {
		c.cancel()
	}
	return c.Context.Err()
}

func writeValidationPNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	m := image.NewRGBA(image.Rect(0, 0, 2, 2))
	m.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(f, m); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
