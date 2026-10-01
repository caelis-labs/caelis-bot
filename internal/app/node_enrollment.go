package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

type enrollmentPreflightError struct{ reason string }

func (e enrollmentPreflightError) Error() string { return "node enrollment " + e.reason }

// Native-only journal. The immutable request and target registration stay in
// private storage; the renderer receives only original IDs and safe outcomes.
type nodeEnrollmentRecord struct {
	Version      int                `json:"version"`
	Request      api.NodeAddRequest `json:"request"`
	Digest       string             `json:"digest"`
	Phase        string             `json:"phase"`
	Candidate    *NodeRegistration  `json:"candidate,omitempty"`
	Registration *NodeRegistration  `json:"registration,omitempty"`
	Result       api.NodeAddResult  `json:"result"`
}

func enrollmentDigest(r api.NodeAddRequest) string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (n *nativeNodeManagement) enrollmentPath(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(n.directory, "enrollments", hex.EncodeToString(sum[:])+".json")
}
func readEnrollmentRecord(path string) (record nodeEnrollmentRecord, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return record, err
	}
	if nodeagent.CheckPrivateDirectory(filepath.Dir(path)) != nil {
		return record, errors.New("private enrollment directory unavailable")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 256<<10 {
		return record, errors.New("private enrollment receipt unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return record, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return record, errors.New("private enrollment receipt changed")
	}
	d := json.NewDecoder(io.LimitReader(f, 256<<10))
	d.DisallowUnknownFields()
	if d.Decode(&record) != nil || d.Decode(new(any)) != io.EOF || record.Version != 1 || !productIdentifier.MatchString(record.Request.OperationID) || record.Digest != enrollmentDigest(record.Request) || record.Result.OperationID != record.Request.OperationID || (record.Phase != "preflight" && record.Phase != "bootstrap") || (record.Result.Outcome != "unknown" && record.Result.Outcome != "failed" && record.Result.Outcome != "committed") {
		return record, errors.New("private enrollment receipt invalid")
	}
	if candidate := record.Candidate; candidate != nil && (validateNodeRegistration(*candidate) != nil || candidate.Join != api.NodeSSH || candidate.Label != record.Request.Label || candidate.SSHDestination != record.Request.SSHDestination || candidate.HelperPath != filepath.Join(candidate.Directory, "caelis-agent") || candidate.HostHelperPath != "" && candidate.HostHelperPath != filepath.Join(candidate.Directory, "caelis-node")) {
		return record, errors.New("original enrollment candidate changed")
	}
	if record.Candidate != nil && record.Registration != nil && *record.Candidate != *record.Registration {
		return record, errors.New("original enrollment identity changed")
	}
	if record.Registration != nil && (validateNodeRegistration(*record.Registration) != nil || record.Registration.Label != record.Request.Label || record.Registration.Join != record.Request.Join || record.Registration.Join == api.NodeSSH && record.Registration.SSHDestination != record.Request.SSHDestination || record.Result.Node.ID != record.Registration.ID || record.Result.Node.Join != record.Registration.Join || record.Result.Node.Label != record.Registration.Label) {
		return record, errors.New("original enrollment target changed")
	}
	if record.Result.Outcome == "committed" && record.Registration == nil {
		return record, errors.New("original enrollment registration unavailable")
	}
	return record, nil
}
func (n *nativeNodeManagement) pendingEnrollments() ([]string, error) {
	directory := filepath.Join(n.directory, "enrollments")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || len(entries) > 128 || nodeagent.CheckPrivateDirectory(directory) != nil {
		return nil, errors.New("original enrollment receipts unavailable")
	}
	var pending []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".managed-stage-") {
			continue
		}
		r, e := readEnrollmentRecord(filepath.Join(directory, entry.Name()))
		if e != nil {
			return nil, errors.New("original enrollment receipts unavailable")
		}
		if filepath.Base(n.enrollmentPath(r.Request.OperationID)) != entry.Name() {
			return nil, errors.New("original enrollment receipt scope changed")
		}
		if r.Result.Outcome == "unknown" {
			pending = append(pending, r.Request.OperationID)
		}
	}
	return pending, nil
}
func unknownEnrollment(id string) api.NodeAddResult {
	return api.NodeAddResult{OperationID: id, Outcome: "unknown", Reason: "unknown"}
}
func failedEnrollment(id, reason string) api.NodeAddResult {
	return api.NodeAddResult{OperationID: id, Outcome: "failed", Reason: reason}
}

