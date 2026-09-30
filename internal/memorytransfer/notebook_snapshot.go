package memorytransfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/botmemory"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

// NotebookFormat is separate from Format: it never carries Memory receipts or
// a SQLite image. The complete file list replaces a generation, including deletes.
const NotebookFormat = "caelis.bot-notebook.v1"
const MaxNotebookPayload = 16 << 20
const maxNotebookFile = 8 << 20
const maxNotebookBytes = 10 << 20
const maxNotebookEntries = 4096

type NotebookFile struct {
	File
	Body []byte `json:"body"`
}

// NotebookSnapshot is portable data, never authority to execute saved work.
// Digest is outside the payload to avoid a self-referential checksum.
type NotebookSnapshot struct {
	Format      string         `json:"format"`
	BotID       string         `json:"botId"`
	Epoch       string         `json:"epoch"`
	Version     string         `json:"version"`
	Files       []NotebookFile `json:"files"`
	Attachments []string       `json:"attachments"`
}

type NotebookExportOptions struct {
	Source        string
	SourceStopped bool // all writers and autonomous admission must be gated
	Epoch         string
	Version       string
	Attachments   []string
}

// NotebookCommit must hold the owner's latest-snapshot CAS and stopped/standby
// gate while calling install, or return an error without calling it. A settings
// or UI gate is insufficient. It must reject an older epoch/version/digest even
// when the caller's payload was valid before staging began.
type NotebookCommit func(context.Context, nodeplane.SnapshotRef, func() error) error

type NotebookApplyOptions struct {
	Payload            []byte
	Destination        string // absent generation; never merge into an old profile
	DestinationStopped bool
	Expected           nodeplane.SnapshotRef
	Commit             NotebookCommit
}

type notebookInstallation struct {
	Format   string                `json:"format"`
	Version  int                   `json:"version"`
	Snapshot nodeplane.SnapshotRef `json:"snapshot"`
}

// ReadInstalledNotebookRef reads the local import receipt, binding it to the
// profile identity. It is evidence of installation, not a lease or permission
// to start: the lifecycle owner must still compare against broker latest.
func ReadInstalledNotebookRef(ctx context.Context, profile string) (nodeplane.SnapshotRef, error) {
	if err := ctx.Err(); err != nil {
		return nodeplane.SnapshotRef{}, err
	}
	if err := rejectRedirectedPath(profile); err != nil {
		return nodeplane.SnapshotRef{}, err
	}
	read := func(p string, limit int64) ([]byte, error) {
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > limit {
			return nil, errors.New("invalid bounded private snapshot receipt file")
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		after, err := f.Stat()
		if err != nil || !os.SameFile(info, after) {
			return nil, errors.New("snapshot receipt changed while opening")
		}
		b, err := io.ReadAll(io.LimitReader(f, limit+1))
		if int64(len(b)) > limit {
			return nil, errors.New("snapshot receipt exceeds limit")
		}
		return b, err
	}
	b, err := read(filepath.Join(profile, "notebook-snapshot.json"), 4096)
	if err != nil {
		return nodeplane.SnapshotRef{}, err
	}
	var receipt notebookInstallation
	if len(b) > 4096 || decode(b, &receipt, true) != nil || receipt.Format != NotebookFormat || receipt.Version != 1 || !validSnapshotToken(receipt.Snapshot.BotID) || !validSnapshotToken(receipt.Snapshot.Epoch) || !validSnapshotVersion(receipt.Snapshot.Version) {
		return nodeplane.SnapshotRef{}, errors.New("invalid Notebook installation receipt")
	}
	if len(receipt.Snapshot.Digest) != 64 {
		return nodeplane.SnapshotRef{}, errors.New("invalid installed Notebook checksum")
	}
	for _, r := range receipt.Snapshot.Digest {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return nodeplane.SnapshotRef{}, errors.New("invalid installed Notebook checksum")
		}
	}
	b, err = read(filepath.Join(profile, "bot.json"), 2<<20)
	if err != nil {
		return nodeplane.SnapshotRef{}, err
	}
	var id Identity
	if len(b) > 2<<20 || decode(b, &id, false) != nil || id.ID != receipt.Snapshot.BotID || id.Version != 1 || id.PersonalVersion != 1 {
		return nodeplane.SnapshotRef{}, errors.New("installed Notebook receipt identity mismatch")
	}
	return receipt.Snapshot, nil
}

