package workerfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func expireFile(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func fixtureTrash(t *testing.T, paths *[]string) func(string) error {
	t.Helper()
	bin := t.TempDir()
	return func(path string) error {
		*paths = append(*paths, path)
		return os.Rename(path, filepath.Join(bin, strconv.Itoa(len(*paths))))
	}
}

func TestInputsStorageKeepsCurrentAndUnknownReferencesAndReclaimsOnlyOldOrphans(t *testing.T) {
	s, err := OpenInputs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(t.TempDir(), "selected")
	if err := os.WriteFile(selected, []byte("current input bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	_, refs, err := s.Retain(t.Context(), "original-submission", "original input", []api.InputFile{{Name: "table.csv", Path: selected}})
	if err != nil {
		t.Fatal(err)
	}
	key, _, _, _ := parseInputReference(refs[0].ID)
	expireFile(t, filepath.Join(s.root, "manifests", key+".json"))
	expireFile(t, filepath.Join(s.root, "bytes", refs[0].SHA256))
	orphan := filepath.Join(s.root, "bytes", inputHash([]byte("old unpublished bytes")))
	if err := os.WriteFile(orphan, []byte("old unpublished bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	expireFile(t, orphan)
	keep := Protection{Complete: false, References: []api.TaskFileReference{{ID: inputReference(inputHash([]byte("uncertain submission")), 0, filepath.Base(orphan))}}}
	info, err := s.Storage(t.Context(), keep, false, nil)
	if err != nil || info.Files != 3 || info.EligibleFiles != 0 {
		t.Fatal("unknown input or old current manifest became eligible", info, err)
	}
	keep.References = nil
	keep.Complete = true
	keep.Submissions = []string{"original-submission"}
	info, err = s.Storage(t.Context(), keep, false, nil)
	if err != nil || info.Files != 3 || info.EligibleFiles != 1 || !info.CanClean {
		t.Fatal("unpublished old bytes were omitted from storage", info, err)
	}
	var moved []string
	info, err = s.Storage(t.Context(), keep, true, fixtureTrash(t, &moved))
	if err != nil || len(moved) != 1 || moved[0] != orphan || info.Files != 2 || info.EligibleFiles != 0 {
		t.Fatal("cleanup touched a current index or failed to reclaim orphan", info, moved, err)
	}
	reopened, err := OpenInputs(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := reopened.Read(t.Context(), refs[0].ID); err != nil || string(value.Data) != "current input bytes" {
		t.Fatal("restarting lost protected original input", err)
	}
}

func TestInputIntentRefusalPublishesNoNewCopies(t *testing.T) {
	s, err := OpenInputs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(t.TempDir(), "selected")
	_ = os.WriteFile(selected, []byte("original bytes"), 0600)
	files := []api.InputFile{{Name: "table.csv", Path: selected}}
	if _, _, err := s.Retain(t.Context(), "original-id", "original input", files); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(selected, []byte("rejected different bytes"), 0600)
	if _, _, err := s.Retain(t.Context(), "original-id", "original input", files); !errors.Is(err, ErrInputConflict) {
		t.Fatal("changed original intent was not refused", err)
	}
	if entries, err := os.ReadDir(filepath.Join(s.root, "bytes")); err != nil || len(entries) != 1 {
		t.Fatal("known refused intent left another retained copy", len(entries), err)
	}
	files = append(files, api.InputFile{Name: "missing.csv", Path: filepath.Join(t.TempDir(), "missing")})
	if _, _, err := s.Retain(t.Context(), "rejected-new-id", "new input", files); err == nil {
		t.Fatal("missing original file was accepted")
	}
	if entries, err := os.ReadDir(filepath.Join(s.root, "bytes")); err != nil || len(entries) != 1 {
		t.Fatal("partially validated new input published bytes", len(entries), err)
	}
}

func TestOutputsStorageKeepsCurrentOfflineAndRetiresSupersededCopies(t *testing.T) {
	s := OpenOutputs(t.TempDir())
	data := []byte("first version")
	read := func(context.Context) (api.WorkFileContent, error) {
		return api.WorkFileContent{Name: "report.csv", Data: data}, nil
	}
	first, err := s.Materialize(t.Context(), "task", "ref", false, read)
	if err != nil {
		t.Fatal(err)
	}
	expireFile(t, first.Path)
	data = []byte("latest version")
	latest, err := s.Materialize(t.Context(), "task", "ref", true, read)
	if err != nil || latest.Path == first.Path {
		t.Fatal("fixture did not replace the native ref's cached version", err)
	}
	expireFile(t, latest.Path)
	info, err := s.Storage(t.Context(), Protection{Complete: true}, false, nil)
	if err != nil || info.Files != 3 || info.EligibleFiles != 1 || info.EligibleBytes != int64(len("first version")) {
		t.Fatal("current offline output became eligible or superseded copy was hidden", info, err)
	}
	var moved []string
	if _, err := s.Storage(t.Context(), Protection{Complete: true}, true, fixtureTrash(t, &moved)); err != nil || len(moved) != 1 || moved[0] != first.Path {
		t.Fatal("cleanup removed current output or missed superseded bytes", moved, err)
	}
	reopened := OpenOutputs(s.root)
	copy, err := reopened.Materialize(t.Context(), "task", "ref", false, func(context.Context) (api.WorkFileContent, error) {
		return api.WorkFileContent{}, errors.New("original owner offline")
	})
	if err != nil || !copy.Cached || copy.Path != latest.Path {
		t.Fatal("restarting lost protected offline result", copy, err)
	}
}

func TestWorkerStorageDamageSymlinksAndFailedTrashNeverLoseReferencedBytes(t *testing.T) {
	t.Run("damaged index", func(t *testing.T) {
		s := OpenOutputs(t.TempDir())
		copy, err := s.Materialize(t.Context(), "task", "ref", false, func(context.Context) (api.WorkFileContent, error) {
			return api.WorkFileContent{Name: "report.csv", Data: []byte("protected")}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		index := filepath.Join(s.root, "refs", api.WorkFingerprint(struct{ Task, Ref string }{"task", "ref"})+".json")
		_ = os.WriteFile(index, []byte("corrupt index"), 0600)
		expireFile(t, copy.Path)
		var moved []string
		info, err := s.Storage(t.Context(), Protection{Complete: true}, true, fixtureTrash(t, &moved))
		if err == nil || info.EligibleFiles != 0 || len(moved) != 0 {
			t.Fatal("unverified references authorized cleanup", info, moved, err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root, outside := t.TempDir(), t.TempDir()
		path := filepath.Join(outside, "private")
		_ = os.WriteFile(path, []byte("outside bytes"), 0600)
		if err := os.Symlink(outside, filepath.Join(root, "redirect")); err != nil {
			t.Skip("fixture platform cannot create symbolic link")
		}
		var moved []string
		if _, err := PlainStorage(t.Context(), root, nil, true, fixtureTrash(t, &moved)); err == nil || len(moved) != 0 {
			t.Fatal("cleanup followed a redirected directory", moved, err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != "outside bytes" {
			t.Fatal("external file was touched", err)
		}
	})
	t.Run("trash failure", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "own-stage.csv")
		_ = os.WriteFile(path, []byte("staged"), 0600)
		expireFile(t, path)
		for _, fail := range []func(string) error{func(string) error { return errors.New("fixture refused trash") }, func(string) error { return nil }} {
			if _, err := PlainStorage(t.Context(), root, nil, true, fail); err == nil {
				t.Fatal("unconfirmed trash was reported successful")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("failed trash lost original file", err)
			}
		}
	})
}

func TestPlainStorageHonorsProtectedOperationPathsAndThirtyDays(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"kept/op/input.csv", "rejected/op/input.csv", "recent/input.csv"} {
		path := filepath.Join(root, name)
		_ = os.MkdirAll(filepath.Dir(path), 0700)
		_ = os.WriteFile(path, []byte(name), 0600)
		if name != "recent/input.csv" {
			expireFile(t, path)
		}
	}
	var moved []string
	info, err := PlainStorage(t.Context(), root, []string{filepath.Join(root, "kept")}, true, fixtureTrash(t, &moved))
	if err != nil || len(moved) != 1 || moved[0] != filepath.Join(root, "rejected/op/input.csv") || info.Files != 2 {
		t.Fatal("staging cleanup lost protected/current bytes", info, moved, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := PlainStorage(ctx, root, nil, true, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled cleanup was admitted", err)
	}
}

type cancelOnPublishedInput struct {
	context.Context
	cancel context.CancelFunc
	path   string
}

func (c cancelOnPublishedInput) Err() error {
	if _, err := os.Stat(c.path); err == nil {
		c.cancel()
	}
	return c.Context.Err()
}
func TestInputPreparationCancellationRollsBackOnlyUnpublishedBytes(t *testing.T) {
	s, err := OpenInputs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	_ = os.WriteFile(a, []byte("new-first"), 0600)
	_ = os.WriteFile(b, []byte("new-second"), 0600)
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := cancelOnPublishedInput{parent, cancel, filepath.Join(s.root, "bytes", inputHash([]byte("new-first")))}
	if _, _, err = s.Retain(ctx, "cancelled-before-publication", "input", []api.InputFile{{Name: "a.csv", Path: a}, {Name: "b.csv", Path: b}}); !errors.Is(err, context.Canceled) {
		t.Fatal("fixture did not cancel after first byte publication", err)
	}
	for _, name := range []string{"bytes", "manifests"} {
		entries, err := os.ReadDir(filepath.Join(s.root, name))
		if err != nil || len(entries) != 0 {
			t.Fatal("cancelled preparation leaked unbound cache files", name, len(entries), err)
		}
	}
}
func TestOldUnreferencedInputBytesRetireButOriginalIntentCannotChange(t *testing.T) {
	s, err := OpenInputs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "selected")
	_ = os.WriteFile(source, []byte("original"), 0600)
	files := []api.InputFile{{Name: "a.csv", Path: source}}
	_, refs, err := s.Retain(t.Context(), "rejected-unused-selection", "input", files)
	if err != nil {
		t.Fatal(err)
	}
	key, _, _, _ := parseInputReference(refs[0].ID)
	expireFile(t, filepath.Join(s.root, "manifests", key+".json"))
	expireFile(t, filepath.Join(s.root, "bytes", refs[0].SHA256))
	for _, keep := range []Protection{{Complete: false}, {Complete: true, Submissions: []string{"rejected-unused-selection"}}, {Complete: true, References: []api.TaskFileReference{{ID: refs[0].ID}}}} {
		info, err := s.Storage(t.Context(), keep, false, nil)
		if err != nil || info.EligibleFiles != 0 {
			t.Fatal("active/unknown/reference evidence did not protect input", info, err)
		}
	}
	var moved []string
	if _, err = s.Storage(t.Context(), Protection{Complete: true}, true, fixtureTrash(t, &moved)); err != nil || len(moved) != 1 {
		t.Fatal("unused indexed bytes never retire", moved, err)
	}
	_ = os.WriteFile(source, []byte("different"), 0600)
	if _, _, err = s.Retain(t.Context(), "rejected-unused-selection", "input", files); !errors.Is(err, ErrInputConflict) {
		t.Fatal("cleanup erased immutable original intent", err)
	}
	_ = os.WriteFile(source, []byte("original"), 0600)
	if _, again, err := s.Retain(t.Context(), "rejected-unused-selection", "input", files); err != nil || again[0].ID != refs[0].ID {
		t.Fatal("identical original material cannot be restored", err)
	}
}
func TestOutputCleanupProtectsUnknownAndExplicitInputPaths(t *testing.T) {
	s := OpenOutputs(t.TempDir())
	read := func(text string) func(context.Context) (api.WorkFileContent, error) {
		return func(context.Context) (api.WorkFileContent, error) {
			return api.WorkFileContent{Name: "a.csv", Data: []byte(text)}, nil
		}
	}
	first, err := s.Materialize(t.Context(), "task", "ref", false, read("old"))
	if err != nil {
		t.Fatal(err)
	}
	expireFile(t, first.Path)
	if _, err = s.Materialize(t.Context(), "task", "ref", true, read("new")); err != nil {
		t.Fatal(err)
	}
	for _, keep := range []Protection{{Complete: false}, {Complete: true, Paths: []string{first.Path}}} {
		info, err := s.Storage(t.Context(), keep, false, nil)
		if err != nil || info.EligibleFiles != 0 {
			t.Fatal("unknown original material became garbage", info, err)
		}
	}
}
func TestCacheReadAndMaintenanceSerializeAcrossReopenedStores(t *testing.T) {
	inputs, err := OpenInputs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(t.TempDir(), "input")
	_ = os.WriteFile(selected, []byte("retained input"), 0600)
	_, refs, err := inputs.Retain(t.Context(), "live", "text", []api.InputFile{{Name: "a.csv", Path: selected}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	out := OpenOutputs(root)
	_, err = out.Materialize(t.Context(), "task", "ref", false, func(context.Context) (api.WorkFileContent, error) {
		return api.WorkFileContent{Name: "a.csv", Data: []byte("retained result")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	fail := make(chan error, 3)
	for range 3 {
		wg.Go(func() {
			for range 15 {
				in, e := OpenInputs(inputs.root)
				if e != nil {
					fail <- e
					return
				}
				if _, e = in.Read(t.Context(), refs[0].ID); e != nil {
					fail <- e
					return
				}
				if _, e = in.Storage(t.Context(), Protection{Complete: true, Submissions: []string{"live"}}, true, func(string) error { return errors.New("current input must not be trashed") }); e != nil {
					fail <- e
					return
				}
				o := OpenOutputs(root)
				if _, e = o.Materialize(t.Context(), "task", "ref", false, func(context.Context) (api.WorkFileContent, error) {
					return api.WorkFileContent{}, errors.New("B offline")
				}); e != nil {
					fail <- e
					return
				}
				if _, e = o.Storage(t.Context(), Protection{Complete: true}, true, func(string) error { return errors.New("current output must not be trashed") }); e != nil {
					fail <- e
					return
				}
			}
		})
	}
	wg.Wait()
	close(fail)
	for e := range fail {
		t.Fatal(e)
	}
	out.maintenance.RLock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = OpenOutputs(root).Storage(ctx, Protection{Complete: true}, true, nil)
	out.maintenance.RUnlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancelled maintenance left an orphan lock waiter", err)
	}
}
func TestDamagedOrRedirectedCacheNeverAuthorizesCleanupOrPublication(t *testing.T) {
	t.Run("input namespace", func(t *testing.T) {
		s, err := OpenInputs(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err = os.Remove(filepath.Join(s.root, "bytes")); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(outside, filepath.Join(s.root, "bytes")); err != nil {
			t.Skip("symlink unavailable")
		}
		source := filepath.Join(t.TempDir(), "file")
		_ = os.WriteFile(source, []byte("must stay inside"), 0600)
		if _, _, err = s.Retain(t.Context(), "id", "input", []api.InputFile{{Name: "a.csv", Path: source}}); err == nil {
			t.Fatal("input publication followed redirect")
		}
		entries, _ := os.ReadDir(outside)
		if len(entries) != 0 {
			t.Fatal("cache wrote outside its namespace")
		}
	})
	t.Run("output namespace", func(t *testing.T) {
		root, outside := t.TempDir(), t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "objects")); err != nil {
			t.Skip("symlink unavailable")
		}
		_, err := OpenOutputs(root).Materialize(t.Context(), "task", "ref", false, func(context.Context) (api.WorkFileContent, error) {
			return api.WorkFileContent{Name: "a.csv", Data: []byte("not outside")}, nil
		})
		if err == nil {
			t.Fatal("output publication followed redirect")
		}
		entries, _ := os.ReadDir(outside)
		if len(entries) != 0 {
			t.Fatal("output bytes escaped cache")
		}
	})
	t.Run("existing output redirect", func(t *testing.T) {
		s := OpenOutputs(t.TempDir())
		copy, err := s.Materialize(t.Context(), "task", "ref", false, func(context.Context) (api.WorkFileContent, error) {
			return api.WorkFileContent{Name: "report.csv", Data: []byte("original result")}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "moved")
		dir := filepath.Dir(copy.Path)
		if err = os.Rename(dir, outside); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(outside, dir); err != nil {
			t.Skip("symlink unavailable")
		}
		_, err = s.Materialize(t.Context(), "task", "ref", false, func(context.Context) (api.WorkFileContent, error) {
			return api.WorkFileContent{}, errors.New("B offline")
		})
		if err == nil {
			t.Fatal("cached read followed redirect outside its namespace")
		}
		if _, err = s.Storage(t.Context(), Protection{Complete: true}, true, func(string) error {
			t.Fatal("cleanup followed redirect")
			return nil
		}); err == nil {
			t.Fatal("redirected cache was considered safe to clean")
		}
	})
}