func (n *nativeNodeManagement) Add(ctx context.Context, r api.NodeAddRequest) (api.NodeAddResult, error) {
	legacy := r.OperationID == ""
	if legacy {
		r.OperationID = "enroll-" + rand.Text()
	}
	result, err := n.addOriginalEnrollment(ctx, r)
	if legacy && err == nil && result.Outcome != "committed" {
		err = errors.New("node enrollment " + result.Outcome)
	}
	return result, err
}
func (n *nativeNodeManagement) addOriginalEnrollment(ctx context.Context, r api.NodeAddRequest) (api.NodeAddResult, error) {
	n.controlMu.Lock()
	defer n.controlMu.Unlock()
	if !productIdentifier.MatchString(r.OperationID) {
		return api.NodeAddResult{}, errors.New("invalid original enrollment identity")
	}
	if prior, err := readEnrollmentRecord(n.enrollmentPath(r.OperationID)); err == nil {
		if prior.Request != r {
			return unknownEnrollment(r.OperationID), nil
		}
		return n.reconcileEnrollment(ctx, prior)
	} else if !errors.Is(err, os.ErrNotExist) {
		return unknownEnrollment(r.OperationID), nil
	}
	pending, err := n.pendingEnrollments()
	if err != nil {
		return failedEnrollment(r.OperationID, "receipt-unavailable"), nil
	}
	if len(pending) > 0 {
		result := unknownEnrollment(pending[0])
		result.Reason = "original-pending"
		return result, nil
	}
	if r.Label == "" || len(r.Label) > 512 || strings.ContainsAny(r.Label, "\x00") || (r.Join != api.NodeSSH && r.Join != api.NodeOutgoing) || (r.Join == api.NodeSSH && (r.SSHDestination == "" || len(r.SSHDestination) > 256)) || (r.Join == api.NodeOutgoing && r.SSHDestination != "") || r.ExpectedRevision == "" || len(r.ExpectedRevision) > 256 {
		return api.NodeAddResult{OperationID: r.OperationID, Outcome: "failed", Reason: "invalid"}, nil
	}
	directory := filepath.Dir(n.enrollmentPath(r.OperationID))
	if err = os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return failedEnrollment(r.OperationID, "receipt-unavailable"), nil
	}
	if nodeagent.CheckPrivateDirectory(directory) != nil {
		return failedEnrollment(r.OperationID, "receipt-unavailable"), nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return failedEnrollment(r.OperationID, "receipt-unavailable"), nil
	}
	if len(entries) >= 128 {
		return failedEnrollment(r.OperationID, "limit"), nil
	}
	record := nodeEnrollmentRecord{Version: 1, Request: r, Digest: enrollmentDigest(r), Phase: "preflight", Result: unknownEnrollment(r.OperationID)}
	write := func() error { return nodeagent.WriteManagedPrivateJSON(n.enrollmentPath(r.OperationID), record) }
	if write() != nil {
		return failedEnrollment(r.OperationID, "receipt-unavailable"), nil
	}
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	if n.ownerCtx != nil {
		stop := context.AfterFunc(n.ownerCtx, cancel)
		defer stop()
	}
	beforeMutation := func() error {
		if err := life.Err(); err != nil {
			return err
		}
		record.Phase = "bootstrap"
		if err := write(); err != nil {
			record.Phase = "preflight"
			return enrollmentPreflightError{"receipt-unavailable"}
		}
		return nil
	}
	prepared := func(reg NodeRegistration, result api.NodeAddResult) error {
		if err := life.Err(); err != nil {
			return err
		}
		record.Registration = &reg
		result.OperationID = r.OperationID
		result.Outcome = "unknown"
		result.Reason = "unknown"
		record.Result = result
		return write()
	}
	candidate := func(reg NodeRegistration) error {
		if err := life.Err(); err != nil {
			return err
		}
		record.Candidate = &reg
		return write()
	}
	result, err := n.addEnrollment(life, r, beforeMutation, candidate, prepared)
	if err == nil {
		result.OperationID = r.OperationID
		result.Outcome = "committed"
		record.Result = result
	} else if record.Phase == "preflight" {
		reason := "preflight"
		var safe enrollmentPreflightError
		if errors.As(err, &safe) {
			reason = safe.reason
		}
		record.Result = api.NodeAddResult{OperationID: r.OperationID, Outcome: "failed", Reason: reason}
	}
	if write() != nil {
		if record.Phase == "preflight" {
			return record.Result, nil
		}
		return unknownEnrollment(r.OperationID), nil
	}
	return record.Result, nil
}