func validSnapshotToken(s string) bool {
	if s == "" || len(s) > 256 || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validSnapshotVersion(s string) bool {
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == s
}

func snapshotRef(s NotebookSnapshot, payload []byte) nodeplane.SnapshotRef {
	return nodeplane.SnapshotRef{BotID: s.BotID, Epoch: s.Epoch, Version: s.Version, Digest: digest(payload)}
}

// EncodeNotebook is a pure, bounded codec for a complete snapshot. It computes
// canonical sorted checksums, then validates the same receiver allowlist.
func EncodeNotebook(ctx context.Context, s NotebookSnapshot) ([]byte, nodeplane.SnapshotRef, error) {
	if len(s.Files) > maxNotebookEntries || len(s.Attachments) > maxNotebookEntries {
		return nil, nodeplane.SnapshotRef{}, errors.New("too many Notebook snapshot entries")
	}
	s.Format = NotebookFormat
	s.Files = append([]NotebookFile(nil), s.Files...)
	s.Attachments = append([]string{}, s.Attachments...)
	for i := range s.Files {
		if err := ctx.Err(); err != nil {
			return nil, nodeplane.SnapshotRef{}, err
		}
		if len(s.Files[i].Body) > maxNotebookFile {
			return nil, nodeplane.SnapshotRef{}, errors.New("Notebook file exceeds limit")
		}
		s.Files[i].Size = int64(len(s.Files[i].Body))
		s.Files[i].SHA256 = digest(s.Files[i].Body)
	}
	sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Path < s.Files[j].Path })
	sort.Strings(s.Attachments)
	if err := validateNotebook(ctx, s); err != nil {
		return nil, nodeplane.SnapshotRef{}, err
	}
	payload, err := jsonBytes(s)
	if err != nil || len(payload) > MaxNotebookPayload {
		return nil, nodeplane.SnapshotRef{}, errors.New("Notebook payload exceeds limit")
	}
	return payload, snapshotRef(s, payload), nil
}

func decodeNotebook(ctx context.Context, payload []byte) (NotebookSnapshot, nodeplane.SnapshotRef, error) {
	var s NotebookSnapshot
	if len(payload) > MaxNotebookPayload {
		return s, nodeplane.SnapshotRef{}, errors.New("Notebook payload exceeds limit")
	}
	if err := ctx.Err(); err != nil {
		return s, nodeplane.SnapshotRef{}, err
	}
	if err := decode(payload, &s, true); err != nil {
		return s, nodeplane.SnapshotRef{}, err
	}
	if err := validateNotebook(ctx, s); err != nil {
		return s, nodeplane.SnapshotRef{}, err
	}
	canonical, err := jsonBytes(s)
	if err != nil || !bytes.Equal(canonical, payload) {
		return s, nodeplane.SnapshotRef{}, errors.New("Notebook payload is not canonical")
	}
	return s, snapshotRef(s, payload), nil
}

func ValidateNotebookPayload(ctx context.Context, payload []byte) (nodeplane.SnapshotRef, error) {
	_, ref, err := decodeNotebook(ctx, payload)
	return ref, err
}

func validateNotebook(ctx context.Context, s NotebookSnapshot) error {
	if s.Format != NotebookFormat || !validSnapshotToken(s.BotID) || !validSnapshotToken(s.Epoch) || !validSnapshotVersion(s.Version) || len(s.Files) > maxNotebookEntries || len(s.Attachments) > maxNotebookEntries {
		return errors.New("unsupported Notebook snapshot identity or version")
	}
	attachments := map[string]bool{}
	for i, p := range s.Attachments {
		if len(p) > 1024 || !allowedAttachment(p) || attachments[p] || i > 0 && s.Attachments[i-1] >= p {
			return errors.New("unsafe or noncanonical Notebook attachment allowlist")
		}
		attachments[p] = true
	}
	files := map[string][]byte{}
	var total int64
	for i, f := range s.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(f.Path) > 1024 || !safePath(f.Path) || i > 0 && s.Files[i-1].Path >= f.Path || f.Size < 0 || f.Size > maxNotebookFile || int64(len(f.Body)) != f.Size || digest(f.Body) != f.SHA256 {
			return errors.New("invalid Notebook file path, size or checksum")
		}
		total += f.Size
		if total > maxNotebookBytes {
			return errors.New("Notebook snapshot exceeds limit")
		}
		switch f.Path {
		case "bot.json", "notebook-migration.json", "bot-initialization.json":
		default:
			rel := strings.TrimPrefix(f.Path, "Notebook/")
			if rel == f.Path || rel == "INDEX.md" || rel == notebook.HandoffName || !strings.EqualFold(path.Ext(rel), ".md") && !attachments[rel] {
				return errors.New("file outside Notebook snapshot allowlist")
			}
		}
		if strings.EqualFold(path.Ext(f.Path), ".md") && !utf8.Valid(f.Body) {
			return errors.New("Notebook Markdown must be UTF-8")
		}
		if f.Path == "Notebook/MEMORY.md" && len(f.Body) > 128<<10 {
			return errors.New("Notebook core memory exceeds new-session context limit")
		}
		files[f.Path] = f.Body
	}
	var id Identity
	if decode(files["bot.json"], &id, true) != nil || id.Version != 1 || id.PersonalVersion != 1 || id.ID != s.BotID || id.Schedules == nil || len(id.Schedules) != 0 {
		return errors.New("Notebook snapshot Bot identity mismatch")
	}
	var migration struct {
		Version int `json:"version"`
	}
	if decode(files["notebook-migration.json"], &migration, true) != nil || migration.Version != 1 {
		return errors.New("Notebook migration barrier is required")
	}
	if b, exists := files["bot-initialization.json"]; exists {
		var intro struct {
			Version int    `json:"version"`
			ID      string `json:"id"`
			Status  string `json:"status"`
		}
		if decode(b, &intro, true) != nil || intro.Version != 1 || intro.Status != "accepted" || !validSnapshotToken(intro.ID) {
			return errors.New("Notebook initialization must be accepted and portable")
		}
	}
	for p := range attachments {
		if _, ok := files["Notebook/"+p]; !ok {
			return errors.New("missing allowlisted attachment")
		}
	}
	return checkReferences(files, s.Attachments)
}

