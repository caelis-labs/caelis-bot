package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

func stageFiles(t *testing.T, m *Manager) []string {
	t.Helper()
	var paths []string
	_ = filepath.WalkDir(filepath.Join(m.root, ".inputs"), func(path string, d os.DirEntry, e error) error {
		if e == nil && !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	return paths
}
func TestTaskInputPreparationRejectAndCancellationLeaveNoStagingCopies(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected", true: "cancelled"}[cancelled], func(t *testing.T) {
			f := newRuntime()
			m := openFixture(t, t.TempDir(), "codex", f)
			m.ConfigureInputResolver(func(_ context.Context, id string) (api.WorkFileContent, error) {
				if id == "second" {
					if cancelled {
						return api.WorkFileContent{}, context.Canceled
					}
					return api.WorkFileContent{}, errors.New("unavailable attachment")
				}
				return api.WorkFileContent{Name: "first.csv", Data: []byte("first prepared copy")}, nil
			})
			in := input("new-rejected-preparation")
			in.Attachments = []api.TaskFileReference{{ID: "first"}, {ID: "second"}}
			if _, err := m.StartTask(t.Context(), in); err == nil {
				t.Fatal("bad input accepted")
			}
			if len(stageFiles(t, m)) != 0 || f.starts != 0 || len(m.state.Records) != 0 {
				t.Fatal("failed preparation leaked input or fake work", stageFiles(t, m), f.starts)
			}
		})
	}
}
func TestTaskUnknownReceiptReferencesSurviveUnpinReopenAndCleanup(t *testing.T) {
	f := newRuntime()
	f.unknownStart = true
	root := t.TempDir()
	m := openFixture(t, root, "codex", f)
	m.ConfigureInputResolver(func(context.Context, string) (api.WorkFileContent, error) {
		return api.WorkFileContent{Name: "input.csv", Data: []byte("unknown original input")}, nil
	})
	in := input("unknown-with-input")
	in.Attachments = []api.TaskFileReference{{ID: "original-user-upload"}}
	task, err := m.StartTask(t.Context(), in)
	if err == nil || task.ID == "" {
		t.Fatal("fixture did not lose original receipt", err)
	}
	paths := stageFiles(t, m)
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	_ = os.Chtimes(paths[0], old, old)
	if _, err = m.PinTask(task.ID, false); err != nil {
		t.Fatal(err)
	}
	m = openFixture(t, root, "codex", f)
	calls := 0
	_, err = m.WithFileStorage(t.Context(), func(keep workerfiles.Protection, protected []string) (api.AttachmentStorage, error) {
		if !keep.Complete || len(keep.References) != 1 || len(protected) != 1 {
			t.Fatal("retained/unknown original cache identities missing", keep, protected)
		}
		return workerfiles.PlainStorage(t.Context(), filepath.Join(m.root, ".inputs"), protected, true, func(string) error { calls++; return errors.New("referenced original must not be trashed") })
	})
	if err != nil || calls != 0 {
		t.Fatal("unknown material became garbage", err, calls)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || string(data) != "unknown original input" {
		t.Fatal("original input lost", err)
	}
}
func TestTaskCleanupCannotRaceAnUncommittedInputPreparation(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "codex", f)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.ConfigureInputResolver(func(_ context.Context, id string) (api.WorkFileContent, error) {
		if id == "wait" {
			once.Do(func() { close(entered) })
			<-release
			return api.WorkFileContent{}, context.Canceled
		}
		return api.WorkFileContent{Name: "a.csv", Data: []byte("in-flight preparation")}, nil
	})
	done := make(chan error, 1)
	go func() {
		in := input("inflight-cache-original")
		in.Attachments = []api.TaskFileReference{{ID: "first"}, {ID: "wait"}}
		_, err := m.StartTask(t.Context(), in)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("fixture preparation not reached")
	}
	paths := stageFiles(t, m)
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	_ = os.Chtimes(paths[0], old, old)
	_, err := m.WithFileStorage(t.Context(), func(keep workerfiles.Protection, protected []string) (api.AttachmentStorage, error) {
		if keep.Complete {
			t.Fatal("inflight cache preparation was treated complete")
		}
		return workerfiles.PlainStorage(t.Context(), filepath.Join(m.root, ".inputs"), protected, true, func(string) error { t.Error("pending copy was selected"); return nil })
	})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err == nil {
		t.Fatal("cancelled preparation unexpectedly accepted")
	}
	if len(stageFiles(t, m)) != 0 {
		t.Fatal("cancelled preparation left files")
	}
}