// Reconcile checks the original journal and existing SSH identity/catalog.
// It never installs, allocates another NodeID or restarts a Runtime/Bot owner.
func (n *nativeNodeManagement) ReconcileEnrollment(ctx context.Context, id string) (api.NodeAddResult, error) {
	if !productIdentifier.MatchString(id) {
		return api.NodeAddResult{}, errors.New("invalid original enrollment identity")
	}
	if err := ctx.Err(); err != nil {
		return unknownEnrollment(id), nil
	}
	n.controlMu.Lock()
	defer n.controlMu.Unlock()
	record, err := readEnrollmentRecord(n.enrollmentPath(id))
	if err != nil {
		return unknownEnrollment(id), nil
	}
	return n.reconcileEnrollment(ctx, record)
}
func (n *nativeNodeManagement) reconcileEnrollment(ctx context.Context, record nodeEnrollmentRecord) (api.NodeAddResult, error) {
	if record.Result.Outcome != "unknown" {
		return record.Result, nil
	}
	if record.Phase == "preflight" {
		// controlMu proves no live dispatch owns this preflight-only record.
		record.Result.Outcome = "failed"
		record.Result.Reason = "preflight"
	} else if record.Registration != nil {
		n.mu.Lock()
		for _, reg := range n.document.Nodes {
			if reg == *record.Registration {
				record.Result.Outcome = "committed"
				record.Result.Reason = ""
				break
			}
		}
		n.mu.Unlock()
	}
	if record.Result.Outcome == "unknown" && record.Request.Join == api.NodeSSH && n.options.Bootstrap == nil {
		// Older journals may lack a candidate after helper publication. Adopt
		// only the same SSH user's private fixed slot, never an arbitrary path.
		catalog, err := n.Catalog(ctx)
		if err != nil || catalog.Revision != record.Request.ExpectedRevision {
			return record.Result, nil
		}
		identity, err := nodeagent.ProbeSSHEnrollmentIdentity(ctx, nodeagent.SSHConfig{Target: record.Request.SSHDestination})
		if err != nil || identity.NodeID == "" || !identity.AgentAvailable {
			return record.Result, nil
		}
		reg := NodeRegistration{ID: identity.NodeID, Label: record.Request.Label, Join: api.NodeSSH, SSHDestination: record.Request.SSHDestination, Directory: identity.Directory, HelperPath: filepath.Join(identity.Directory, "caelis-agent")}
		if identity.HostAvailable {
			reg.HostHelperPath = filepath.Join(identity.Directory, "caelis-node")
		}
		if record.Candidate != nil && *record.Candidate != reg || record.Registration != nil && *record.Registration != reg {
			return record.Result, nil
		}
		n.mu.Lock()
		revision := n.document.Revision
		blocked := n.closed || len(n.document.Nodes) >= 16
		for _, existing := range n.document.Nodes {
			blocked = blocked || existing.ID == reg.ID || existing.SSHDestination == reg.SSHDestination
		}
		n.mu.Unlock()
		if blocked {
			return record.Result, nil
		}
		prepared := func(reg NodeRegistration, result api.NodeAddResult) error {
			record.Candidate, record.Registration = &reg, &reg
			result.OperationID, result.Outcome, result.Reason = record.Request.OperationID, "unknown", "unknown"
			record.Result = result
			return nodeagent.WriteManagedPrivateJSON(n.enrollmentPath(record.Request.OperationID), record)
		}
		result, err := n.completeEnrollment(ctx, reg, nil, revision, prepared)
		if err == nil {
			result.OperationID, result.Outcome = record.Request.OperationID, "committed"
			record.Result = result
		}
	}
	if record.Result.Outcome != "unknown" && nodeagent.WriteManagedPrivateJSON(n.enrollmentPath(record.Request.OperationID), record) != nil {
		if record.Phase == "preflight" {
			return record.Result, nil
		}
		return unknownEnrollment(record.Request.OperationID), nil
	}
	return record.Result, nil
}
