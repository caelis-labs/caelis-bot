package nodeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// This native-only port is carried by the already paired NodeAgent channel.
// No renderer DTO, arbitrary executable, shell or credential field exists.
type OutgoingRoute struct{ Target, Helper, Directory string }
type RoamingDeploymentMetadata struct {
	NodeID, Directory, Helper, HelperSHA256, Architecture, OS string
	Route                                                     OutgoingRoute
}
type RoamingDeploymentRequest struct {
	NodeID             string                 `json:"nodeId"`
	Action             string                 `json:"action"`
	OperationID        string                 `json:"operationId,omitempty"`
	PlanID             string                 `json:"planId,omitempty"`
	DisableOperationID string                 `json:"disableOperationId,omitempty"`
	State              string                 `json:"state,omitempty"`
	Plan               json.RawMessage        `json:"plan,omitempty"`
	Workers            json.RawMessage        `json:"workers,omitempty"`
	Preferences        *ExecutionPreferences  `json:"preferences,omitempty"`
	Companion          *RoamingCompanionChunk `json:"companion,omitempty"`
}
type RoamingDeploymentReceipt struct {
	NodeID, OperationID, PlanID, DisableOperationID, State, Outcome string
	Metadata                                                        *RoamingDeploymentMetadata `json:"metadata,omitempty"`
}
type RoamingDeploymentPort interface {
	RoamingDeployment(context.Context, RoamingDeploymentRequest) (RoamingDeploymentReceipt, error)
}

func ValidateRoamingDeploymentRequest(r RoamingDeploymentRequest) error {
	if !identifier.MatchString(r.NodeID) {
		return errors.New("exact deployment node required")
	}
	if r.Action == "metadata" {
		if r.OperationID != "" || r.PlanID != "" || r.DisableOperationID != "" || r.State != "" || len(r.Plan)+len(r.Workers) != 0 || r.Preferences != nil || r.Companion != nil {
			return errors.New("metadata is read-only")
		}
		return nil
	}
	if r.Action != "helper" && r.Companion != nil {
		return errors.New("unexpected helper transfer")
	}
	if !identifier.MatchString(r.OperationID) {
		return errors.New("original deployment operation required")
	}
	hash, e := hex.DecodeString(r.PlanID)
	if e != nil || len(hash) != 32 {
		return errors.New("exact reviewed deployment required")
	}
	switch r.Action {
	case "helper":
		if r.Companion == nil || len(r.Plan)+len(r.Workers) != 0 || r.Preferences != nil || r.DisableOperationID != "" || r.State != "" {
			return errors.New("fixed reviewed companion chunk required")
		}
	case "preflight", "prepare":
		if len(r.Plan) == 0 || r.Preferences == nil || len(r.Workers) == 0 || r.DisableOperationID != "" || r.State != "" {
			return errors.New("closed deployment documents required")
		}
	case "start":
		if len(r.Plan)+len(r.Workers) != 0 || r.Preferences != nil || r.DisableOperationID != "" || r.State != "" {
			return errors.New("start accepts original identity only")
		}
	case "control", "receipt":
		if len(r.Plan)+len(r.Workers) != 0 || r.Preferences != nil || !identifier.MatchString(r.DisableOperationID) || r.Action == "receipt" && r.State != "" || r.Action == "control" && r.State != "disabling" && r.State != "disabled" {
			return errors.New("closed original disable intent required")
		}
	default:
		return errors.New("closed deployment action required")
	}
	return nil
}

