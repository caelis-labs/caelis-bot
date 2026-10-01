// Package memorytransfer moves a stopped Bot's identity and memory into a new
// local profile. It never starts a Bot, model, scheduler or SSH connection.
package memorytransfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/botmemory"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
	owner "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	mem "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/appliance"
)

const Format = "caelis.bot-memory.v1"

type Identity struct {
	Version         int        `json:"version"`
	PersonalVersion int        `json:"personalVersion"`
	ID              string     `json:"id"`
	Schedules       []struct{} `json:"schedules"`
}

type personalIndex struct {
	Version  int      `json:"version"`
	BotID    string   `json:"botId"`
	Receipts []string `json:"receipts"`
}

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest includes no bearer credentials or native execution bindings. Receipt
// digests check the authoritative index against the restored owner plane.
type Manifest struct {
	Format         string                      `json:"format"`
	BotID          string                      `json:"botId"`
	Scope          string                      `json:"scope"`
	SchemaVersion  int                         `json:"schemaVersion"`
	Files          []File                      `json:"files"`
	Attachments    []string                    `json:"attachments"`
	Counts         map[string]int64            `json:"counts"`
	Receipts       owner.ReceiptDiagnostics    `json:"receipts"`
	Governance     owner.GovernanceDiagnostics `json:"governance"`
	ReceiptDigests map[string]string           `json:"receiptDigests"`
}

type ExportOptions struct {
	Source string
	Bundle string
	// SourceStopped affirms that admission, all direct file writers, scheduled
	// work and automatic restart have been stopped by the operator. The Memory
	// owner lock additionally rejects a live store, but cannot fence file tools.
	// The operator must reconcile outstanding effects and explicitly end or
	// leave original Workers under source-node management before moving memory.
	SourceStopped bool
	// Attachments are explicit Notebook-relative, referenced nonsecret files.
	Attachments []string
}

type ImportOptions struct {
	Bundle      string
	Destination string
	// SourceStopped affirms the source is still stopped throughout activation.
	// DestinationStopped also covers direct writers to the target parent.
	SourceStopped      bool
	DestinationStopped bool
}

// Result reports retained staging on failure. Activation only installs files;
// starting the target with its own Runtime login is a separate operator action.
type Result struct {
	BotID     string
	Path      string
	Activated bool
}

func scopeFor(id string) string {
	h := sha256.Sum256([]byte(id))
	return "bot-" + hex.EncodeToString(h[:16])
}

func Export(ctx context.Context, in ExportOptions) (out Result, err error) {
	if !in.SourceStopped {
		return out, errors.New("stop the source Bot, direct writers and automatic restart before export")
	}
	if err = distinctPaths(in.Source, in.Bundle); err != nil {
		return out, err
	}
	if err = requireDirectory(in.Source, false); err != nil {
		return out, err
	}
	// Reject a live owner before reading cross-component source state. Missing
	// storage must fail rather than initializing a new source appliance.
	data := filepath.Join(in.Source, "personal", "memory")
	if err = requireDirectory(filepath.Join(in.Source, "personal"), false); err != nil {
		return out, err
	}
	if err = checkTree(data, false); err != nil {
		return out, err
	}
	if _, err = os.Lstat(filepath.Join(data, "memory.db")); err != nil {
		return out, err
	}
	runtime, err := appliance.Open(ctx, appliance.Options{DataDir: data})
	if err != nil {
		return out, fmt.Errorf("source Memory must be stopped: %w", err)
	}
	defer func() { err = errors.Join(err, runtime.Close()) }()
	files, identity, index, err := sourceFiles(in.Source, in.Attachments)
	if err != nil {
		return out, err
	}
	out.BotID = identity.ID
	manifest := Manifest{Format: Format, BotID: identity.ID, Scope: scopeFor(identity.ID), Attachments: append([]string{}, in.Attachments...)}
	if err = captureMemory(ctx, runtime, index, &manifest); err != nil {
		return out, err
	}
	stage, release, err := reserve(in.Bundle, ".memory-export-")
	if err != nil {
		return out, err
	}
	defer release()
	out.Path = stage
	for path, body := range files {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		if err = writeFile(stage, path, body); err != nil {
			return out, err
		}
	}
	backup, err := createFile(stage, "personal/memory/memory.db")
	if err != nil {
		return out, err
	}
	err = runtime.Backup(ctx, backup)
	if err == nil {
		err = backup.Sync()
	}
	err = errors.Join(err, backup.Close())
	if err != nil {
		return out, err
	}
	// The owner lock does not stop an unrelated direct Notebook writer. Detect
	// persistent changes across the export; the explicit stopped precondition is
	// still required because a write-and-revert cannot be detected this way.
	after, afterIdentity, afterIndex, err := sourceFiles(in.Source, in.Attachments)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(files, after) || !reflect.DeepEqual(identity, afterIdentity) || !reflect.DeepEqual(index, afterIndex) {
		return out, errors.New("source identity or Notebook changed during export; keep it stopped and retry")
	}
	manifest.Files, err = listFiles(stage)
	if err != nil {
		return out, err
	}
	if err = writeJSON(stage, "manifest.json", manifest); err != nil {
		return out, err
	}
	if _, _, _, err = validateBundle(stage); err != nil {
		return out, err
	}
	if err = runtime.Close(); err != nil {
		return out, err
	}
	if err = activate(stage, in.Bundle); err != nil {
		return out, err
	}
	out.Path, out.Activated = in.Bundle, true
	return out, nil
}

