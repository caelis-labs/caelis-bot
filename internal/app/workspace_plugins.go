package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/botworkspace"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const workspacePluginLimit = 4096

type workspacePluginVersionKey struct{}

type workspacePluginStatus struct {
	botworkspace.PluginRequest
	State    string `json:"state"`
	Revision uint64 `json:"revision,omitempty"`
}

type workspacePluginReceipt struct {
	workspacePluginStatus
	Digest string `json:"digest"`
}

// consumeWorkspacePluginRequest runs outside the turn and app lifecycle locks.
// The workspace status is derived. Only the private receipt may govern replay.
func (a *Application) consumeWorkspacePluginRequest(ctx context.Context) error {
	a.workspacePluginMu.Lock()
	defer a.workspacePluginMu.Unlock()
	root, err := openWorkspacePluginDirectory(a.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("workspace plugin directory unavailable")
	}
	defer root.Close()
	body, original, err := readWorkspacePluginFile(root, botworkspace.PluginRequestName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		if errors.Is(err, errInvalidWorkspaceFile) {
			return quarantinePluginRequest(root)
		}
		return errors.New("workspace plugin request unreadable")
	}
	var req botworkspace.PluginRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || !req.HasValidID() {
		return quarantinePluginRequest(root)
	}
	if a.plugins == nil || ctx.Err() != nil {
		return errors.New("workspace plugin request deferred")
	}
	canonical, _ := json.Marshal(req)
	digest := sha256.Sum256(canonical)
	id := sha256.Sum256([]byte(req.ID))
	receiptDir := filepath.Join(a.plugins.Root(), "workspace-requests")
	if err := localstate.EnsurePrivateDirectory(receiptDir); err != nil {
		return errors.New("workspace plugin receipt storage unavailable")
	}
	path := filepath.Join(receiptDir, hex.EncodeToString(id[:])+".json")
	valid := req.Validate() == nil
	status := workspacePluginStatus{PluginRequest: botworkspace.PluginRequest{ID: req.ID}, State: "unknown"}
	if valid {
		status.PluginRequest = req
	}
	receipt := workspacePluginReceipt{workspacePluginStatus: status, Digest: hex.EncodeToString(digest[:])}
	claimed, err := claimWorkspacePluginReceipt(path, receipt)
	if err == nil && claimed {
		receipt.State = "rejected"
		if valid {
			snapshot, actionErr := a.PluginAction(context.WithValue(ctx, workspacePluginVersionKey{}, req.Version), req.PluginID, req.Action)
			var writeErr *localstate.WriteError
			switch {
			case actionErr == nil:
				receipt.State, receipt.Revision = "saved", snapshot.Revision
			case errors.As(actionErr, &writeErr):
				receipt.State = "unknown"
			}
		}
		if receipt.State != "unknown" {
			err = localstate.WriteConfirmed(path, receipt)
		}
	} else if err == nil {
		receipt = workspacePluginReceipt{}
		err = readWorkspacePluginReceipt(path, &receipt)
		if err == nil && receipt.ID == "" {
			// Older claims stored only the digest and outcome.
			receipt.ID = req.ID
			if receipt.Digest == hex.EncodeToString(digest[:]) {
				receipt.PluginRequest = status.PluginRequest
			}
		}
	}
	if err != nil {
		return errors.New("workspace plugin receipt unavailable")
	}
	// An ID always reports its original action and outcome. Edited requests
	// cannot replace either the private receipt or its workspace projection.
	if receipt.ID != req.ID {
		return errors.New("workspace plugin receipt identity mismatch")
	}
	if err := publishWorkspacePluginStatus(root, receipt.workspacePluginStatus); err != nil {
		return errors.New("workspace plugin status unavailable")
	}
	// The writer publishes with exclusive creation and leaves the occupied
	// slot alone. Still check identity and bytes before removing our request.
	current, info, err := readWorkspacePluginFile(root, botworkspace.PluginRequestName)
	if err == nil && os.SameFile(original, info) && bytes.Equal(body, current) {
		err = root.Remove(botworkspace.PluginRequestName)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("workspace plugin request cleanup unavailable")
	}
	return nil
}

func openWorkspacePluginDirectory(appRoot string) (*os.Root, error) {
	path := filepath.Join(appRoot, "Notebook")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("invalid Notebook directory")
	}
	notes, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer notes.Close()
	info, err = notes.Lstat(".agents")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("invalid workspace plugin directory")
	}
	return notes.OpenRoot(".agents")
}

var errInvalidWorkspaceFile = errors.New("invalid workspace plugin file")

// Preserve malformed submissions without opening redirected paths or applying an action.
func quarantinePluginRequest(root *os.Root) error {
	name := ".bot-plugin-rejected-" + rand.Text()
	if err := root.Rename(botworkspace.PluginRequestName, name); err != nil {
		return err
	}
	return errors.Join(errors.New("invalid workspace plugin request retained"), localstate.SyncParent(filepath.Join(root.Name(), name)))
}

func readWorkspacePluginFile(root *os.Root, name string) ([]byte, os.FileInfo, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > workspacePluginLimit {
		return nil, nil, errInvalidWorkspaceFile
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) {
		return nil, nil, errors.New("workspace plugin file changed")
	}
	body, err := io.ReadAll(io.LimitReader(file, workspacePluginLimit+1))
	if err != nil || len(body) > workspacePluginLimit {
		return nil, nil, errInvalidWorkspaceFile
	}
	return body, info, nil
}

func claimWorkspacePluginReceipt(path string, receipt workspacePluginReceipt) (bool, error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".claim-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	err = localstate.ProtectOpenFile(file)
	if err == nil {
		err = json.NewEncoder(file).Encode(receipt)
	}
	if err == nil {
		err = file.Sync()
	}
	if err = errors.Join(err, file.Close()); err != nil {
		return false, err
	}
	// Publish a complete claim exclusively. A crash cannot leave a partial
	// receipt occupying the request slot.
	if err := os.Link(file.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, localstate.SyncParent(path)
		}
		return false, err
	}
	return true, localstate.SyncParent(path)
}

func readWorkspacePluginReceipt(path string, receipt *workspacePluginReceipt) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if json.Unmarshal(body, receipt) != nil || len(receipt.Digest) != sha256.Size*2 {
		return errors.New("invalid plugin receipt")
	}
	switch receipt.State {
	case "unknown", "rejected":
		if receipt.Revision == 0 {
			return nil
		}
	case "saved":
		if receipt.Revision != 0 {
			return nil
		}
	}
	return errors.New("invalid plugin receipt status")
}

func publishWorkspacePluginStatus(root *os.Root, status workspacePluginStatus) error {
	name := ".bot-plugin-status-" + rand.Text()
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	err = json.NewEncoder(file).Encode(status)
	if err == nil {
		err = file.Sync()
	}
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	if err := root.Rename(name, status.StatusName()); err != nil {
		return err
	}
	return localstate.SyncParent(filepath.Join(root.Name(), status.StatusName()))
}
