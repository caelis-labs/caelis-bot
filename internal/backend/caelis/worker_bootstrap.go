package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

// WorkerBootstrapRequest is native stdin only, never an application tool/DTO.
// AppCredential is scoped and cannot administer the Host.
type WorkerBootstrapRequest struct {
	Protocol                                                     WorkerProtocol `json:"protocol,omitempty"`
	Action, Store, WorkspaceRoot, TaskID, Workspace, OperationID string
	StoreID, InstanceID, PrincipalID                             string
	Selected                                                     bool
	AppCredential                                                string `json:"appCredential,omitempty"`
}
type WorkerBootstrapResult struct {
	Execution                                             api.WorkExecutionSettings
	ModelConfigured                                       bool
	ModelAuth                                             string
	Endpoint, StoreID, InstanceID, PrincipalID, Workspace string
	OS, Arch                                              string
	Capabilities                                          []string
	Connection                                            *wire.ApplicationConnection `json:"connection,omitempty"`
}

// RunWorkerBootstrap is shared by the narrow SSH helper and future node host.
// Errors intentionally omit paths, HTTP response bodies and credential bytes.
func RunWorkerBootstrap(ctx context.Context, in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(io.LimitReader(in, 65537))
	dec.DisallowUnknownFields()
	var req WorkerBootstrapRequest
	if err := dec.Decode(&req); err != nil {
		return errors.New("invalid Worker bootstrap request")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return errors.New("invalid Worker bootstrap request")
	}
	result, err := workerBootstrap(ctx, req)
	if err != nil {
		return errors.New("Worker bootstrap unavailable")
	}
	return json.NewEncoder(out).Encode(result)
}
func workerBootstrap(ctx context.Context, req WorkerBootstrapRequest) (WorkerBootstrapResult, error) {
	var out WorkerBootstrapResult
	protocol, protocolErr := workerProtocol(req.Protocol)
	if protocolErr != nil {
		return out, protocolErr
	}
	store, err := caelisruntime.Store(req.Store)
	if err != nil {
		return out, err
	}
	if err = privateDir(store); err != nil {
		return out, err
	}
	if err = rejectWorkspaceLinks(store); err != nil {
		return out, err
	}
	d, token, err := Discover(api.RuntimeSettings{CaelisStore: store})
	if err != nil {
		return out, err
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		return out, err
	}
	defer c.http.CloseIdleConnections()
	info, err := initializeCapabilities(ctx, c, workerProtocolCapabilities(protocol))
	if err != nil {
		return out, err
	}
	if value(info.InstanceId) != d.InstanceID || req.StoreID != "" && req.StoreID != value(info.StoreId) || req.InstanceID != "" && req.InstanceID != d.InstanceID || req.PrincipalID != "" && req.PrincipalID != d.PrincipalID {
		return out, errors.New("identity mismatch")
	}
	out = WorkerBootstrapResult{Endpoint: d.Endpoint, StoreID: value(info.StoreId), InstanceID: d.InstanceID, PrincipalID: d.PrincipalID, Capabilities: info.Capabilities, OS: runtime.GOOS, Arch: runtime.GOARCH}
	if req.Action == "probe" {
		out.Execution, out.ModelConfigured, out.ModelAuth = workerDefaultModel(ctx, c)
		if req.AppCredential != "" {
			return out, errors.New("probe cannot enroll")
		}
		return out, nil
	}
	if req.StoreID == "" || req.InstanceID == "" || req.PrincipalID == "" {
		return out, errors.New("identity required for mutation")
	}
	switch req.Action {
	case "enroll":
		if len(req.OperationID) < 1 || len(req.OperationID) > 128 || len(req.AppCredential) < 32 || len(req.AppCredential) > 256 || strings.ContainsAny(req.OperationID+req.AppCredential, "\x00\r\n") {
			return out, errors.New("invalid enrollment")
		}
		// Persist exact scope/operation BEFORE registration. A changed secret cannot
		// reuse the operation, including after a lost response or helper restart.
		root, err := caelisruntime.Store(req.Store)
		if err != nil {
			return out, err
		}
		journalPath := filepath.Join(root, "runtime", "bot-worker-enrollments", digest([]byte(req.OperationID))+".json")
		intent := credential{StoreID: out.StoreID, PrincipalID: out.PrincipalID, OperationID: req.OperationID, Token: req.AppCredential}
		if err = persistEnrollmentIntent(journalPath, intent); err != nil {
			return out, err
		}

		var life wire.ApplicationConnection
		err = c.json(ctx, "POST", "/applications/register", wire.ApplicationRegistration{OperationId: req.OperationID, Name: "Caelis Bot Worker", Credential: req.AppCredential}, &life, req.OperationID, "")
		if err != nil || life.PrincipalId != out.PrincipalID || life.ApplicationId == "" || life.ConnectionId == "" || life.Revoked {
			return out, errors.New("enrollment unconfirmed")
		}
		out.Connection = &life
	case "resolve_workspace", "prepare_workspace":
		if req.AppCredential != "" || req.TaskID == "" || len(req.TaskID) > 256 {
			return out, errors.New("invalid workspace request")
		}
		requested := req.Workspace
		if !req.Selected {
			if !filepath.IsAbs(req.WorkspaceRoot) {
				return out, errors.New("workspace root required")
			}
			requested = filepath.Join(req.WorkspaceRoot, "task-"+digest([]byte(req.TaskID))[:24])
		}
		if !filepath.IsAbs(requested) || strings.ContainsRune(requested, 0) {
			return out, errors.New("absolute workspace required")
		}
		target := filepath.Clean(requested)
		if err = rejectWorkspaceLinks(target); err != nil {
			return out, err
		}
		if req.Action == "prepare_workspace" && !req.Selected {
			if err = os.MkdirAll(target, 0700); err != nil {
				return out, err
			}
			if err = rejectWorkspaceLinks(target); err != nil {
				return out, err
			}
		}
		stat, statErr := os.Stat(target)
		if statErr == nil && (!stat.IsDir() || stat.Mode().Perm()&0200 == 0) {
			return out, errors.New("workspace unavailable")
		}
		if statErr != nil && (req.Selected || req.Action == "prepare_workspace" || !errors.Is(statErr, os.ErrNotExist)) {
			return out, statErr
		}
		out.Workspace = target
	default:
		return out, errors.New("unknown bootstrap action")
	}
	return out, nil
}
func rejectWorkspaceLinks(target string) error {
	current := target
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return errors.New("workspace path redirected")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

// Read only public model metadata. Configured/default/auth reports are separate
// from live provider permission or billable model execution, which is not tested.
func workerDefaultModel(ctx context.Context, c *client) (api.WorkExecutionSettings, bool, string) {
	var candidates []wire.SlashArgCandidate
	if c.json(ctx, "POST", "/completion/slash-arguments", wire.CompletionRequest{Command: pointer("model"), Limit: pointer(1000)}, &candidates, "", "") != nil {
		return api.WorkExecutionSettings{}, false, "unknown"
	}
	var status wire.StatusSnapshot
	statusKnown := c.json(ctx, "GET", "/status", nil, &status, "", "") == nil
	for _, model := range candidates {
		current := model.ModelSelection != nil && value(model.ModelSelection.Current)
		if !current && statusKnown && value(status.ModelStatus.Alias) != "" {
			current = model.Value == value(status.ModelStatus.Alias) || value(model.ModelConfigId) == value(status.ModelStatus.Alias)
		}
		if !current || model.Value == "" {
			continue
		}
		execution := api.WorkExecutionSettings{Model: model.Value}
		if model.ModelSelection != nil {
			execution.Effort = model.ModelSelection.Effort
			if value(model.ModelSelection.Fast) {
				execution.ServiceTier = "priority"
			}
		}
		auth := "unknown"
		if model.NoAuth != nil {
			if *model.NoAuth {
				auth = "reported_missing"
			} else {
				auth = "reported_ready"
			}
		} else if statusKnown && status.ModelStatus.MissingApiKey != nil {
			if *status.ModelStatus.MissingApiKey {
				auth = "reported_missing"
			} else {
				auth = "reported_ready"
			}
		}
		return execution, true, auth
	}
	return api.WorkExecutionSettings{}, false, "unknown"
}

// Publish a complete synced immutable intent with an exclusive hard link. Two
// helpers cannot replace each other's scope, and readers never see partial JSON.
func persistEnrollmentIntent(path string, intent credential) error {
	return publishEnrollmentIntent(path, intent, os.Link)
}
func publishEnrollmentIntent(path string, intent credential, link func(string, string) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := privateDir(dir); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".enrollment-intent-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	err = json.NewEncoder(temp).Encode(intent)
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = link(temp.Name(), path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		raw, readErr := privateRead(path, 65536)
		var previous credential
		if readErr != nil || json.Unmarshal(raw, &previous) != nil || previous != intent {
			return errors.New("enrollment intent conflict")
		}
	}
	// Also flush an already-published identical intent before a second helper can
	// replay native registration; the winner may not yet have flushed the entry.
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