func Import(ctx context.Context, in ImportOptions) (Result, error) {
	return importBundle(ctx, in, appliance.RestoreOwned, appliance.CommitRestoreOwned)
}

// The two ports permit testing failures in public restore/commit boundaries;
// production always uses the released appliance implementation.
func importBundle(ctx context.Context, in ImportOptions, restore func(context.Context, appliance.OfflineRestoreOptions) (appliance.RestoreResult, error), commit func(string) error) (out Result, err error) {
	if !in.SourceStopped || !in.DestinationStopped {
		return out, errors.New("keep source and destination stopped, including direct writers and automatic restart, before import")
	}
	if err = distinctPaths(in.Bundle, in.Destination); err != nil {
		return out, err
	}
	manifest, files, index, err := validateBundle(in.Bundle)
	if err != nil {
		return out, err
	}
	out.BotID = manifest.BotID
	stage, release, err := reserve(in.Destination, ".memory-import-")
	if err != nil {
		return out, err
	}
	defer release()
	out.Path = stage
	data := filepath.Join(stage, "personal", "memory")
	// Create target-owned credentials without copying any source token files.
	runtime, err := appliance.Open(ctx, appliance.Options{DataDir: data})
	if err != nil {
		return out, err
	}
	info, inspectErr := runtime.Management().Inspect(ctx)
	err = errors.Join(inspectErr, runtime.Close())
	if err != nil {
		return out, err
	}
	var required uint64
	for _, file := range manifest.Files {
		required += uint64(file.Size)
	}
	// Leave room for a restore staging image, SQLite work and generated INDEX.
	required += uint64(len(files["personal/memory/memory.db"])) + 16<<20
	if info.Storage.AvailableBytes < required {
		return out, errors.New("insufficient target space for inactive restore staging")
	}
	for path, body := range files {
		if path == "personal/memory/memory.db" {
			continue
		}
		if err = ctx.Err(); err != nil {
			return out, err
		}
		if err = writeFile(stage, path, body); err != nil {
			return out, err
		}
	}
	restored, err := restore(ctx, appliance.OfflineRestoreOptions{DataDir: data, Snapshot: bytes.NewReader(files["personal/memory/memory.db"])})
	if err != nil {
		return out, fmt.Errorf("restore into inactive staging: %w", err)
	}
	if restored.SchemaVersion != manifest.SchemaVersion || restored.SourceGeneration == "" || restored.StorageGeneration == "" || restored.SourceGeneration == restored.StorageGeneration {
		return out, errors.New("restored snapshot schema or generation transition is invalid")
	}
	// v0.6.1 Open refuses restore-pending generations, including owner reads.
	// RestoreOwned already validates the SQLite image/schema; commit only this
	// inactive staging, then check all product authority before installation.
	if err = commit(data); err != nil {
		return out, fmt.Errorf("commit inactive staging restore: %w", err)
	}
	runtime, err = appliance.Open(ctx, appliance.Options{DataDir: data})
	if err != nil {
		return out, err
	}
	err = verifyMemory(ctx, runtime, index, manifest)
	err = errors.Join(err, runtime.Close())
	if err != nil {
		return out, err
	}
	// Normal Bot store startup rotates the source issuer, validates its scope,
	// and exercises receipt reads using the original authoritative index.
	store, err := botmemory.Open(ctx, filepath.Join(stage, "personal"), manifest.BotID)
	if err != nil {
		return out, err
	}
	_, err = store.ReadMemory(ctx, "")
	err = errors.Join(err, store.Close())
	if err != nil {
		return out, err
	}
	vault, err := notebook.OpenVault(filepath.Join(stage, "Notebook"))
	if err != nil {
		return out, err
	}
	err = vault.Refresh(ctx, time.Now())
	err = errors.Join(err, vault.Close())
	if err != nil {
		return out, err
	}
	if err = activate(stage, in.Destination); err != nil {
		return out, err
	}
	out.Path, out.Activated = in.Destination, true
	return out, nil
}

