package workerfiles

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func selectedInput(t *testing.T, name string, data []byte) api.InputFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "selected")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return api.InputFile{Name: name, Path: path}
}
func openInputFixture(t *testing.T) *Inputs {
	t.Helper()
	s, err := OpenInputs(filepath.Join(t.TempDir(), "inputs"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInputsRetainOriginalBytesNameAndSubmissionIdentityAcrossReopen(t *testing.T) {
	s := openInputFixture(t)
	data := []byte("\xef\xbb\xbfname,value\r\n名,7\r\n")
	file := selectedInput(t, "data.csv", data)
	prepared, refs, err := s.Retain(t.Context(), "user-original", "Use the exact selected CSV", []api.InputFile{file})
	if err != nil || len(refs) != 1 || len(prepared) != 1 || prepared[0].Path == file.Path || prepared[0].Name != file.Name {
		t.Fatal("upload was not snapshotted", prepared, refs, err)
	}
	_, repeated, err := s.Retain(t.Context(), "user-original", "Use the exact selected CSV", []api.InputFile{file})
	if err != nil || repeated[0] != refs[0] {
		t.Fatal("original upload identity changed", repeated, err)
	}
	if err := os.Remove(file.Path); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenInputs(s.root)
	if err != nil {
		t.Fatal(err)
	}
	v, err := reopened.Read(t.Context(), refs[0].ID)
	if err != nil || v.Name != "data.csv" || !bytes.Equal(v.Data, data) || refs[0].SHA256 != inputHash(data) {
		t.Fatal("original upload did not survive source removal or App reopen", v, err)
	}
	stored, err := os.ReadFile(prepared[0].Path)
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatal("native encoder's copy changed", err)
	}
}

func TestInputsSameNameDifferentBytesAndOriginalIDConflicts(t *testing.T) {
	s := openInputFixture(t)
	one, two := selectedInput(t, "same.pdf", []byte("%PDF-first")), selectedInput(t, "same.pdf", []byte("%PDF-second"))
	prepared, refs, err := s.Retain(t.Context(), "original", "read both", []api.InputFile{one, two})
	if err != nil || refs[0].ID == refs[1].ID || prepared[0].Path == prepared[1].Path {
		t.Fatal("same names aliased bytes", refs, err)
	}
	renamed := one
	renamed.Name = "renamed.pdf"
	for _, changed := range []struct {
		text  string
		files []api.InputFile
	}{{"changed prompt", []api.InputFile{one, two}}, {"read both", []api.InputFile{two, one}}, {"read both", []api.InputFile{one}}, {"read both", []api.InputFile{renamed, two}}, {"read both", nil}} {
		if _, _, err := s.Retain(t.Context(), "original", changed.text, changed.files); !errors.Is(err, ErrInputConflict) {
			t.Fatal("original submission accepted changed input", err)
		}
	}
	if err := os.WriteFile(one.Path, []byte("changed source bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Retain(t.Context(), "original", "read both", []api.InputFile{one, two}); !errors.Is(err, ErrInputConflict) {
		t.Fatal("source mutation inherited original identity", err)
	}
	v, err := s.Read(t.Context(), refs[0].ID)
	if err != nil || string(v.Data) != "%PDF-first" {
		t.Fatal("conflict changed retained bytes", v, err)
	}
}

func TestInputsKeepExactImageBytesAndOriginalName(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	s := openInputFixture(t)
	file := selectedInput(t, "picture.png", data)
	_, refs, err := s.Retain(t.Context(), "original-picture", "inspect this picture", []api.InputFile{file})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Read(t.Context(), refs[0].ID)
	if err != nil || v.Name != "picture.png" || v.MediaType != "image/png" || !bytes.Equal(v.Data, data) {
		t.Fatal("original image was resized, renamed or transcoded", v.Name, v.MediaType, err)
	}
}

func TestInputsEightAndTwentyMiBBoundsAndEmptyBytes(t *testing.T) {
	s := openInputFixture(t)
	empty := selectedInput(t, "empty.csv", nil)
	files := make([]api.InputFile, 8)
	for i := range files {
		files[i] = empty
	}
	_, refs, err := s.Retain(t.Context(), "eight", "eight empty selections", files)
	if err != nil || len(refs) != 8 {
		t.Fatal("eight-file boundary rejected", err)
	}
	if _, _, err := s.Retain(t.Context(), "nine", "too many", append(files, empty)); err == nil {
		t.Fatal("ninth upload accepted")
	}
	max := selectedInput(t, "limit.pdf", bytes.Repeat([]byte{'x'}, api.MaxWorkFileBytes))
	if _, _, err := s.Retain(t.Context(), "max", "exact byte boundary", []api.InputFile{max}); err != nil {
		t.Fatal("20 MiB boundary rejected", err)
	}
	f, err := os.OpenFile(max.Path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.Write([]byte{'x'})
	if err := errors.Join(writeErr, f.Close()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Retain(t.Context(), "too-large", "over boundary", []api.InputFile{max}); err == nil {
		t.Fatal("oversized upload accepted")
	}
}

func TestInputsRejectForgedReferencesCorruptManifestAndChangedCacheBytes(t *testing.T) {
	s := openInputFixture(t)
	_, refs, err := s.Retain(t.Context(), "original", "private input text is not persisted", []api.InputFile{selectedInput(t, "data.csv", []byte("original bytes"))})
	if err != nil {
		t.Fatal(err)
	}
	key, _, _, _ := parseInputReference(refs[0].ID)
	for _, id := range []string{filepath.Join(s.root, "bytes", refs[0].SHA256), "data.csv", inputReferencePrefix + "../escape", inputReference(key, 1, refs[0].SHA256), inputReference(strings.Repeat("0", 64), 0, refs[0].SHA256), inputReference(key, 0, strings.Repeat("0", 64))} {
		if _, err := s.Read(t.Context(), id); err == nil {
			t.Fatal("forged upload reference accepted", id)
		}
	}
	manifestPath := filepath.Join(s.root, "manifests", key+".json")
	body, err := os.ReadFile(manifestPath)
	if err != nil || bytes.Contains(body, []byte("private input text")) {
		t.Fatal("manifest leaked model input", err)
	}
	var record inputManifest
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	record.Files[0].Name = "damaged.csv"
	damaged, _ := json.Marshal(record)
	if err := os.WriteFile(manifestPath, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(t.Context(), refs[0].ID); err == nil {
		t.Fatal("damaged manifest accepted")
	}
	if err := os.WriteFile(manifestPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "bytes", refs[0].SHA256), []byte("changed cache bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(t.Context(), refs[0].ID); err == nil {
		t.Fatal("changed cache bytes accepted")
	}
}

func TestInputsReopenedStoresSerializeOriginalSubmissionPublication(t *testing.T) {
	one := openInputFixture(t)
	two, err := OpenInputs(one.root)
	if err != nil {
		t.Fatal(err)
	}
	file := selectedInput(t, "source.txt", []byte("one source"))
	gate := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for index, store := range []*Inputs{one, two} {
		group.Go(func() {
			<-gate
			_, _, err := store.Retain(context.Background(), "concurrent-original", []string{"first intent", "second intent"}[index], []api.InputFile{file})
			results <- err
		})
	}
	close(gate)
	group.Wait()
	close(results)
	accepted, conflicted := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrInputConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflicted != 1 {
		t.Fatal("original upload was rebound concurrently", accepted, conflicted)
	}
}
