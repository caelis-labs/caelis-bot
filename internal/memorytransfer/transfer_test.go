package memorytransfer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botmemory"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	owner "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	mem "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/appliance"
)

func put(t *testing.T, root, p string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, p), body, 0600); err != nil {
		t.Fatal(err)
	}
}

func state(t *testing.T, root, p string, value any) {
	t.Helper()
	body, err := jsonBytes(value)
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, p, body)
}

func sourceProfile(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	runtime, err := bot.New(filepath.Join(root, "bot.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	id := runtime.State().ID
	// Native execution/schedule state must not migrate, even when definitely
	// accepted. This fixture does not launch any model or schedule.
	state(t, root, "bot.json", map[string]any{"version": 1, "personalVersion": 1, "id": id, "schedules": []map[string]any{{"id": "old-schedule", "prompt": "do not migrate"}}, "wake": map[string]any{"status": "accepted", "id": "native-wake", "prompt": "do not replay"}})
	state(t, root, "bot-initialization.json", map[string]any{"version": 1, "id": "accepted-introduction", "status": "accepted", "runtime": "codex", "message": "old native metadata"})
	state(t, root, "notebook-migration.json", map[string]any{"version": 1})
	put(t, root, "Notebook/MEMORY.md", []byte("# Memory\n\nMy name is Cedar.\n"))
	put(t, root, "Notebook/2026/09/30/note.md", []byte("# Authored body\n\nA durable note.\n"))
	put(t, root, "Notebook/INDEX.md", []byte("obsolete generated navigation"))
	put(t, root, "tasks.json", []byte("private original tasks"))
	put(t, root, "providers/caelis/application.token", []byte("source provider credential"))
	store, err := botmemory.Open(context.Background(), filepath.Join(root, "personal"), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return root, id
}

func exportProfile(t *testing.T, root string, attachments ...string) string {
	t.Helper()
	bundle := filepath.Join(t.TempDir(), "bundle")
	out, err := Export(context.Background(), ExportOptions{Source: root, Bundle: bundle, SourceStopped: true, Attachments: attachments})
	if err != nil || !out.Activated {
		t.Fatalf("export: %#v, %v", out, err)
	}
	return bundle
}

func importProfile(t *testing.T, bundle string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "new-profile")
	out, err := Import(context.Background(), ImportOptions{Bundle: bundle, Destination: dst, SourceStopped: true, DestinationStopped: true})
	if err != nil || !out.Activated {
		t.Fatalf("import: %#v, %v", out, err)
	}
	return dst
}

func TestPublicMemoryRoundTripKeepsIdentityCorrectionForgettingAndOwnCredentials(t *testing.T) {
	ctx := context.Background()
	source, id := sourceProfile(t)
	store, err := botmemory.Open(ctx, filepath.Join(source, "personal"), id)
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Remember(ctx, "remember-coffee", "I like coffee in the morning.", "settings")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CorrectMemory(ctx, api.MemoryChange{RequestID: "correct-coffee", ID: old.ID, Text: "I prefer tea in the morning."}); err != nil {
		t.Fatal(err)
	}
	forgotten, err := store.Remember(ctx, "remember-private", "Private phrase to forget completely.", "settings")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CorrectMemory(ctx, api.MemoryChange{RequestID: "correct-private", ID: forgotten.ID, Text: "Replacement private phrase to forget."}); err != nil {
		t.Fatal(err)
	}
	view, err := store.ReadMemory(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var forgottenReplacement string
	for _, entry := range view.Evidence {
		if strings.Contains(entry.Text, "Replacement private") {
			forgottenReplacement = entry.ID
		}
	}
	if forgottenReplacement == "" {
		t.Fatal("replacement missing")
	}
	if err = store.ForgetMemory(ctx, api.MemoryChange{RequestID: "forget-private", ID: forgottenReplacement}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	put(t, source, "Notebook/assets/diagram.png", []byte("nonsecret referenced attachment"))
	put(t, source, "Notebook/attachment.md", []byte("# Reference\n\n![Diagram](assets/diagram.png)\n"))
	bundle := exportProfile(t, source, "assets/diagram.png")
	for _, filename := range []string{"management.token", "steward-worker.token"} {
		secret, err := os.ReadFile(filepath.Join(source, "personal", "memory", filename))
		if err != nil {
			t.Fatal(err)
		}
		files, err := listFiles(bundle)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			body, err := os.ReadFile(filepath.Join(bundle, filepath.FromSlash(file.Path)))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(body, bytes.TrimSpace(secret)) {
				t.Fatalf("source credential in %s", file.Path)
			}
		}
	}
	dst := importProfile(t, bundle)
	for _, filename := range []string{"management.token", "steward-worker.token"} {
		a, _ := os.ReadFile(filepath.Join(source, "personal", "memory", filename))
		b, err := os.ReadFile(filepath.Join(dst, "personal", "memory", filename))
		if err != nil || bytes.Equal(a, b) {
			t.Fatalf("target did not create its own %s", filename)
		}
	}
	runtime, err := bot.New(filepath.Join(dst, "bot.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if state := runtime.State(); state.ID != id || len(state.Schedules) != 0 || state.Wake != nil {
		t.Fatalf("migrated execution state: %#v", state)
	}
	intro, err := bot.OpenInitializer(filepath.Join(dst, "bot-initialization.json"))
	if err != nil || intro.Initialization().Status != "accepted" {
		t.Fatalf("introduction lost: %v", err)
	}
	for _, p := range []string{"tasks.json", "providers", "conversation.json"} {
		if _, err := os.Lstat(filepath.Join(dst, p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected migrated state %s", p)
		}
	}
	note, _ := os.ReadFile(filepath.Join(dst, "Notebook", "2026", "09", "30", "note.md"))
	if !strings.Contains(string(note), "A durable note.") {
		t.Fatal("authored note lost")
	}
	index, _ := os.ReadFile(filepath.Join(dst, "Notebook", "INDEX.md"))
	if bytes.Contains(index, []byte("obsolete")) || !bytes.Contains(index, []byte("Authored body")) {
		t.Fatal("INDEX was not rebuilt")
	}
	marker, _ := os.ReadFile(filepath.Join(dst, "notebook-migration.json"))
	if !bytes.Contains(marker, []byte(`"version": 1`)) {
		t.Fatal("migration marker missing")
	}
	store, err = botmemory.Open(ctx, filepath.Join(dst, "personal"), id)
	if err != nil {
		t.Fatal(err)
	}
	view, err = store.ReadMemory(ctx, "")
	if err != nil || len(view.Evidence) != 1 || view.Evidence[0].Text != "I prefer tea in the morning." {
		t.Fatalf("restored evidence: %#v, %v", view, err)
	}
	// Replay of old mutation IDs cannot resurrect either the forgotten chain
	// or superseded original receipt after restore.
	if _, err = store.Remember(ctx, "remember-private", "Private phrase to forget completely.", "settings"); err == nil {
		t.Fatal("forgotten memory resurrected")
	}
	if _, err = store.Remember(ctx, "remember-coffee", "I like coffee in the morning.", "settings"); err == nil {
		t.Fatal("corrected memory resurrected")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	memRuntime, err := appliance.Open(ctx, appliance.Options{DataDir: filepath.Join(dst, "personal", "memory")})
	if err != nil {
		t.Fatal(err)
	}
	defer memRuntime.Close()
	trace, err := memRuntime.Management().TraceReceipt(ctx, owner.TraceReceiptRequest{ReceiptID: mem.ReceiptID(old.ID)})
	if err != nil || trace.State != owner.ReceiptStateCorrected || trace.Receipt.CorrectedBy == "" {
		t.Fatalf("correction chain lost: %#v, %v", trace, err)
	}
	for _, receipt := range []string{forgotten.ID, forgottenReplacement} {
		trace, err := memRuntime.Management().TraceReceipt(ctx, owner.TraceReceiptRequest{ReceiptID: mem.ReceiptID(receipt)})
		if err != nil || trace.State != owner.ReceiptStateDeleted || trace.Receipt != nil || trace.Tombstone == nil {
			t.Fatalf("tombstone lost: %#v, %v", trace, err)
		}
	}
	// Reverse migration is another stopped backup/restore into a fresh profile.
	if err = memRuntime.Close(); err != nil {
		t.Fatal(err)
	}
	back := importProfile(t, exportProfile(t, dst, "assets/diagram.png"))
	store, err = botmemory.Open(ctx, filepath.Join(back, "personal"), id)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	view, err = store.ReadMemory(ctx, "")
	if err != nil || len(view.Evidence) != 1 {
		t.Fatalf("reverse restore: %#v, %v", view, err)
	}
}

func TestOfflinePreconditionsAndLiveOwner(t *testing.T) {
	root, id := sourceProfile(t)
	bundle := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(context.Background(), ExportOptions{Source: root, Bundle: bundle}); err == nil {
		t.Fatal("missing stop assertion accepted")
	}
	store, err := botmemory.Open(context.Background(), filepath.Join(root, "personal"), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Export(context.Background(), ExportOptions{Source: root, Bundle: bundle, SourceStopped: true}); err == nil {
		t.Fatal("live Memory owner accepted")
	}
	store.Close()
	if _, err := os.Stat(bundle); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed export activated bundle")
	}
	bundle = exportProfile(t, root)
	if _, err := Import(context.Background(), ImportOptions{Bundle: bundle, Destination: filepath.Join(t.TempDir(), "new")}); err == nil {
		t.Fatal("missing import stop assertion accepted")
	}
	existing := t.TempDir()
	put(t, existing, "keep.txt", []byte("existing profile"))
	if _, err := Import(context.Background(), ImportOptions{Bundle: bundle, Destination: existing, SourceStopped: true, DestinationStopped: true}); err == nil {
		t.Fatal("existing destination replaced")
	}
	if body, _ := os.ReadFile(filepath.Join(existing, "keep.txt")); string(body) != "existing profile" {
		t.Fatal("existing destination changed")
	}
}

func rewriteManifest(t *testing.T, bundle string, modify func(*Manifest)) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(bundle, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err = decode(body, &m, true); err != nil {
		t.Fatal(err)
	}
	modify(&m)
	state(t, bundle, "manifest.json", m)
}

func refreshManifestFile(t *testing.T, bundle, p string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(bundle, p))
	if err != nil {
		t.Fatal(err)
	}
	rewriteManifest(t, bundle, func(m *Manifest) {
		for i := range m.Files {
			if m.Files[i].Path == p {
				m.Files[i].Size = int64(len(body))
				m.Files[i].SHA256 = digest(body)
				return
			}
		}
		t.Fatal("file not manifested")
	})
}

func TestMalformedBundlesNeverActivate(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"missing snapshot":  func(t *testing.T, b string) { os.Remove(filepath.Join(b, "personal", "memory", "memory.db")) },
		"missing index":     func(t *testing.T, b string) { os.Remove(filepath.Join(b, "personal", "index.json")) },
		"missing migration": func(t *testing.T, b string) { os.Remove(filepath.Join(b, "notebook-migration.json")) },
		"digest":            func(t *testing.T, b string) { put(t, b, "Notebook/MEMORY.md", []byte("changed")) },
		"version":           func(t *testing.T, b string) { rewriteManifest(t, b, func(m *Manifest) { m.Format = "future.v99" }) },
		"traversal": func(t *testing.T, b string) {
			rewriteManifest(t, b, func(m *Manifest) { m.Files[0].Path = "../outside" })
		},
		"duplicate": func(t *testing.T, b string) {
			rewriteManifest(t, b, func(m *Manifest) { m.Files = append(m.Files, m.Files[0]) })
		},
		"index identity": func(t *testing.T, b string) {
			state(t, b, "personal/index.json", personalIndex{Version: 1, BotID: "different", Receipts: []string{}})
			refreshManifestFile(t, b, "personal/index.json")
		},
		"scope":              func(t *testing.T, b string) { rewriteManifest(t, b, func(m *Manifest) { m.Scope = "another-scope" }) },
		"forgotten counts":   func(t *testing.T, b string) { rewriteManifest(t, b, func(m *Manifest) { m.Governance.Barriers++ }) },
		"unsafe permissions": func(t *testing.T, b string) { os.Chmod(filepath.Join(b, "Notebook", "MEMORY.md"), 0644) },
		"unlisted token":     func(t *testing.T, b string) { put(t, b, "provider.token", []byte("secret")) },
		"symlink":            func(t *testing.T, b string) { os.Symlink("MEMORY.md", filepath.Join(b, "Notebook", "redirect.md")) },
		"unknown introduction": func(t *testing.T, b string) {
			state(t, b, "bot-initialization.json", map[string]any{"version": 1, "id": "pending-id", "status": "unknown"})
			refreshManifestFile(t, b, "bot-initialization.json")
		},
		"invalid sqlite": func(t *testing.T, b string) {
			put(t, b, "personal/memory/memory.db", []byte("invalid image"))
			refreshManifestFile(t, b, "personal/memory/memory.db")
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			root, _ := sourceProfile(t)
			bundle := exportProfile(t, root)
			change(t, bundle)
			destination := filepath.Join(t.TempDir(), "new")
			out, err := Import(context.Background(), ImportOptions{Bundle: bundle, Destination: destination, SourceStopped: true, DestinationStopped: true})
			if err == nil || out.Activated {
				t.Fatalf("invalid bundle activated: %#v, %v", out, err)
			}
			if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("destination changed")
			}
			if out.Path != "" {
				if _, err := os.Stat(out.Path); err != nil {
					t.Fatal("failed staging was not retained")
				}
			}
			if _, err := os.Stat(filepath.Join(root, "personal", "memory", "memory.db")); err != nil {
				t.Fatal("source lost")
			}
		})
	}
}

func TestRestoreAndCommitFailureRetainInactiveStaging(t *testing.T) {
	for _, boundary := range []string{"restore", "commit"} {
		t.Run(boundary, func(t *testing.T) {
			root, _ := sourceProfile(t)
			bundle := exportProfile(t, root)
			destination := filepath.Join(t.TempDir(), "new")
			restore := appliance.RestoreOwned
			commit := appliance.CommitRestoreOwned
			if boundary == "restore" {
				restore = func(context.Context, appliance.OfflineRestoreOptions) (appliance.RestoreResult, error) {
					return appliance.RestoreResult{}, errors.New("controlled restore failure")
				}
			}
			if boundary == "commit" {
				commit = func(string) error { return errors.New("controlled commit failure") }
			}
			out, err := importBundle(context.Background(), ImportOptions{Bundle: bundle, Destination: destination, SourceStopped: true, DestinationStopped: true}, restore, commit)
			if err == nil || out.Activated || out.Path == "" {
				t.Fatalf("failure activated: %#v, %v", out, err)
			}
			if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("destination changed")
			}
			if _, err := os.Stat(filepath.Join(out.Path, "Notebook", "MEMORY.md")); err != nil {
				t.Fatal("staging lost")
			}
			if boundary == "commit" {
				data := filepath.Join(out.Path, "personal", "memory")
				if r, err := appliance.Open(context.Background(), appliance.Options{DataDir: data}); err == nil {
					r.Close()
					t.Fatal("pending failed commit became writable")
				}
				if _, err := appliance.RollbackRestoreOwned(context.Background(), data, nil); err != nil {
					t.Fatalf("public rollback unavailable: %v", err)
				}
			}
		})
	}
}

func TestSourceIdentityInitializationAndAttachments(t *testing.T) {
	for _, status := range []string{"pending", "dispatching", "unknown", "rejected"} {
		t.Run(status, func(t *testing.T) {
			root, _ := sourceProfile(t)
			state(t, root, "bot-initialization.json", map[string]any{"version": 1, "id": "native-id", "status": status})
			if _, err := Export(context.Background(), ExportOptions{Source: root, Bundle: filepath.Join(t.TempDir(), "bundle"), SourceStopped: true}); err == nil {
				t.Fatal("unfinished introduction fabricated accepted")
			}
		})
	}
	t.Run("required stays required", func(t *testing.T) {
		root, _ := sourceProfile(t)
		os.Remove(filepath.Join(root, "bot-initialization.json"))
		os.Remove(filepath.Join(root, "notebook-migration.json"))
		dst := importProfile(t, exportProfile(t, root))
		i, err := bot.OpenInitializer(filepath.Join(dst, "bot-initialization.json"))
		if err != nil || !i.Initialization().Required {
			t.Fatal("missing introduction fabricated accepted")
		}
	})
	t.Run("source identity mismatch", func(t *testing.T) {
		root, _ := sourceProfile(t)
		state(t, root, "personal/index.json", personalIndex{Version: 1, BotID: "other", Receipts: []string{}})
		if _, err := Export(context.Background(), ExportOptions{Source: root, Bundle: filepath.Join(t.TempDir(), "bundle"), SourceStopped: true}); err == nil {
			t.Fatal("index mismatch accepted")
		}
	})
	t.Run("source missing database", func(t *testing.T) {
		root, _ := sourceProfile(t)
		os.Remove(filepath.Join(root, "personal", "memory", "memory.db"))
		if _, err := Export(context.Background(), ExportOptions{Source: root, Bundle: filepath.Join(t.TempDir(), "bundle"), SourceStopped: true}); err == nil {
			t.Fatal("new source Memory silently initialized")
		}
	})
	t.Run("pending wake", func(t *testing.T) {
		root, id := sourceProfile(t)
		state(t, root, "bot.json", map[string]any{"version": 1, "personalVersion": 1, "id": id, "wake": map[string]any{"status": "unknown"}})
		if _, err := Export(context.Background(), ExportOptions{Source: root, Bundle: filepath.Join(t.TempDir(), "bundle"), SourceStopped: true}); err == nil {
			t.Fatal("unknown wake accepted")
		}
	})
	for name, link := range map[string]string{"missing attachment": "assets/diagram.png", "external reference": "../../model-token.txt", "secret filename": "assets/credential.txt", "absolute": "/tmp/private.png"} {
		t.Run(name, func(t *testing.T) {
			root, _ := sourceProfile(t)
			put(t, root, "Notebook/reference.md", []byte("![x]("+link+")"))
			if _, err := Export(context.Background(), ExportOptions{Source: root, Bundle: filepath.Join(t.TempDir(), "bundle"), SourceStopped: true}); err == nil {
				t.Fatal("unsafe or incomplete reference accepted")
			}
		})
	}
	t.Run("excluded symlink", func(t *testing.T) {
		root, _ := sourceProfile(t)
		os.Symlink("MEMORY.md", filepath.Join(root, "Notebook", "credential.txt"))
		if _, err := Export(context.Background(), ExportOptions{Source: root, Bundle: filepath.Join(t.TempDir(), "bundle"), SourceStopped: true}); err == nil {
			t.Fatal("source symlink accepted")
		}
	})
	t.Run("reference definition", func(t *testing.T) {
		root, _ := sourceProfile(t)
		put(t, root, "Notebook/assets/data.csv", []byte("a,b\n1,2\n"))
		put(t, root, "Notebook/reference.md", []byte("[Data][data]\n\n[data]: assets/data.csv\n"))
		importProfile(t, exportProfile(t, root, "assets/data.csv"))
	})
}

func TestSnapshotWithDifferentBotScopeFailsBeforeActivation(t *testing.T) {
	root, _ := sourceProfile(t)
	bundle := exportProfile(t, root)
	other, _ := sourceProfile(t)
	otherBundle := exportProfile(t, other)
	body, err := os.ReadFile(filepath.Join(otherBundle, "personal", "memory", "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, bundle, "personal/memory/memory.db", body)
	refreshManifestFile(t, bundle, "personal/memory/memory.db")
	destination := filepath.Join(t.TempDir(), "new")
	out, err := Import(context.Background(), ImportOptions{Bundle: bundle, Destination: destination, SourceStopped: true, DestinationStopped: true})
	if err == nil || out.Activated {
		t.Fatal("different snapshot scope activated")
	}
}

func TestIndexUnknownFieldsCannotCarryCredentials(t *testing.T) {
	root, id := sourceProfile(t)
	if err := localstate.Write(filepath.Join(root, "personal", "index.json"), map[string]any{"version": 1, "botId": id, "receipts": []string{}, "credential": "do-not-copy"}); err != nil {
		t.Fatal(err)
	}
	bundle := exportProfile(t, root)
	body, _ := os.ReadFile(filepath.Join(bundle, "personal", "index.json"))
	if bytes.Contains(body, []byte("do-not-copy")) {
		t.Fatal("unknown credential field copied")
	}
}

func TestTransferRejectsAliasedOverlappingTrees(t *testing.T) {
	root, _ := sourceProfile(t)
	parent := t.TempDir()
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(filepath.Dir(root), alias); err != nil {
		t.Fatal(err)
	}
	aliasedSource := filepath.Join(alias, filepath.Base(root))
	if _, err := Export(context.Background(), ExportOptions{Source: aliasedSource, Bundle: filepath.Join(root, "bundle"), SourceStopped: true}); err == nil {
		t.Fatal("alias concealed bundle inside source profile")
	}
}

func TestLegacyPersonalVersionIsPreserved(t *testing.T) {
	root, id := sourceProfile(t)
	state(t, root, "bot.json", map[string]any{"version": 1, "personalVersion": 0, "id": id, "schedules": []struct{}{}})
	destination := importProfile(t, exportProfile(t, root))
	runtime, err := bot.New(filepath.Join(destination, "bot.json"), nil)
	if err != nil || runtime.State().PersonalVersion != 0 || runtime.State().ID != id {
		t.Fatalf("identity fields changed: %v", err)
	}
}
