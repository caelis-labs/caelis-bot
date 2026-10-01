// Package nodeagent is the optional foreground node catalog/installer owner.
// It never assembles the desktop APP, starts a Bot, reads model credentials,
// enrolls accounts or claims execution authority from an installed executable.
package nodeagent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type installer interface {
	Manage(context.Context, runtimemanagement.Request) (runtimemanagement.Status, error)
}

// NativeHealth is supplied only by an assembled native owner. Installation and
// version probes cannot create this proof. No credential/native path is public.
type NativeHealth struct {
	AuthenticationKnown, Authenticated, HealthKnown, Healthy bool
	ManagedOwner, Fenceable, BotEligible, WorkerEligible     bool
	SharedHost                                               bool
}
type Options struct {
	Directory, RuntimeDirectory, NodeID, Label string
	Join                                       api.NodeJoin
	Binaries                                   map[api.NodeBackend]string
	Health                                     func(context.Context, api.NodeBackend) (NativeHealth, error)
	Configurations                             map[api.NodeBackend]NativeConfiguration
	RuntimeOwner                               nodeplane.RuntimeProofPort
	ManagedProduct                             ManagedProductPort
	ManagedStart                               ManagedStartPort
}

const MaxOperations = 4096

type Service struct {
	options      Options
	installation installer
	mu           sync.Mutex
}

var _ nodeplane.CatalogAgent = (*Service)(nil)

