package workerfiles

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestOutputCopyKeepsNativeNameSeparateFromPortableHostPath(t *testing.T) {
	s := OpenOutputs(t.TempDir())
	for _, name := range []string{"report:stream.csv", "CON.pdf", "image.png", "native:opaque", "trailing."} {
		v, err := s.Materialize(t.Context(), "owned", name, true, func(context.Context) (api.WorkFileContent, error) {
			return api.WorkFileContent{Name: name, Data: []byte("fixture")}, nil
		})
		if err != nil || v.Name != name || filepath.Base(v.Path) != outputObjectName(name) {
			t.Fatal("original name lost or used as host path", v, err)
		}
		b, err := os.ReadFile(v.Path)
		if err != nil || string(b) != "fixture" {
			t.Fatal("checked portable copy unreadable", err)
		}
	}
}

func TestOutputCopiesPreserveVersionsScopeAndOfflineBytes(t *testing.T) {
	root := t.TempDir()
	s := OpenOutputs(root)
	data := []byte("a,b\n1,2\n")
	offline := false
	reads := 0
	read := func(context.Context) (api.WorkFileContent, error) {
		reads++
		if offline {
			return api.WorkFileContent{}, errors.New("owner_offline")
		}
		return api.WorkFileContent{Name: "result.csv", Data: data}, nil
	}
	first, err := s.Materialize(t.Context(), "owned-task", "native-file", true, read)
	if err != nil || first.Cached || first.Bytes != int64(len(data)) || first.FetchedAt == 0 {
		t.Fatal(first, err)
	}
	data = []byte("a,b\n3,4\n")
	cached, err := s.Materialize(t.Context(), "owned-task", "native-file", false, read)
	if err != nil || !cached.Cached || cached.SHA256 != first.SHA256 || reads != 1 {
		t.Fatal("cached bytes posed as latest or re-read", cached, err)
	}
	next, err := s.Materialize(t.Context(), "owned-task", "native-file", true, read)
	if err != nil || next.Cached || next.SHA256 == first.SHA256 || next.Path == first.Path {
		t.Fatal("refresh overwrote old immutable version", next, err)
	}
	oldBytes, _ := os.ReadFile(first.Path)
	if !bytes.Equal(oldBytes, []byte("a,b\n1,2\n")) {
		t.Fatal("old version mutated")
	}
	offline = true
	s = OpenOutputs(root)
	retained, err := s.Materialize(t.Context(), "owned-task", "native-file", true, read)
	if err != nil || !retained.Cached || retained.SHA256 != next.SHA256 {
		t.Fatal("offline copy lost", retained, err)
	}
	if _, err = s.Materialize(t.Context(), "other-task", "native-file", false, read); err == nil {
		t.Fatal("same native ID crossed task scope")
	}
	if err = os.WriteFile(next.Path, []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Materialize(t.Context(), "owned-task", "native-file", true, read); err == nil {
		t.Fatal("unchecked cache corruption exposed")
	}
}
func TestOutputCopiesRejectUnboundedAndInvalidNames(t *testing.T) {
	s := OpenOutputs(t.TempDir())
	for _, name := range []string{"", ".", "..", "a/b", "a\\b", "a\x00b", "a\nb"} {
		if _, err := s.Materialize(t.Context(), "owned", name+"ref", true, func(context.Context) (api.WorkFileContent, error) {
			return api.WorkFileContent{Name: name, Data: []byte("bounded")}, nil
		}); err == nil {
			t.Fatal("invalid name", name)
		}
	}
	if _, err := s.Materialize(t.Context(), "owned", "large", true, func(context.Context) (api.WorkFileContent, error) {
		return api.WorkFileContent{Name: "large", Data: make([]byte, api.MaxWorkFileBytes+1)}, nil
	}); err == nil {
		t.Fatal("oversized bytes exposed")
	}
}
