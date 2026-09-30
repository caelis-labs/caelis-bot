package memorytransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botmemory"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

func snapshotDir(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func snapshotSource(t *testing.T) (string, string) {
	t.Helper()
	source, id := sourceProfile(t)
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	return source, id
}

func snapshotExport(t *testing.T, source, epoch, version string, attachments ...string) ([]byte, nodeplane.SnapshotRef) {
	t.Helper()
	payload, ref, err := ExportNotebook(t.Context(), NotebookExportOptions{Source: source, SourceStopped: true, Epoch: epoch, Version: version, Attachments: attachments})
	if err != nil {
		t.Fatal(err)
	}
	validated, err := ValidateNotebookPayload(t.Context(), payload)
	if err != nil || validated != ref {
		t.Fatalf("validated: %#v %v", validated, err)
	}
	return payload, ref
}

func snapshotGate(latest *nodeplane.SnapshotRef) NotebookCommit {
	return func(ctx context.Context, ref nodeplane.SnapshotRef, install func() error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if *latest != ref {
			return errors.New("latest snapshot changed")
		}
		return install()
	}
}

func snapshotApply(t *testing.T, payload []byte, ref nodeplane.SnapshotRef) string {
	t.Helper()
	destination := filepath.Join(snapshotDir(t), "profile")
	out, err := ApplyNotebook(t.Context(), NotebookApplyOptions{Payload: payload, Destination: destination, DestinationStopped: true, Expected: ref, Commit: snapshotGate(&ref)})
	if err != nil || !out.Activated || out.Path != destination {
		t.Fatalf("apply: %#v %v", out, err)
	}
	return destination
}

func TestNotebookSnapshotCurrentBodiesFreshMemoryIdentityAndNoHandoffReplay(t *testing.T) {
	source, id := snapshotSource(t)
	store, err := botmemory.Open(t.Context(), filepath.Join(source, "personal"), id)
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Remember(t.Context(), "original-coffee", "Old coffee preference", "settings")
	if err != nil {
		t.Fatal(err)
	}
	// This source receipt must never be coupled to a fresh target SQLite image.
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	put(t, source, "Notebook/MEMORY.md", []byte("# Memory\nI now prefer tea.\n"))
	put(t, source, "Notebook/HANDOFF.md", []byte(notebook.DreamMarker("completed")+"\nCompleted action; do not replay.\n"))
	put(t, source, "Notebook/assets/diagram.png", []byte("referenced picture"))
	put(t, source, "Notebook/picture.md", []byte("![Diagram](assets/diagram.png)\n"))
	payload, ref := snapshotExport(t, source, "epoch-one", "1", "assets/diagram.png")
	if bytes.Contains(payload, []byte(old.ID)) || bytes.Contains(payload, []byte("personal/index.json")) || bytes.Contains(payload, []byte("memory.db")) || bytes.Contains(payload, []byte("HANDOFF.md")) || bytes.Contains(payload, []byte("tasks.json")) {
		t.Fatal("snapshot copied execution/evidence state")
	}
	payload2, ref2 := snapshotExport(t, source, "epoch-one", "1", "assets/diagram.png")
	if !bytes.Equal(payload, payload2) || ref != ref2 {
		t.Fatal("codec is not deterministic")
	}
	dst := snapshotApply(t, payload, ref)
	installedRef, err := ReadInstalledNotebookRef(t.Context(), dst)
	if err != nil || installedRef != ref {
		t.Fatal("installed snapshot receipt mismatch", installedRef, err)
	}
	runtime, err := bot.New(filepath.Join(dst, "bot.json"), nil)
	if err != nil || runtime.State().ID != id || runtime.State().PersonalVersion != 1 || len(runtime.State().Schedules) != 0 || runtime.State().Wake != nil {
		t.Fatalf("identity: %#v %v", runtime, err)
	}
	intro, err := bot.OpenInitializer(filepath.Join(dst, "bot-initialization.json"))
	if err != nil || intro.Initialization().Status != "accepted" {
		t.Fatal("introduction changed", err)
	}
	for _, p := range []string{"Notebook/HANDOFF.md", "providers", "tasks.json", "connection.json"} {
		if _, err := os.Lstat(filepath.Join(dst, p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old state installed: %s", p)
		}
	}
	store, err = botmemory.Open(t.Context(), filepath.Join(dst, "personal"), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"", "coffee", "tea"} {
		view, err := store.ReadMemory(t.Context(), q)
		if err != nil || len(view.Evidence) != 0 {
			t.Fatalf("fresh public recall: %#v %v", view, err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	var index personalIndex
	b, err := os.ReadFile(filepath.Join(dst, "personal", "index.json"))
	if err != nil || json.Unmarshal(b, &index) != nil || index.BotID != id || index.Receipts == nil || len(index.Receipts) != 0 {
		t.Fatal("fresh index incoherent", err)
	}
	vault, err := notebook.OpenVault(filepath.Join(dst, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	seed, err := vault.PrepareContext(t.Context())
	if err != nil || !strings.Contains(seed.Text, "I now prefer tea.") || seed.HandoffDigest != "" || strings.Contains(seed.Text, "Completed action") {
		t.Fatalf("fresh context: %#v %v", seed, err)
	}
	if err := vault.ConsumeContext(seed); err != nil {
		t.Fatal(err)
	}
	seed2, err := vault.PrepareContext(t.Context())
	if err != nil || seed2.Text != seed.Text {
		t.Fatal("periodic data produced a one-use replay", err)
	}
	b, err = os.ReadFile(filepath.Join(dst, "Notebook", "INDEX.md"))
	if err != nil || bytes.Contains(b, []byte("obsolete generated")) || !bytes.Contains(b, []byte("picture.md")) {
		t.Fatal("index was not rebuilt", err)
	}
	b, err = os.ReadFile(filepath.Join(dst, "Notebook", "assets", "diagram.png"))
	if err != nil || string(b) != "referenced picture" {
		t.Fatal("attachment missing", err)
	}
}

func TestNotebookSnapshotDeletionReplacementAndStaleApplyCAS(t *testing.T) {
	source, _ := snapshotSource(t)
	put(t, source, "Notebook/forgotten.md", []byte("# Forgotten\nOld secret preference.\n"))
	oldPayload, oldRef := snapshotExport(t, source, "epoch-old", "1")
	oldProfile := snapshotApply(t, oldPayload, oldRef)
	if err := os.Remove(filepath.Join(source, "Notebook", "forgotten.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "Notebook", "2026", "09", "30", "note.md")); err != nil {
		t.Fatal(err)
	}
	put(t, source, "Notebook/MEMORY.md", []byte("# Memory\nCorrected current understanding.\n"))
	newPayload, newRef := snapshotExport(t, source, "epoch-new", "2")
	newProfile := snapshotApply(t, newPayload, newRef)
	for _, p := range []string{"forgotten.md", "2026/09/30/note.md"} {
		if _, err := os.Stat(filepath.Join(newProfile, "Notebook", p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("deleted body resurrected: %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(oldProfile, "Notebook", "forgotten.md")); err != nil {
		t.Fatal("old complete generation damaged", err)
	}
	// A stale node starts staging its valid old snapshot; latest changes before
	// the actual commit. The final CAS must prevent installation/resurrection.
	latest := oldRef
	dst := filepath.Join(snapshotDir(t), "stale-profile")
	out, err := ApplyNotebook(t.Context(), NotebookApplyOptions{Payload: oldPayload, Destination: dst, DestinationStopped: true, Expected: oldRef, Commit: func(ctx context.Context, ref nodeplane.SnapshotRef, install func() error) error {
		latest = newRef
		return snapshotGate(&latest)(ctx, ref, install)
	}})
	if err == nil || out.Activated {
		t.Fatal("stale snapshot installed", out)
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale destination appeared", err)
	}
	// Resynchronizing exactly the latest full snapshot restores eligibility.
	out, err = ApplyNotebook(t.Context(), NotebookApplyOptions{Payload: newPayload, Destination: dst, DestinationStopped: true, Expected: newRef, Commit: snapshotGate(&latest)})
	if err != nil || !out.Activated {
		t.Fatal("latest resync failed", out, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "Notebook", "forgotten.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("resync merged deleted note")
	}
}

func TestNotebookSnapshotChecksumAndAtomicFailurePreservePriorData(t *testing.T) {
	source, _ := snapshotSource(t)
	payload, ref := snapshotExport(t, source, "epoch-one", "1")
	var s NotebookSnapshot
	if err := json.Unmarshal(payload, &s); err != nil {
		t.Fatal(err)
	}
	s.Files[0].Body = append(s.Files[0].Body, 'x')
	tampered, err := jsonBytes(s)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(snapshotDir(t), "profile")
	called := false
	_, err = ApplyNotebook(t.Context(), NotebookApplyOptions{Payload: tampered, Destination: dst, DestinationStopped: true, Expected: ref, Commit: func(context.Context, nodeplane.SnapshotRef, func() error) error { called = true; return nil }})
	if err == nil || called {
		t.Fatal("checksum failure reached publication")
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("checksum failure wrote destination")
	}
	// Simulate an unrelated profile appearing at commit. A single rename must
	// refuse it and preserve both its data and the complete inactive staging.
	out, err := ApplyNotebook(t.Context(), NotebookApplyOptions{Payload: payload, Destination: dst, DestinationStopped: true, Expected: ref, Commit: func(ctx context.Context, r nodeplane.SnapshotRef, install func() error) error {
		if err := os.Mkdir(dst, 0700); err != nil {
			return err
		}
		put(t, dst, "keep.txt", []byte("prior profile"))
		return install()
	}})
	if err == nil || out.Activated || out.Path == dst {
		t.Fatal("atomic conflict reported success", out, err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "keep.txt"))
	if err != nil || string(b) != "prior profile" {
		t.Fatal("prior profile changed", err)
	}
	if _, err := os.Stat(filepath.Join(out.Path, "Notebook", "MEMORY.md")); err != nil {
		t.Fatal("complete staging not retained", err)
	}
}

func TestNotebookSnapshotRejectsUnsafeFilesReferencesAndUngatedApply(t *testing.T) {
	source, _ := snapshotSource(t)
	payload, ref := snapshotExport(t, source, "epoch", "1")
	var base NotebookSnapshot
	if err := json.Unmarshal(payload, &base); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../escape.md", "Notebook/../escape.md", "Notebook/HANDOFF.md", "Notebook/INDEX.md", "personal/index.json", "personal/memory/memory.db", "providers/token", "Notebook/secret-token.png"} {
		t.Run(p, func(t *testing.T) {
			s := base
			s.Files = append(append([]NotebookFile{}, base.Files...), NotebookFile{File: File{Path: p}, Body: []byte("unsafe")})
			if _, _, err := EncodeNotebook(t.Context(), s); err == nil {
				t.Fatal("unsafe file accepted")
			}
		})
	}
	for _, v := range []string{"0", "01", "+1", "-1", "18446744073709551616"} {
		s := base
		s.Version = v
		if _, _, err := EncodeNotebook(t.Context(), s); err == nil {
			t.Fatal("noncanonical version", v)
		}
	}
	if _, _, err := ExportNotebook(t.Context(), NotebookExportOptions{Source: source, Epoch: "epoch", Version: "1"}); err == nil {
		t.Fatal("ungated export")
	}
	if _, err := ApplyNotebook(t.Context(), NotebookApplyOptions{Payload: payload, Destination: filepath.Join(snapshotDir(t), "profile"), Expected: ref}); err == nil {
		t.Fatal("ungated apply")
	}
	put(t, source, "Notebook/assets/picture.png", []byte("picture"))
	if _, _, err := ExportNotebook(t.Context(), NotebookExportOptions{Source: source, SourceStopped: true, Epoch: "epoch", Version: "1", Attachments: []string{"assets/picture.png"}}); err == nil {
		t.Fatal("unreferenced attachment accepted")
	}
	put(t, source, "Notebook/MEMORY.md", []byte("![Picture](/absolute/machine/file.png)"))
	if _, _, err := ExportNotebook(t.Context(), NotebookExportOptions{Source: source, SourceStopped: true, Epoch: "epoch", Version: "1"}); err == nil {
		t.Fatal("absolute machine reference accepted")
	}
	put(t, source, "Notebook/MEMORY.md", []byte("# Memory"))
	outside := filepath.Join(snapshotDir(t), "outside.md")
	put(t, filepath.Dir(outside), filepath.Base(outside), []byte("external body"))
	if err := os.Symlink(outside, filepath.Join(source, "Notebook", "redirect.md")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExportNotebook(t.Context(), NotebookExportOptions{Source: source, SourceStopped: true, Epoch: "epoch", Version: "1"}); err == nil {
		t.Fatal("redirected source accepted")
	}
	parent := snapshotDir(t)
	alias := filepath.Join(snapshotDir(t), "alias")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyNotebook(t.Context(), NotebookApplyOptions{Payload: payload, Destination: filepath.Join(alias, "profile"), DestinationStopped: true, Expected: ref, Commit: snapshotGate(&ref)}); err == nil {
		t.Fatal("redirected target parent accepted")
	}
}

func TestNotebookSnapshotBoundsAndInstalledReceiptIdentity(t *testing.T) {
	source, _ := snapshotSource(t)
	payload, ref := snapshotExport(t, source, "epoch", "1")
	var s NotebookSnapshot
	if err := json.Unmarshal(payload, &s); err != nil {
		t.Fatal(err)
	}
	large := s
	large.Files = append(append([]NotebookFile{}, s.Files...), NotebookFile{File: File{Path: "Notebook/large.md"}, Body: make([]byte, maxNotebookFile+1)})
	if _, _, err := EncodeNotebook(t.Context(), large); err == nil {
		t.Fatal("unbounded file accepted")
	}
	large = s
	large.Files = make([]NotebookFile, maxNotebookEntries+1)
	if _, _, err := EncodeNotebook(t.Context(), large); err == nil {
		t.Fatal("unbounded entries accepted")
	}
	if _, err := ValidateNotebookPayload(t.Context(), make([]byte, MaxNotebookPayload+1)); err == nil {
		t.Fatal("unbounded payload accepted")
	}
	dst := snapshotApply(t, payload, ref)
	state(t, dst, "bot.json", Identity{Version: 1, PersonalVersion: 1, ID: "other-bot", Schedules: []struct{}{}})
	if _, err := ReadInstalledNotebookRef(t.Context(), dst); err == nil {
		t.Fatal("installed snapshot trusted a different Bot")
	}
}