// ReadNativeCompanionMetadata inspects only the fixed packaged native helper.
// It never reads Runtime credentials or establishes outward SSH authority.
func ReadNativeCompanionMetadata(directory, nodeID string) (RoamingDeploymentMetadata, error) {
	value := RoamingDeploymentMetadata{NodeID: nodeID, Directory: directory, Architecture: runtime.GOARCH, OS: runtime.GOOS}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" || CheckPrivateDirectory(directory) != nil {
		return value, errors.New("native process ownership unsupported")
	}
	var identity struct {
		ID string `json:"id"`
	}
	if e := readPrivateJSON(filepath.Join(directory, "node.json"), &identity); e != nil || identity.ID != nodeID {
		return value, errors.New("existing native enrollment required")
	}
	executable, e := os.Executable()
	if e != nil {
		return value, e
	}
	value.Helper = filepath.Join(filepath.Dir(executable), "caelis-node")
	if filepath.Base(executable) == "caelis-node" {
		value.Helper = executable
	}
	info, e := os.Lstat(value.Helper)
	if errors.Is(e, os.ErrNotExist) {
		value.Helper = filepath.Join(directory, "caelis-node")
		info, e = os.Lstat(value.Helper)
	}
	if !errors.Is(e, os.ErrNotExist) && (e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 || info.Size() <= 0 || info.Size() > 256<<20) {
		return value, errors.New("verified installed companion unavailable")
	}
	if e == nil {
		f, e := os.Open(value.Helper)
		if e != nil {
			return value, e
		}
		defer f.Close()
		opened, e := f.Stat()
		if e != nil || !os.SameFile(info, opened) {
			return value, errors.New("companion changed")
		}
		h := sha256.New()
		if _, e = io.Copy(h, f); e != nil {
			return value, e
		}
		value.HelperSHA256 = hex.EncodeToString(h.Sum(nil))
	}
	return value, nil
}

// ReadRoamingDeploymentMetadata additionally validates the existing outward
// pairing; helper discovery is shared with approved native readiness actions.
func ReadRoamingDeploymentMetadata(directory, nodeID string) (RoamingDeploymentMetadata, error) {
	value, e := ReadNativeCompanionMetadata(directory, nodeID)
	if e != nil {
		return value, e
	}
	if e = readPrivateJSON(filepath.Join(directory, "outgoing-route.json"), &value.Route); e != nil {
		return value, errors.New("existing outbound pairing metadata unavailable")
	}
	if _, e = JoinArgs(SSHConfig{Target: value.Route.Target}, value.Route.Helper, value.Route.Directory, filepath.Join(directory, "agent.sock")); e != nil {
		return value, e
	}
	return value, nil
}

func (s *Service) RoamingDeployment(ctx context.Context, r RoamingDeploymentRequest) (RoamingDeploymentReceipt, error) {
	result := RoamingDeploymentReceipt{NodeID: r.NodeID, OperationID: r.OperationID, PlanID: r.PlanID, DisableOperationID: r.DisableOperationID, Outcome: "unknown"}
	if r.NodeID != s.options.NodeID || s.options.Join != "outgoing" || ValidateRoamingDeploymentRequest(r) != nil {
		return result, errors.New("exact outgoing deployment scope required")
	}
	metadata, e := ReadRoamingDeploymentMetadata(s.options.Directory, s.options.NodeID)
	if e != nil {
		return result, e
	}
	if r.Action == "helper" {
		return s.stageRoamingCompanion(ctx, r, metadata)
	}
	if r.Action == "metadata" {
		result.Metadata = &metadata
		result.Outcome = "observed"
		return result, nil
	}
	if r.Action == "preflight" || r.Action == "prepare" {
		if e = s.validateDeploymentRuntime(ctx, r); e != nil {
			return result, e
		}
	}
	if r.Action == "preflight" {
		if e = s.preflightJoinedDeployment(ctx, r, metadata); e != nil {
			return result, e
		}
		result.Outcome = "accepted"
		return result, nil
	}
	if metadata.HelperSHA256 == "" {
		return result, errors.New("reviewed native companion must be staged first")
	}
	// The companion parses the closed plan and reuses production supervisor
	// validation, intent locks and durable records. Caller cannot select a path.
	body, e := json.Marshal(r)
	if e != nil {
		return result, e
	}
	command := exec.CommandContext(ctx, metadata.Helper, "deploy-joined-roaming", "--directory", s.options.Directory, "--node-id", s.options.NodeID)
	command.Stdin = bytes.NewReader(body)
	command.Stderr = io.Discard
	var output deploymentOutput
	command.Stdout = &output
	if e = command.Run(); e != nil {
		return result, errors.New("original deployment outcome unconfirmed")
	}
	d := json.NewDecoder(bytes.NewReader(output.Bytes()))
	d.DisallowUnknownFields()
	if d.Decode(&result) != nil || d.Decode(new(any)) != io.EOF || result.NodeID != r.NodeID || result.OperationID != r.OperationID || result.PlanID != r.PlanID || result.DisableOperationID != r.DisableOperationID {
		return RoamingDeploymentReceipt{}, errors.New("original deployment receipt scope changed")
	}
	return result, nil
}

