package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
)

// This receipt belongs only to this APP profile. It admits a normal fresh local
// startup after the original remote stop/copy, never a stop/start retry.
type notebookLocalReturn struct {
	OperationID  string `json:"operationId"`
	SourceNodeID string `json:"sourceNodeId"`
	BotID        string `json:"botId"`
}
type notebookLocalStop struct {
	BotID   string `json:"botId"`
	Stopped bool   `json:"stopped"`
}

func readNotebookPrivate(path string, out any) error { return ReadNotebookOwnerRecord(path, out) }

// ReadNotebookOwnerRecord shares private metadata validation with the node helper.
// Bot state contains schedules and has no 64 KiB producer limit.
func ReadNotebookOwnerRecord(path string, out any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	_, botState := out.(*bot.State)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !botState && info.Size() > 64<<10 {
		return errors.New("private Notebook owner record unavailable")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("extra Notebook owner record data")
	}
	return nil
}

// ReadNotebookRuntimeSettings retains Notebook's private-file and strict JSON
// checks while sharing the normal setup/backend persisted schema and validation.
func ReadNotebookRuntimeSettings(profile string) (api.RuntimeSettings, error) {
	var doc backend.RuntimeDocument
	if err := readNotebookPrivate(filepath.Join(profile, "runtime.json"), &doc); err != nil {
		return api.RuntimeSettings{}, err
	}
	if err := doc.Validate(); err != nil {
		return api.RuntimeSettings{}, err
	}
	return doc.RuntimeSettings, nil
}

// Refuse traversal through links or special native state before moving files.
func notebookNativeParents(profile, name string) error {
	for directory := filepath.Dir(filepath.Join(profile, name)); ; directory = filepath.Dir(directory) {
		info, err := os.Lstat(directory)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Notebook native state parent unsafe; originals preserved")
		}
		if directory == profile {
			return nil
		}
		if directory == filepath.Dir(directory) {
			return errors.New("Notebook state outside profile")
		}
	}
}

// PrepareFreshNotebookProfile retains the target's native bindings locally.
// Runtime configuration/authentication and Notebook files are never moved or
// copied here. Both native serve-bot and the ordinary desktop return use it.
func PrepareFreshNotebookProfile(profile, operation string) error {
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(operation) {
		return errors.New("original fresh-start operation required")
	}
	var identity bot.State
	if err := readNotebookPrivate(filepath.Join(profile, "bot.json"), &identity); err != nil {
		return err
	}
	archive := filepath.Join(profile, "Product", "retired-"+operation)
	if err := os.MkdirAll(filepath.Dir(archive), 0700); err != nil {
		return err
	}
	if err := notebookNativeParents(profile, "Product/archive"); err != nil {
		return err
	}
	if err := os.Mkdir(archive, 0700); err != nil {
		return err
	}
	for _, name := range []string{"conversation.json", "providers/caelis/application.json", "providers/caelis/conversation.json", "personal", "tasks.json", "dream-codex.json", "dream-caelis.json", "care.json", "preview.json", "draft.json", "Product/receipts.json", "Product/completed-handoff.json"} {
		original := filepath.Join(profile, name)
		info, err := os.Lstat(original)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := notebookNativeParents(profile, name); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
			return errors.New("target native state unsafe; originals preserved")
		}
		target := filepath.Join(archive, name)
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if err = os.Rename(original, target); err != nil {
			return err
		}
	}
	if err := localstate.Write(filepath.Join(archive, "bot.json"), identity); err != nil {
		return err
	}
	return localstate.Write(filepath.Join(profile, "bot.json"), bot.State{Version: 1, PersonalVersion: 1, ID: identity.ID, Schedules: []bot.Schedule{}})
}
func (c *defaultNotebookSync) localStandby(botID string) error {
	if c.app.product == nil {
		return errors.New("local APP is already the active owner")
	}
	var proof notebookLocalStop
	if err := readNotebookPrivate(filepath.Join(c.app.root, "nodeplane", "notebook-local-owner.json"), &proof); err != nil {
		return err
	}
	if !proof.Stopped || proof.BotID != botID {
		return errors.New("original local owner stop is unconfirmed")
	}
	return notebookProfileStopped(c.app.root)
}
func (c *defaultNotebookSync) preserveLocalNotebook(botID, operation string) error {
	if err := c.localStandby(botID); err != nil {
		return err
	}
	// Preserve the entire previous Notebook, including files absent on the source.
	// An existing archive is a pending original operation, never permission to retry.
	archive := filepath.Join(c.app.root, "Product", "notebook-before-return-"+operation)
	if err := os.MkdirAll(filepath.Dir(archive), 0700); err != nil {
		return err
	}
	if err := notebookNativeParents(c.app.root, "Product/archive"); err != nil {
		return err
	}
	if err := os.Mkdir(archive, 0700); err != nil {
		return err
	}
	original := filepath.Join(c.app.root, "Notebook")
	info, err := os.Lstat(original)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("local Notebook unavailable; original preserved")
	}
	if err = os.Rename(original, filepath.Join(archive, "Notebook")); err != nil {
		return err
	}
	return os.Mkdir(original, 0700)
}
func (c *defaultNotebookSync) prepareLocalReturn(botID, operation string) error {
	if err := c.localStandby(botID); err != nil {
		return err
	}
	if err := PrepareFreshNotebookProfile(c.app.root, operation); err != nil {
		return err
	}
	receipt := notebookLocalReturn{OperationID: operation, SourceNodeID: c.settings.SourceNodeID, BotID: botID}
	if err := localstate.Write(filepath.Join(c.app.root, "nodeplane", "notebook-local-return.json"), receipt); err != nil {
		return err
	}
	if _, err := c.app.Backend.SaveProductPairing(backend.ProductPairing{Mode: "local"}, c.app.Backend.ProductConnection().Revision); err != nil {
		return err
	}
	return notebooksync.ErrRestartRequired
}
func (c *defaultNotebookSync) validateReturn(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.validateReturnLocked(ctx)
}
func (c *defaultNotebookSync) validateReturnLocked(ctx context.Context) error {
	if c.returning == nil {
		return nil
	}
	var identity bot.State
	if err := readNotebookPrivate(filepath.Join(c.app.root, "bot.json"), &identity); err != nil {
		return err
	}
	if identity.ID != c.returning.BotID || c.returning.SourceNodeID == api.LocalNodeID {
		return errors.New("local return identity changed")
	}
	doc, err := loadNodeManagementDocument(filepath.Join(c.app.root, "nodeplane", "config.json"))
	if err != nil {
		return err
	}
	for _, r := range doc.Nodes {
		if r.ID == c.returning.SourceNodeID && r.Join == api.NodeSSH {
			state, err := c.ownerCall(ctx, r, nodeagent.NotebookOwnerRequest{Action: "status", BotID: identity.ID})
			if err != nil {
				return err
			}
			if !state.Stopped {
				return errors.New("remote owner stop unconfirmed; local startup blocked")
			}
			return nil
		}
	}
	return errors.New("original remote owner unavailable; local startup blocked")
}