type storageFileRuntime struct {
	*fixtureRuntime
	offline bool
	reads   int
	reject  bool
}

func (f *storageFileRuntime) ReadWorkFile(context.Context, string, string) (api.WorkFileContent, error) {
	f.reads++
	if f.offline {
		return api.WorkFileContent{}, errors.New("B offline")
	}
	return api.WorkFileContent{Name: "result.csv", Data: []byte("original checked result")}, nil
}
func (f *storageFileRuntime) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	v, err := f.fixtureRuntime.StartWork(ctx, in)
	if f.reject {
		v.Status = "failed"
		v.Outcome = "rejected"
		f.states[in.ID] = api.WorkState{Task: v}
		return v, &api.WorkMessageRejection{Cause: errors.New("definite native refusal")}
	}
	return v, err
}
func TestTaskRetrievedOutputIsReusableOfflineAndOwned(t *testing.T) {
	f := &storageFileRuntime{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	old := start(t, m, "original-output-task")
	copy, err := m.MaterializeTaskFile(t.Context(), old.ID, "original-ref", false)
	if err != nil {
		t.Fatal(err)
	}
	f.offline = true
	in := input("reuse-offline-result")
	in.Attachments = []api.TaskFileReference{{TaskID: old.ID, ID: "original-ref"}}
	v, err := m.StartTask(t.Context(), in)
	if err != nil || v.Outcome != "accepted" || f.reads != 1 {
		t.Fatal("cached output required B or lost original ownership", v, err, f.reads)
	}
	if data, err := os.ReadFile(f.lastStart.Files[0].Path); err != nil || string(data) != "original checked result" {
		t.Fatal("offline input bytes changed", err)
	}
	if _, err = m.MaterializeTaskFile(t.Context(), "foreign-task", "original-ref", false); err == nil {
		t.Fatal("cache bypassed product task ownership")
	}
	if _, err = os.Stat(copy.Path); err != nil {
		t.Fatal("offline original removed", err)
	}
}
func TestTaskNativeRejectedInputDropsOnlyCacheReferences(t *testing.T) {
	f := &storageFileRuntime{fixtureRuntime: newRuntime(), reject: true}
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureInputResolver(func(context.Context, string) (api.WorkFileContent, error) {
		return api.WorkFileContent{Name: "rejected.csv", Data: []byte("known rejected bytes")}, nil
	})
	in := input("native-refused-original")
	in.Attachments = []api.TaskFileReference{{ID: "selected-original"}}
	v, err := m.StartTask(t.Context(), in)
	if err == nil || v.Outcome != "rejected" {
		t.Fatal("fixture not rejected", v, err)
	}
	r := m.state.Records[v.ID]
	if len(stageFiles(t, m)) != 0 || len(r.InputPaths) != 0 || len(r.InputReferences) != 0 || len(r.Requests) != 1 || r.Operations[in.RequestID].RequestID != in.RequestID {
		t.Fatal("cache cleanup lost original receipt or leaked declined bytes", r, stageFiles(t, m))
	}
}
func TestTaskFileStorageKeepsLegacyAndFailedPersistenceConservative(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "codex", f)
	m.ConfigureInputResolver(func(context.Context, string) (api.WorkFileContent, error) {
		return api.WorkFileContent{Name: "input.csv", Data: []byte("unconfirmed ledger material")}, nil
	})
	write := m.write
	m.write = func() error {
		if len(m.state.Records) > 0 {
			return errors.New("unconfirmed state write")
		}
		return write()
	}
	in := input("unknown-ledger-write")
	in.Attachments = []api.TaskFileReference{{ID: "input"}}
	if _, err := m.StartTask(t.Context(), in); err == nil {
		t.Fatal("fixture did not fail ledger write")
	}
	if len(stageFiles(t, m)) != 1 {
		t.Fatal("unconfirmed state write lost material")
	}
	if _, err := m.WithFileStorage(t.Context(), func(keep workerfiles.Protection, paths []string) (api.AttachmentStorage, error) {
		if keep.Complete {
			t.Fatal("unconfirmed persistence advertised complete references")
		}
		return api.AttachmentStorage{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	m.op.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := m.WithFileStorage(ctx, func(workerfiles.Protection, []string) (api.AttachmentStorage, error) {
		t.Fatal("timed out maintenance ran late")
		return api.AttachmentStorage{}, nil
	})
	m.op.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
