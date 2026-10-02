package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// NotebookOwnerRequest is native-only preparation on an already enrolled SSH
// node. Runtime paths are read from its management owner, never renderer input.
type NotebookOwnerRequest struct {
	Action      string                `json:"action"`
	BotID       string                `json:"botId"`
	OperationID string                `json:"operationId,omitempty"`
	Runtime     *OwnedRuntimeSettings `json:"runtime,omitempty"`
}
type NotebookOwnerState struct {
	StopResult *productrpc.Result  `json:"stopResult,omitempty"`
	Stopped    bool                `json:"stopped"`
	Handoff    []byte              `json:"handoff,omitempty"`
	Endpoint   string              `json:"endpoint,omitempty"`
	Identity   productrpc.Identity `json:"identity"`
}

func NotebookOwnerProfile(directory string) string { return filepath.Join(directory, "notebook-bot") }

// NotebookOwner uses the same sanitized SSH pairing as ordinary file backup.
// It returns only stop proof and nonsecret product pairing metadata.
func NotebookOwner(ctx context.Context, ssh SSHConfig, helper, directory, node string, request NotebookOwnerRequest) (NotebookOwnerState, error) {
	if !cleanOwnedRuntimePath(helper) || !cleanOwnedRuntimePath(directory) || !identifier.MatchString(node) {
		return NotebookOwnerState{}, errors.New("existing enrolled Notebook owner required")
	}
	args, close, err := NotebookSSH(ctx, ssh)
	if err != nil {
		return NotebookOwnerState{}, err
	}
	defer close()
	args = append(args, "--", ssh.Target, shellQuote(helper)+" notebook-owner --directory "+shellQuote(directory)+" --node-id "+shellQuote(node))
	body, err := json.Marshal(request)
	if err != nil {
		return NotebookOwnerState{}, err
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin, cmd.Stderr = bytes.NewReader(body), io.Discard
	var output deploymentOutput
	cmd.Stdout = &output
	if cmd.Run() != nil {
		return NotebookOwnerState{}, errors.New("Notebook owner action unconfirmed; inspect the existing node owner")
	}
	var state NotebookOwnerState
	d := json.NewDecoder(bytes.NewReader(output.Bytes()))
	d.DisallowUnknownFields()
	if d.Decode(&state) != nil || d.Decode(new(any)) != io.EOF || state.Identity.NodeID != node || state.Identity.BotID != productrpc.ProfileBotID(request.BotID) {
		return NotebookOwnerState{}, errors.New("Notebook owner identity differs")
	}

	if state.StopResult != nil {
		result := state.StopResult
		if request.Action != "stop" || result.ID != request.OperationID || state.Stopped || result.Outcome != "rejected" || result.Code != productrpc.StopNotDispatchedCode {
			return NotebookOwnerState{}, errors.New("Notebook stop rejection does not match original request")
		}
		return state, api.ErrStopNotDispatched
	}
	return state, nil
}

// Keep Runtime backend scope explicit even for native callers.
func ValidateNotebookOwnerRequest(r NotebookOwnerRequest) error {
	if r.BotID == "" || len(r.BotID) > 128 {
		return errors.New("portable Bot identity required")
	}
	switch r.Action {
	case "prepare":
		if r.Runtime == nil || validateOwnedRuntimeSettings(*r.Runtime, r.Runtime.Backend) != nil {
			return errors.New("target-local Runtime designation required")
		}
	case "status", "stop", "handoff":
		if r.Runtime != nil {
			return errors.New("owner observation cannot reconfigure Runtime")
		}
	case "start":
		if r.Runtime != nil || !identifier.MatchString(r.OperationID) {
			return errors.New("original fresh-start operation required")
		}
	default:
		return errors.New("unsupported Notebook owner action")
	}
	if r.Action == "stop" && !identifier.MatchString(r.OperationID) {
		return errors.New("original stop operation required")
	}
	return nil
}