type deploymentOutput struct{ bytes.Buffer }

func (b *deploymentOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 16<<10 {
		return 0, errors.New("deployment receipt limit")
	}
	return b.Buffer.Write(p)
}
func (c *Client) RoamingDeployment(ctx context.Context, r RoamingDeploymentRequest) (RoamingDeploymentReceipt, error) {
	result := RoamingDeploymentReceipt{NodeID: r.NodeID, OperationID: r.OperationID, PlanID: r.PlanID, DisableOperationID: r.DisableOperationID, Outcome: "unknown"}
	if r.NodeID != c.expected || ValidateRoamingDeploymentRequest(r) != nil {
		return result, errors.New("exact deployment scope required")
	}
	if e := c.request(ctx, "POST", "/v1/node/roaming-deployment", r, &result); e != nil {
		return result, e
	}
	if r.Action == "control" && result.State != r.State {
		return RoamingDeploymentReceipt{}, errors.New("original deployment intent receipt changed")
	}
	if result.NodeID != r.NodeID || result.OperationID != r.OperationID || result.PlanID != r.PlanID || result.DisableOperationID != r.DisableOperationID || r.Action == "metadata" && (result.Metadata == nil || result.Metadata.NodeID != r.NodeID || !cleanOwnedRuntimePath(result.Metadata.Directory) || !cleanOwnedRuntimePath(result.Metadata.Helper)) {
		return RoamingDeploymentReceipt{}, errors.New("original deployment receipt scope changed")
	}
	return result, nil
}

func (s *Service) validateDeploymentRuntime(ctx context.Context, r RoamingDeploymentRequest) error {
	var plan struct {
		Managed *struct{ Backend, CodexBinary, RuntimeDirectory, CaelisBinary, CaelisStore string }
	}
	if json.Unmarshal(r.Plan, &plan) != nil || plan.Managed == nil {
		return errors.New("target runtime plan required")
	}
	m := plan.Managed
	settings, e := s.ReadOwnedRuntimeSettings(ctx, s.options.NodeID, api.NodeBackend(m.Backend))
	if e != nil {
		return e
	}
	if m.Backend == "codex" && (m.CodexBinary != settings.Binary || m.RuntimeDirectory != "" && m.RuntimeDirectory != s.options.RuntimeDirectory || m.CaelisBinary != "" || m.CaelisStore != "") || m.Backend == "caelis" && (m.CaelisBinary != settings.Binary || m.CaelisStore != settings.Store || m.CodexBinary != "" || m.RuntimeDirectory != "") {
		return errors.New("deployment must retain this target's designated Runtime")
	}
	var workers struct {
		Runtimes []struct{ Backend, Binary, Store string }
	}
	if json.Unmarshal(r.Workers, &workers) != nil || len(workers.Runtimes) > 2 {
		return errors.New("closed target worker runtimes required")
	}
	seen := map[string]bool{}
	for _, v := range workers.Runtimes {
		actual, e := s.ReadOwnedRuntimeSettings(ctx, s.options.NodeID, api.NodeBackend(v.Backend))
		if e != nil || seen[v.Backend] || v.Binary != actual.Binary || v.Store != actual.Store {
			return errors.New("worker runtime must retain target-local designation")
		}
		seen[v.Backend] = true
	}
	return nil
}