// rotate follows a confirmed Bot move. The old source becomes a stopped backup
// with its original Runtime; no user needs to reconstruct the sync direction.
func (c *defaultNotebookSync) rotate(id string) error {
	if c.settings.SourceNodeID != id {
		old := backend.NotebookBackupTarget{NodeID: c.settings.SourceNodeID, Backend: c.settings.SourceBackend}
		var next api.NodeBackend
		targets := []backend.NotebookBackupTarget{}
		for _, t := range c.settings.Targets {
			if t.NodeID == id {
				next = t.Backend
			} else {
				targets = append(targets, t)
			}
		}
		if next == "" || old.Backend == "" {
			return errors.New("source Runtime designation unavailable")
		}
		targets = append(targets, old)
		updated := c.settings
		updated.SourceNodeID = id
		updated.SourceBackend = next
		updated.Targets = targets
		if err := localstate.Write(filepath.Join(c.app.root, "nodeplane", "notebook-settings.json"), updated); err != nil {
			return err
		}
		c.settings = updated
	}
	return nil
}
func (c *defaultNotebookSync) activateSource(id string) error {
	if err := c.rotate(id); err != nil {
		return err
	}
	c.state = notebooksync.State{SourceNodeID: id, Targets: []notebooksync.Status{}}
	for _, t := range c.settings.Targets {
		c.state.Targets = append(c.state.Targets, notebooksync.Status{NodeID: t.NodeID, Phase: "ready"})
	}
	sort.Slice(c.state.Targets, func(i, j int) bool { return c.state.Targets[i].NodeID < c.state.Targets[j].NodeID })
	if err := localstate.Write(filepath.Join(c.app.root, "nodeplane", "notebook-sync.json"), c.state); err != nil {
		return err
	}
	c.app.mu.Lock()
	c.app.notebookSyncRecovery = false
	c.app.mu.Unlock()
	if err := c.assemble(context.Background(), false); err != nil {
		return err
	}
	c.app.Backend.ConfigureNotebookSync(c)
	return nil
}
func (c *defaultNotebookSync) observeReturn(ctx context.Context, snapshot api.Snapshot) error {
	if snapshot.Connection != "ready" {
		return nil
	}
	c.app.mu.Lock()
	active := c.app.started && !c.app.closed && c.app.product == nil
	c.app.mu.Unlock()
	if !active {
		return errors.New("local Runtime is not the active owner")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.returning == nil {
		return nil
	}
	if err := c.validateReturnLocked(ctx); err != nil {
		return err
	}
	if err := localstate.Write(filepath.Join(c.app.root, "nodeplane", "notebook-local-owner.json"), notebookLocalStop{BotID: c.returning.BotID}); err != nil {
		return err
	}
	if err := c.activateSource(api.LocalNodeID); err != nil {
		return err
	}
	c.returning = nil
	c.app.mu.Lock()
	if c.app.notebookReturn == c {
		c.app.notebookReturn = nil
	}
	c.app.mu.Unlock()
	c.app.startNotebookSync()
	return nil
}