func captureMemory(ctx context.Context, runtime *appliance.Runtime, index personalIndex, m *Manifest) error {
	info, err := runtime.Management().Inspect(ctx)
	if err != nil {
		return err
	}
	if err = checkScope(info, m.Scope); err != nil {
		return err
	}
	if info.RestorePending || info.Governance.PendingCleanups != 0 {
		return errors.New("source Memory recovery or forgetting cleanup is incomplete")
	}
	m.SchemaVersion, m.Counts, m.Receipts, m.Governance = info.SchemaVersion, info.Counts, info.Receipts, info.Governance
	m.ReceiptDigests, err = receiptDigests(ctx, runtime, index, m.Scope)
	return err
}

func verifyMemory(ctx context.Context, runtime *appliance.Runtime, index personalIndex, m Manifest) error {
	info, err := runtime.Management().Inspect(ctx)
	if err != nil {
		return err
	}
	if err = checkScope(info, m.Scope); err != nil {
		return err
	}
	if info.SchemaVersion != m.SchemaVersion || !reflect.DeepEqual(info.Counts, m.Counts) || !reflect.DeepEqual(info.Receipts, m.Receipts) || !reflect.DeepEqual(info.Governance, m.Governance) {
		return errors.New("restored Memory schema, receipts or forgetting barriers differ from the source")
	}
	digests, err := receiptDigests(ctx, runtime, index, m.Scope)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(digests, m.ReceiptDigests) {
		return errors.New("restored receipt contents or correction chains differ from the source")
	}
	return nil
}

func checkScope(info owner.Inspection, scope string) error {
	if info.SchemaVersion < 1 || len(info.Spaces) != 1 {
		return errors.New("Memory has no unique supported Bot scope")
	}
	s := info.Spaces[0]
	if string(s.ID) != scope || string(s.IdentityID) != scope || string(s.RealmID) != scope || s.Class != mem.SpaceClassPrivate {
		return errors.New("BotID, index and Memory scope do not match")
	}
	return nil
}

func receiptDigests(ctx context.Context, runtime *appliance.Runtime, index personalIndex, scope string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range index.Receipts {
		if _, exists := out[id]; exists {
			return nil, errors.New("duplicate receipt in personal index")
		}
		trace, err := runtime.Management().TraceReceipt(ctx, owner.TraceReceiptRequest{ReceiptID: mem.ReceiptID(id)})
		if err != nil {
			return nil, err
		}
		if trace.Receipt != nil && string(trace.Receipt.SpaceID) == scope || trace.Tombstone != nil && string(trace.Tombstone.SpaceID) == scope {
			body, err := jsonBytes(trace)
			if err != nil {
				return nil, err
			}
			out[id] = digest(body)
		} else {
			return nil, errors.New("indexed receipt does not belong to the Bot scope")
		}
	}
	return out, nil
}
