package workerpeer

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditFailedStageDoesNotRetainUndeliveredBytes(t *testing.T) {
	root := t.TempDir()
	s := &Server{root: root}
	_, _, err := s.stage(t.Context(), "original-source", "task-original", []api.WorkFileContent{{Name: "report.csv", Data: []byte("sensitive selected bytes")}, {Name: "../invalid", Data: []byte("rejected")}})
	if err == nil {
		t.Fatal("expected refusal")
	}
	files := []string{}
	filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e == nil && !d.IsDir() {
			files = append(files, p)
		}
		return e
	})
	if len(files) != 0 {
		t.Fatalf("rejected preparation retained %d unowned byte files: %v", len(files), files)
	}
}

func TestStageIOFailureAndConcurrentCallsKeepOnlyTheirOwnBytes(t *testing.T) {
	s := &Server{root: t.TempDir()}
	_, _, err := s.stage(t.Context(), "source", "task", []api.WorkFileContent{{Name: "ok.csv", Data: []byte("first")}, {Name: strings.Repeat("x", 300), Data: []byte("will fail")}})
	if err == nil {
		t.Fatal("expected native filesystem filename refusal")
	}
	files, clean, err := s.stage(t.Context(), "source", "task", []api.WorkFileContent{{Name: "same.csv", Data: []byte("one")}})
	if err != nil {
		t.Fatal(err)
	}
	defer clean()
	other, done, err := s.stage(t.Context(), "source", "task", []api.WorkFileContent{{Name: "same.csv", Data: []byte("two")}})
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if files[0].Path == other[0].Path {
		t.Fatal("calls shared transport path")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.InputStorage(ctx, true, os.Remove); !errors.Is(err, context.Canceled) {
		t.Fatal("cleanup crossed active calls", err)
	}
	clean()
	clean()
	data, err := os.ReadFile(other[0].Path)
	if err != nil || string(data) != "two" {
		t.Fatal("one cleanup removed another call", string(data), err)
	}
	done()
	info, err := s.InputStorage(t.Context(), false, nil)
	if err != nil || info.Bytes != 0 {
		t.Fatal("transport bytes retained", info, err)
	}
	// Only its explicitly owned stale transport namespace is eligible; no native
	// inputs/receipts are scanned or removed by this cache inventory.
	old := filepath.Join(s.root, "uploads", "interrupted.csv")
	if err = os.WriteFile(old, []byte("orphan"), 0600); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-31 * 24 * time.Hour)
	if err = os.Chtimes(old, then, then); err != nil {
		t.Fatal(err)
	}
	info, err = s.InputStorage(t.Context(), false, nil)
	if err != nil || info.EligibleFiles != 1 {
		t.Fatal("owned interrupted copy missing from usage", info, err)
	}
	info, err = s.InputStorage(t.Context(), true, os.Remove)
	if err != nil || info.Files != 0 {
		t.Fatal(info, err)
	}
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("stale transport copy not cleaned", err)
	}
}

type stagingReceiptOwner struct {
	*fixtureOwner
	path    string
	data    []byte
	failure error
}

func (o *stagingReceiptOwner) StartWork(_ context.Context, in api.WorkStart) (api.Task, error) {
	o.path = in.Files[0].Path
	o.data, _ = os.ReadFile(o.path) // models synchronous native-owned byte retention
	return api.Task{ID: in.ID, Outcome: "unknown"}, o.failure
}
func TestDispatchReleasesTransportBytesForEveryNativeReturn(t *testing.T) {
	for _, failure := range []error{nil, errors.New("unknown original effect"), context.Canceled, &api.WorkMessageRejection{Cause: errors.New("refused")}} {
		o := &stagingReceiptOwner{fixtureOwner: newFixtureOwner(), failure: failure}
		s, ctx := auditServer(t, o)
		s.dispatch(ctx, "source-a", Request{Action: "start", Start: api.WorkStart{ID: "task-a", TaskStart: api.TaskStart{RequestID: "original-start"}}, Files: []api.WorkFileContent{{Name: "report.csv", Data: []byte("original bytes")}}})
		if string(o.data) != "original bytes" {
			t.Fatal("native did not take original bytes", failure)
		}
		if _, err := os.Stat(o.path); !os.IsNotExist(err) {
			t.Fatal("native return retained transport bytes", failure, err)
		}
	}
}
