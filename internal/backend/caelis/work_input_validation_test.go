package caelis

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
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestValidateWorkFilesReadsPreparedInputsWithoutNativeWork(t *testing.T) {
	dir := t.TempDir()
	s := New(Options{Directory: filepath.Join(dir, "private")})
	owner := &WorkOwner{engine: s}
	before, err := json.Marshal(s.state)
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
	original, err := readWorkerInputs(files)
	if err != nil || !strings.HasPrefix(original[0].Media, "text/csv") || original[1].Media != "image/png" {
		t.Fatal("CSV/PNG fixture", err)
	}
	for _, validator := range []api.WorkInputValidator{s, owner} {
		if err := validator.ValidateWorkFiles(t.Context(), "host-route-not-a-native-task", files); err != nil {
			t.Fatal("valid CSV and PNG", err)
		}
		if err := validator.ValidateWorkFiles(t.Context(), "", nil); err != nil {
			t.Fatal("empty inputs", err)
		}
	}
	after, _ := json.Marshal(s.state)
	if string(after) != string(before) || s.connected || s.cancel != nil || len(s.state.Workers) != 0 || len(s.state.Operations) != 0 {
		t.Fatal("read-only validation created native state", string(after))
	}
	if _, err := os.Stat(s.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validation persisted a binding", err)
	}
	afterBytes, err := readWorkerInputs(files)
	if err != nil || !reflect.DeepEqual(original, afterBytes) {
		t.Fatal("validation changed input names or bytes", err)
	}
}

func TestValidateWorkFilesEnforcesCaelisBoundariesBeforeDispatch(t *testing.T) {
	s := &Session{}
	dir := t.TempDir()
	file := filepath.Join(dir, "input.csv")
	if err := os.WriteFile(file, []byte("value\n1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files := make([]api.InputFile, 4)
	for i := range files {
		files[i] = api.InputFile{Name: "input.csv", Path: file}
	}
	if err := s.ValidateWorkFiles(t.Context(), "", files); err != nil {
		t.Fatal("four files must be supported", err)
	}
	if err := s.ValidateWorkFiles(t.Context(), "", append(files, api.InputFile{})); err == nil || err.Error() != "Caelis supports at most four input files" {
		t.Fatal("fifth input must be rejected before reading it", err)
	}
	large := filepath.Join(dir, "boundary.bin")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(8 << 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateWorkFiles(t.Context(), "", []api.InputFile{{Name: "boundary.bin", Path: large}}); err != nil {
		t.Fatal("exactly 8 MiB must remain supported", err)
	}
	if err := os.Truncate(large, 8<<20+1); err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateWorkFiles(t.Context(), "", []api.InputFile{{Name: "boundary.bin", Path: large}}); err == nil {
		t.Fatal("8 MiB plus one byte accepted")
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