// rejectRedirectedPath rejects symlinks in every existing ancestor, including
// the receiver parent. The host still owns exclusion of concurrent filesystem
// writers while applying; this is not an isolation boundary from the OS user.
func rejectRedirectedPath(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return errors.New("use a clean absolute snapshot path")
	}
	for current := p; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("snapshot path cannot be redirected")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func notebookSource(ctx context.Context, in NotebookExportOptions) (NotebookSnapshot, error) {
	s := NotebookSnapshot{Format: NotebookFormat, Epoch: in.Epoch, Version: in.Version, Attachments: append([]string{}, in.Attachments...)}
	if err := rejectRedirectedPath(in.Source); err != nil {
		return s, err
	}
	if err := requireDirectory(in.Source, false); err != nil {
		return s, err
	}
	root, err := os.OpenRoot(in.Source)
	if err != nil {
		return s, err
	}
	defer root.Close()
	var consumed int64
	read := func(p string) ([]byte, error) {
		info, err := root.Lstat(p)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > maxNotebookFile {
			return nil, errors.New("Notebook source must be bounded regular files")
		}
		// Root confines path resolution; walking Notebook below rejects internal symlinks.
		f, err := root.Open(p)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		after, err := f.Stat()
		if err != nil || !os.SameFile(info, after) {
			return nil, errors.New("Notebook source changed while opening")
		}
		b, err := io.ReadAll(io.LimitReader(f, maxNotebookFile+1))
		consumed += int64(len(b))
		if len(b) > maxNotebookFile || consumed > maxNotebookBytes {
			return nil, errors.New("Notebook source exceeds limit")
		}
		return b, err
	}
	load := func(p string, value any) error {
		b, err := read(p)
		if err != nil {
			return err
		}
		if len(b) > 2<<20 {
			return errors.New("snapshot state exceeds limit")
		}
		return decode(b, value, false)
	}
	var state struct {
		Version         int    `json:"version"`
		PersonalVersion int    `json:"personalVersion"`
		ID              string `json:"id"`
		Wake            *struct {
			Status string `json:"status"`
		} `json:"wake"`
	}
	if err = load("bot.json", &state); err != nil {
		return s, err
	}
	if state.Version != 1 || state.PersonalVersion < 0 || state.PersonalVersion > 1 || !validSnapshotToken(state.ID) || state.Wake != nil && state.Wake.Status != "accepted" {
		return s, errors.New("resolve source identity and pending wake before Notebook export")
	}
	s.BotID = state.ID
	add := func(p string, body []byte) { s.Files = append(s.Files, NotebookFile{File: File{Path: p}, Body: body}) }
	b, _ := jsonBytes(Identity{Version: 1, PersonalVersion: 1, ID: s.BotID, Schedules: []struct{}{}})
	add("bot.json", b)
	marker := struct {
		Version int `json:"version"`
	}{1}
	if _, e := root.Lstat("notebook-migration.json"); e == nil {
		if load("notebook-migration.json", &marker) != nil || marker.Version != 1 {
			return s, errors.New("unsupported Notebook migration marker")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return s, e
	}
	b, _ = jsonBytes(marker)
	add("notebook-migration.json", b)
	var intro struct {
		Version int    `json:"version"`
		ID      string `json:"id"`
		Status  string `json:"status"`
	}
	if _, e := root.Lstat("bot-initialization.json"); e == nil {
		if load("bot-initialization.json", &intro) != nil || intro.Version != 1 {
			return s, errors.New("unsupported initialization marker")
		}
		if intro.Status == "accepted" && validSnapshotToken(intro.ID) {
			b, _ = jsonBytes(intro)
			add("bot-initialization.json", b)
		} else if intro.Status != "required" {
			return s, errors.New("reconcile source introduction before Notebook export")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return s, e
	}
	count := 0
	err = fs.WalkDir(root.FS(), "Notebook", func(p string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > maxNotebookEntries {
			return errors.New("too many Notebook entries")
		}
		if e.Type()&os.ModeSymlink != 0 || !e.IsDir() && !e.Type().IsRegular() {
			return errors.New("Notebook cannot contain redirected or special files")
		}
		if e.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, "Notebook/")
		if rel == "INDEX.md" || rel == notebook.HandoffName || !strings.EqualFold(path.Ext(rel), ".md") {
			return nil
		}
		if !safePath(p) {
			return errors.New("unsafe Notebook path")
		}
		b, err := read(p)
		if err != nil {
			return err
		}
		add(p, b)
		return nil
	})
	if err != nil {
		return s, err
	}
	for _, rel := range in.Attachments {
		if !allowedAttachment(rel) {
			return s, errors.New("unsafe Notebook attachment")
		}
		b, err := read("Notebook/" + rel)
		if err != nil {
			return s, err
		}
		add("Notebook/"+rel, b)
	}
	return s, nil
}

// ExportNotebook takes no credentials and never opens the source Memory DB.
// A second read detects persistent direct-writer changes; the host's write gate
// is mandatory because a write-and-revert cannot be detected by a codec.
func ExportNotebook(ctx context.Context, in NotebookExportOptions) ([]byte, nodeplane.SnapshotRef, error) {
	if !in.SourceStopped {
		return nil, nodeplane.SnapshotRef{}, errors.New("gate all source writers before Notebook export")
	}
	s, err := notebookSource(ctx, in)
	if err != nil {
		return nil, nodeplane.SnapshotRef{}, err
	}
	payload, ref, err := EncodeNotebook(ctx, s)
	if err != nil {
		return nil, ref, err
	}
	after, err := notebookSource(ctx, in)
	if err != nil {
		return nil, nodeplane.SnapshotRef{}, err
	}
	if !reflect.DeepEqual(s, after) {
		return nil, nodeplane.SnapshotRef{}, errors.New("Notebook changed during export")
	}
	return payload, ref, nil
}

// ApplyNotebook initializes a fresh inactive profile. Old profile generations
// remain available to the local owner, but are never read or merged. The final
// locked CAS must recheck the latest descriptor after all staging work.
func ApplyNotebook(ctx context.Context, in NotebookApplyOptions) (out Result, err error) {
	if !in.DestinationStopped || in.Commit == nil {
		return out, errors.New("stopped target and latest-snapshot commit gate are required")
	}
	s, ref, err := decodeNotebook(ctx, in.Payload)
	if err != nil {
		return out, err
	}
	if ref != in.Expected {
		return out, errors.New("Notebook snapshot differs from expected latest descriptor")
	}
	if err = rejectRedirectedPath(in.Destination); err != nil {
		return out, err
	}
	stage, release, err := reserve(in.Destination, ".notebook-import-")
	if err != nil {
		return out, err
	}
	defer release()
	out.BotID, out.Path = ref.BotID, stage
	for _, f := range s.Files {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		if err = writeFile(stage, f.Path, f.Body); err != nil {
			return out, err
		}
	}
	if err = writeJSON(stage, "notebook-snapshot.json", notebookInstallation{Format: NotebookFormat, Version: 1, Snapshot: ref}); err != nil {
		return out, err
	}
	store, err := botmemory.Open(ctx, filepath.Join(stage, "personal"), ref.BotID)
	if err != nil {
		return out, err
	}
	// Empty evidence enumeration validates the fresh receipt index without
	// spending the public recall query's one-second search budget during cold
	// appliance initialization. Normal keyword recall is exercised separately.
	view, readErr := store.ReadMemory(ctx, "")
	err = errors.Join(readErr, store.Close())
	if err != nil {
		return out, err
	}
	if len(view.Evidence) != 0 {
		return out, errors.New("fresh Notebook profile Memory must be empty")
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
	installed, attempted := false, false
	err = in.Commit(ctx, ref, func() error {
		if attempted {
			return errors.New("snapshot install may only run once")
		}
		attempted = true
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := activate(stage, in.Destination); err != nil {
			return err
		}
		installed = true
		return nil
	})
	if installed {
		out.Path = in.Destination
	}
	if err != nil {
		return out, err
	}
	if !installed {
		return out, fmt.Errorf("latest-snapshot gate did not install the profile")
	}
	out.Path, out.Activated = in.Destination, true
	return out, nil
}