func New(o Options) (*Service, error) {
	if err := CheckPrivateDirectory(o.Directory); err != nil {
		return nil, err
	}
	if o.Join == "" {
		o.Join = api.NodeSSH
	}
	if o.Join != api.NodeLocal && o.Join != api.NodeSSH && o.Join != api.NodeOutgoing {
		return nil, errors.New("invalid native join mode")
	}
	if o.Label == "" {
		o.Label = "Runtime node"
	}
	if len(o.Label) > 256 || strings.ContainsAny(o.Label, "\x00\r\n") {
		return nil, errors.New("invalid node label")
	}
	path := filepath.Join(o.Directory, "node.json")
	var identity struct {
		ID string `json:"id"`
	}
	err := readPrivateJSON(path, &identity)
	if errors.Is(err, os.ErrNotExist) {
		identity.ID = o.NodeID
		if identity.ID == "" {
			identity.ID = "node-" + rand.Text()
		}
		if !identifier.MatchString(identity.ID) {
			return nil, errors.New("invalid native node identity")
		}
		if err = writeState(path, identity); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if !identifier.MatchString(identity.ID) || (o.NodeID != "" && o.NodeID != identity.ID) {
		return nil, errors.New("native node identity changed")
	}
	o.NodeID = identity.ID
	copied := make(map[api.NodeBackend]string, len(o.Binaries))
	for k, v := range o.Binaries {
		if !backend(k) || !filepath.IsAbs(v) {
			return nil, errors.New("explicit native executable must be absolute")
		}
		copied[k] = v
	}
	o.Binaries = copied
	s := &Service{options: o}
	if o.RuntimeDirectory != "" {
		s.installation, err = runtimemanagement.New(o.RuntimeDirectory)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}
func backend(b api.NodeBackend) bool { return b == api.NodeCodex || b == api.NodeCaelis }

func (s *Service) Catalog(ctx context.Context) (api.NodeCatalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.catalog(ctx)
}
func (s *Service) catalog(ctx context.Context) (api.NodeCatalog, error) {
	node := api.NodeInfo{ID: s.options.NodeID, Label: s.options.Label, OS: api.NodeOS(runtime.GOOS), Join: s.options.Join, Runtimes: make([]api.NodeRuntime, 0, 2)}
	for _, b := range []api.NodeBackend{api.NodeCodex, api.NodeCaelis} {
		r := api.NodeRuntime{Backend: b, Authentication: api.NodeAuthUnknown, Health: api.NodeMissing, Roles: []api.NodeRoleCapability{{Role: api.RoleBot, Reason: "runtime-owner-unavailable"}, {Role: api.RoleWorker, Reason: "runtime-owner-unavailable"}}}
		if s.installation != nil {
			status, err := s.installation.Manage(ctx, runtimemanagement.Request{Action: "detect", Runtime: string(b)})
			if err == nil && status.Installed {
				r.Version = status.Version
				r.Health = api.NodeHealthUnknown
			}
		}
		if r.Version == "" {
			if version, err := s.detectVersion(ctx, b); err == nil && version != "" {
				r.Version = version
				r.Health = api.NodeHealthUnknown
			}
		}
		if s.options.Health != nil {
			h, err := s.options.Health(ctx, b)
			if err == nil {
				if h.AuthenticationKnown {
					r.Authentication = api.NodeAuthRequired
					if h.Authenticated {
						r.Authentication = api.NodeAuthenticated
					}
				}
				if h.HealthKnown {
					r.Health = api.NodeUnavailable
					if h.Healthy {
						r.Health = api.NodeHealthy
					}
				}
				controlled := h.ManagedOwner && h.Fenceable && !h.SharedHost && h.HealthKnown && h.Healthy && h.AuthenticationKnown && h.Authenticated
				r.Roles[0].Eligible = controlled && h.BotEligible && runtime.GOOS != "windows"
				r.Roles[1].Eligible = h.WorkerEligible && h.HealthKnown && h.Healthy && h.AuthenticationKnown && h.Authenticated
				if r.Roles[0].Eligible {
					r.Roles[0].Reason = ""
				} else if h.SharedHost {
					r.Roles[0].Reason = "shared-runtime-not-fenceable"
				} else if runtime.GOOS == "windows" && b == api.NodeCodex {
					r.Roles[0].Reason = "windows-process-ownership-unsupported"
				}
				if r.Roles[1].Eligible {
					r.Roles[1].Reason = ""
				}
			}
		}
		node.Runtimes = append(node.Runtimes, r)
	}
	encoded, _ := json.Marshal(node)
	hash := sha256.Sum256(encoded)
	pending, err := s.pendingOperations()
	if err != nil {
		return api.NodeCatalog{}, err
	}
	return api.NodeCatalog{Revision: hex.EncodeToString(hash[:]), Nodes: []api.NodeInfo{node}, SelectedNodeID: node.ID, PendingOperations: pending}, nil
}
func (s *Service) detectVersion(ctx context.Context, b api.NodeBackend) (string, error) {
	path := s.options.Binaries[b]
	if path == "" {
		var err error
		path, err = exec.LookPath(string(b))
		if err != nil {
			return "", nil
		}
	}
	home, err := os.MkdirTemp(s.options.Directory, ".probe-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(home)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{"--version"}
	if b == api.NodeCaelis {
		args = []string{"version", "--format", "json"}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "CODEX_HOME=" + filepath.Join(home, ".codex"), "LANG=C", "LC_ALL=C"}
	cmd.Dir = home
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	var out boundedOutput
	cmd.Stdout = &out
	if cmd.Run() != nil {
		return "", errors.New("native version unavailable")
	}
	version := ""
	if b == api.NodeCodex {
		if strings.HasPrefix(out.String(), "codex-cli ") {
			version = strings.TrimSpace(strings.TrimPrefix(out.String(), "codex-cli "))
		}
	} else {
		var v struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(out.Bytes(), &v) == nil {
			version = strings.TrimPrefix(v.Version, "v")
		}
	}
	if version == "" || len(version) > 128 || strings.ContainsAny(version, "\x00\r\n") {
		return "", errors.New("native version incompatible")
	}
	return version, nil
}

// RequestDigest is canonical full intent excluding the digest field itself.
func RequestDigest(r nodeplane.ManagementRequest) string {
	digest, _ := nodeplane.ManagementDigest(r)
	return digest
}

type operation struct {
	Schema  int                         `json:"schema"`
	Request nodeplane.ManagementRequest `json:"request"`
	Phase   string                      `json:"phase"`
	Receipt api.NodeOperationReceipt    `json:"receipt"`
}

func (s *Service) operationPath(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(s.options.Directory, "receipts", hex.EncodeToString(sum[:])+".json")
}
func readPrivateJSON(path string, v any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err := CheckPrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 1<<20 {
		return errors.New("private agent state invalid")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("private agent state changed")
	}
	d := json.NewDecoder(io.LimitReader(f, 1<<20+1))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("private agent state incompatible")
	}
	return nil
}
func validRequest(r nodeplane.ManagementRequest) bool {
	return identifier.MatchString(r.Ref.OperationID) && nodeplane.ValidateManagementRequest(r) == nil
}
func (s *Service) Configuration(ctx context.Context, nodeID string, b api.NodeBackend) (api.NodeRuntimeConfiguration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configuration(ctx, nodeID, b)
}
func (s *Service) configuration(ctx context.Context, nodeID string, b api.NodeBackend) (api.NodeRuntimeConfiguration, error) {
	if nodeID != s.options.NodeID || !backend(b) {
		return api.NodeRuntimeConfiguration{}, errors.New("configuration scope changed")
	}
	port := s.options.Configurations[b]
	out := api.NodeRuntimeConfiguration{Guard: api.NodeEditGuard{NodeID: nodeID, Backend: b}, ReviewedVersions: []string{}}
	if s.installation != nil {
		out.InstallerAvailable = true
		status, err := s.installation.Manage(ctx, runtimemanagement.Request{Action: "detect", Runtime: string(b)})
		if err == nil && status.Outcome == "accepted" {
			out.Installation = &api.NodeInstallationState{Installed: status.Installed, Version: status.Version, LatestVersion: status.LatestVersion}
		}
		for _, release := range runtimemanagement.Releases() {
			if release.Arch == runtime.GOARCH && release.Runtime == string(b) {
				out.ReviewedVersions = append(out.ReviewedVersions, release.Version)
			}
		}
	}
	if catalog, err := s.catalog(ctx); err == nil {
		out.Guard.Revision = catalog.Revision
	} else {
		return out, err
	}
	if port == nil {
		return out, nil
	}
	config, err := port.Read(ctx)
	if err != nil {
		return out, nil
	}
	out.ConfigurationAvailable = true
	out.Guard.Revision = config.Revision
	out.Configuration = config
	if scopes, ok := port.(interface {
		ExecutionScopes(context.Context) (api.WorkExecutionSettings, api.WorkExecutionSettings, error)
	}); ok {
		conversation, worker, err := scopes.ExecutionScopes(ctx)
		if err != nil {
			return out, err
		}
		out.Conversation = &conversation
		out.Worker = &worker
	}
	return out, nil
}

func (s *Service) Manage(ctx context.Context, r nodeplane.ManagementRequest) (api.NodeOperationReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := api.NodeOperationReceipt{Ref: r.Ref, Outcome: api.NodeRejected, Message: "invalid-command"}
	if !validRequest(r) || r.Ref.NodeID != s.options.NodeID {
		return result, nil
	}
	path := s.operationPath(r.Ref.OperationID)
	var prior operation
	if err := readPrivateJSON(path, &prior); err == nil {
		if prior.Schema != 1 || prior.Request.Ref != r.Ref || RequestDigest(prior.Request) != r.Ref.RequestDigest {
			return result, nil
		}
		return s.reconcile(prior)
	} else if !errors.Is(err, os.ErrNotExist) {
		result.Outcome = api.NodeUnknown
		result.Message = "original-receipt-unavailable"
		return result, nil
	}
	catalog, err := s.catalog(ctx)
	if err != nil {
		return result, err
	}
	result.Revision = catalog.Revision
	configuration, err := s.configuration(ctx, r.Ref.NodeID, r.Ref.Backend)
	if err != nil || (r.Change != nil && !configuration.ConfigurationAvailable) {
		result.Message = "configuration-unavailable"
		return result, nil
	}
	revision := configuration.Guard.Revision
	pendingOperations, err := s.pendingOperations()
	if err != nil {
		result.Outcome = api.NodeUnknown
		result.Message = "original-journal-unavailable"
		return result, nil
	}
	for _, pending := range pendingOperations {
		if pending.Backend == r.Ref.Backend {
			result.Message = "original-operation-pending"
			return result, nil
		}
	}

	if revision != r.Guard.Revision {
		result.Outcome = api.NodeConflicted
		result.Message = "catalog-revision-changed"
		return result, nil
	}
	if r.Installation != nil && (r.Installation.Action == api.NodeUpdate || r.Installation.Action == api.NodeInstall) && s.installation != nil {
		observed := configuration.Installation
		conflict := observed == nil
		if observed != nil {
			if r.Installation.Action == api.NodeUpdate {
				conflict = !observed.Installed || r.Installation.ExpectedVersion == "" || observed.Version != r.Installation.ExpectedVersion
			} else {
				conflict = observed.Installed || r.Installation.ExpectedVersion != ""
			}
		}
		if conflict {
			result.Outcome = api.NodeConflicted
			result.Message = "installation-version-changed"
			return result, nil
		}
	}
	if ctx.Err() != nil {
		result.Message = "cancelled-before-dispatch"
		return result, nil
	}
	if r.Installation != nil && s.installation == nil {
		result.Message = "installation-unavailable"
		return result, nil
	}
	if r.Change != nil && s.options.Configurations[r.Ref.Backend] == nil {
		result.Message = "configuration-unavailable"
		return result, nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return result, err
	}
	if err := CheckPrivateDirectory(dir); err != nil {
		return result, err
	}
	if file, err := os.Open(dir); err == nil {
		names, _ := file.Readdirnames(MaxOperations + 1)
		file.Close()
		if len(names) >= MaxOperations {
			result.Message = "original-receipt-limit"
			return result, nil
		}
	} else {
		return result, err
	}
	result.Outcome = api.NodeUnknown
	result.Message = "original-operation-unresolved"
	prior = operation{Schema: 1, Request: r, Phase: "intent", Receipt: result}
	if err := writeState(path, prior); err != nil {
		return result, errors.New("installation intent unavailable")
	}
	// Admitted mutation belongs to the foreground native owner, not the stream.
	operationCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if r.Installation != nil {
		status, err := s.installation.Manage(operationCtx, s.nativeRequest(r, string(r.Installation.Action)))
		result = installationReceipt(r.Ref, status, err)
	} else {
		native, err := s.options.Configurations[r.Ref.Backend].Change(operationCtx, r)
		result.Message = ""
		if err != nil || native.OperationID == "" {
			result.Outcome = api.NodeUnknown
			result.Message = "native-operation-unresolved"
		} else {
			switch native.Outcome {
			case "committed":
				result.Outcome = api.NodeCommitted
			case "conflicted":
				result.Outcome = api.NodeConflicted
			case "rejected":
				result.Outcome = api.NodeRejected
			default:
				result.Outcome = api.NodeUnknown
			}
		}
	}
	next, err := s.catalog(operationCtx)
	if err == nil {
		result.Revision = next.Revision
	}
	if r.Change != nil {
		if configuration, err := s.configuration(operationCtx, r.Ref.NodeID, r.Ref.Backend); err == nil {
			result.Revision = configuration.Guard.Revision
		}
	}
	prior.Phase = "done"
	prior.Receipt = result
	if err := writeState(path, prior); err != nil {
		result.Outcome = api.NodeUnknown
		result.Message = "original-receipt-unconfirmed"
	}
	return result, nil
}
func (s *Service) nativeRequest(r nodeplane.ManagementRequest, action string) runtimemanagement.Request {
	i := r.Installation
	sum := sha256.Sum256([]byte(r.Ref.NodeID + "\x00" + string(r.Ref.Backend) + "\x00" + r.Ref.OperationID))
	return runtimemanagement.Request{Action: action, Runtime: string(r.Ref.Backend), RequestID: "node-" + hex.EncodeToString(sum[:]), Version: i.Version, ExpectedVersion: i.ExpectedVersion}
}
func installationReceipt(ref api.NodeOperationRef, status runtimemanagement.Status, err error) api.NodeOperationReceipt {
	r := api.NodeOperationReceipt{Ref: ref, Outcome: api.NodeUnknown, Message: "native-operation-unresolved"}
	switch status.Outcome {
	case "accepted":
		if err == nil {
			r.Outcome = api.NodeCommitted
			r.Message = ""
		}
	case "rejected":
		r.Outcome = api.NodeRejected
		r.Message = "installation-rejected"
	}
	return r
}
func (s *Service) Reconcile(ctx context.Context, ref api.NodeOperationRef) (api.NodeOperationReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := api.NodeOperationReceipt{Ref: ref, Outcome: api.NodeUnknown, Message: "original-receipt-unavailable"}
	if ref.NodeID != s.options.NodeID || !backend(ref.Backend) || !identifier.MatchString(ref.OperationID) || len(ref.RequestDigest) != 64 {
		return r, nil
	}
	var prior operation
	if err := readPrivateJSON(s.operationPath(ref.OperationID), &prior); err != nil {
		return r, nil
	}
	if prior.Schema != 1 || prior.Request.Ref != ref || RequestDigest(prior.Request) != ref.RequestDigest {
		return r, nil
	}
	return s.reconcile(prior)
}
func (s *Service) reconcile(prior operation) (api.NodeOperationReceipt, error) {
	if prior.Phase == "done" && prior.Receipt.Outcome != api.NodeUnknown {
		return prior.Receipt, nil
	}
	if prior.Phase != "intent" && !(prior.Phase == "done" && prior.Receipt.Outcome == api.NodeUnknown) {
		return prior.Receipt, nil
	}
	if prior.Request.Change != nil {
		if port, ok := s.options.Configurations[prior.Request.Ref.Backend].(ConfigurationReconciler); ok {
			receipt, err := port.Reconcile(context.Background(), prior.Request.Ref)
			if err == nil && receipt.Ref == prior.Request.Ref && receipt.Outcome != api.NodeUnknown {
				prior.Phase = "done"
				prior.Receipt = receipt
				if writeState(s.operationPath(receipt.Ref.OperationID), prior) != nil {
					receipt.Outcome = api.NodeUnknown
					receipt.Message = "original-receipt-unconfirmed"
				}
			}
			return receipt, err
		}
		return prior.Receipt, nil
	}
	if prior.Request.Installation == nil || s.installation == nil {
		return prior.Receipt, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := s.installation.Manage(ctx, s.nativeRequest(prior.Request, "resolve"))
	r := installationReceipt(prior.Request.Ref, status, err)
	if catalog, err := s.catalog(ctx); err == nil {
		r.Revision = catalog.Revision
	}
	if r.Outcome != api.NodeUnknown {
		prior.Phase = "done"
		prior.Receipt = r
		if err := writeState(s.operationPath(r.Ref.OperationID), prior); err != nil {
			r.Outcome = api.NodeUnknown
			r.Message = "original-receipt-unconfirmed"
		}
	}
	return r, nil
}

// ReadRuntimeProof is a native-only observation of the actual assembled owner.
// An installed CLI or shared Host cannot satisfy this optional proof port.
func (s *Service) ReadRuntimeProof(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	if target.NodeID != s.options.NodeID || target.Role != api.RoleBot || !backend(api.NodeBackend(target.Backend)) || s.options.RuntimeOwner == nil {
		return nodeplane.RuntimeEligibility{}, errors.New("managed runtime owner unavailable")
	}
	proof, err := s.options.RuntimeOwner.ReadRuntimeProof(ctx, target)
	if err != nil {
		return proof, err
	}
	if proof.Proof.NodeID != target.NodeID || string(proof.Proof.Backend) != target.Backend || !proof.Proof.Controllable {
		return nodeplane.RuntimeEligibility{}, errors.New("native runtime proof scope changed")
	}
	return proof, nil
}

// Only bounded original nonterminal journal references enter presentation.
func (s *Service) pendingOperations() ([]api.NodeOperationRef, error) {
	refs := []api.NodeOperationRef{}
	path := filepath.Join(s.options.Directory, "receipts")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return refs, nil
	}
	if err := CheckPrivateDirectory(path); err != nil {
		return nil, errors.New("original agent journal unavailable")
	}
	directory, err := os.Open(path)
	if err != nil {
		return nil, errors.New("original agent journal unavailable")
	}
	defer directory.Close()
	entries, err := directory.ReadDir(MaxOperations + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.New("original agent journal unavailable")
	}
	if len(entries) > MaxOperations {
		return nil, errors.New("original agent journal limit exceeded")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var op operation
		if readPrivateJSON(filepath.Join(path, entry.Name()), &op) != nil || op.Schema != 1 || op.Request.Ref.NodeID != s.options.NodeID || nodeplane.ValidateManagementRequest(op.Request) != nil {
			return nil, errors.New("original agent journal cannot be verified")
		}
		if op.Phase != "intent" && op.Phase != "done" {
			return nil, errors.New("original agent journal phase unavailable")
		}
		if op.Phase == "intent" || op.Receipt.Outcome == api.NodeUnknown {
			refs = append(refs, op.Request.Ref)
		}
	}
	return refs, nil
}

// Agent intent publication syncs the containing directory before dispatch.
func writeState(path string, value any) error {
	if err := localstate.Write(path, value); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
