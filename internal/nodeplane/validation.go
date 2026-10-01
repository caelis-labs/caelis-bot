package nodeplane

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func validBackend(b api.NodeBackend) bool { return b == api.NodeCodex || b == api.NodeCaelis }

func validDigest(s string) bool {
	if len(s) != sha256.Size*2 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// ValidateCatalog checks presentation consistency; it does not grant execution
// authority. Hosts still verify the actual runtime fence at every admission.
func ValidateCatalog(c api.NodeCatalog) error {
	if c.Revision == "" {
		return errors.New("catalog revision is required")
	}
	nodes := map[string]api.NodeInfo{}
	eligibleBot := false
	for _, n := range c.Nodes {
		if n.ID == "" || n.Label == "" {
			return errors.New("node identity and label are required")
		}
		if _, exists := nodes[n.ID]; exists {
			return errors.New("duplicate node identity")
		}
		if n.OS != api.NodeDarwin && n.OS != api.NodeLinux && n.OS != api.NodeWindows && n.OS != api.NodeOSUnknown {
			return errors.New("unsupported node OS")
		}
		if n.Join != api.NodeLocal && n.Join != api.NodeSSH && n.Join != api.NodeOutgoing {
			return errors.New("unsupported node join")
		}
		runtimes := map[api.NodeBackend]bool{}
		for _, r := range n.Runtimes {
			if !validBackend(r.Backend) || runtimes[r.Backend] {
				return errors.New("unsupported or duplicate node runtime")
			}
			runtimes[r.Backend] = true
			if r.Authentication != api.NodeAuthUnknown && r.Authentication != api.NodeAuthRequired && r.Authentication != api.NodeAuthenticated {
				return errors.New("unsupported node authentication state")
			}
			if r.Health != api.NodeHealthUnknown && r.Health != api.NodeHealthy && r.Health != api.NodeUnavailable && r.Health != api.NodeMissing {
				return errors.New("unsupported node health state")
			}
			roles := map[api.WorkRole]bool{}
			for _, role := range r.Roles {
				if (role.Role != api.RoleBot && role.Role != api.RoleWorker) || roles[role.Role] {
					return errors.New("unsupported or duplicate runtime role")
				}
				roles[role.Role] = true
				if role.Eligible && (r.Health != api.NodeHealthy || r.Authentication != api.NodeAuthenticated) {
					return errors.New("eligible runtime must be healthy and authenticated")
				}
				if role.Eligible && role.Role == api.RoleBot && n.OS == api.NodeWindows && r.Backend == api.NodeCodex {
					return errors.New("Windows Codex cannot own Bot")
				}
				eligibleBot = eligibleBot || role.Eligible && role.Role == api.RoleBot
			}
		}
		nodes[n.ID] = n
	}
	for _, id := range []string{c.SelectedNodeID, c.ActiveBotNodeID, c.PairedProductNodeID} {
		if id != "" {
			if _, exists := nodes[id]; !exists {
				return errors.New("catalog references unknown node")
			}
		}
	}
	if c.WorkerTarget != nil {
		if err := c.WorkerTarget.Validate(); err != nil {
			return err
		}
		if c.WorkerTarget.Role != api.RoleWorker {
			return errors.New("Worker target must have Worker role")
		}
		n, exists := nodes[c.WorkerTarget.NodeID]
		if !exists {
			return errors.New("Worker target references unknown node")
		}
		found := false
		for _, r := range n.Runtimes {
			found = found || string(r.Backend) == c.WorkerTarget.Backend
		}
		if !found {
			return errors.New("Worker target references unknown runtime")
		}
	}
	if c.Broker != nil {
		if _, exists := nodes[c.Broker.NodeID]; !exists {
			return errors.New("broker references unknown node")
		}
		if c.Broker.AutomaticRoaming && (!c.Broker.Reachable || !eligibleBot) {
			return errors.New("automatic roaming requires reachable broker and eligible Bot runtime")
		}
	}
	pending := map[api.NodeOperationRef]bool{}
	for _, ref := range c.PendingOperations {
		if err := ValidateOperationRef(ref); err != nil {
			return err
		}
		n, exists := nodes[ref.NodeID]
		if !exists {
			return errors.New("pending operation references unknown node")
		}
		found := false
		for _, runtime := range n.Runtimes {
			found = found || runtime.Backend == ref.Backend
		}
		if !found || pending[ref] {
			return errors.New("pending operation runtime is unavailable or duplicated")
		}
		pending[ref] = true
	}
	return nil
}

func (s SnapshotRef) Validate() error {
	if s.BotID == "" || s.Epoch == "" || !validDigest(s.Digest) {
		return errors.New("snapshot requires Bot, epoch and SHA256 digest")
	}
	v, err := strconv.ParseUint(s.Version, 10, 64)
	if err != nil || v == 0 || strconv.FormatUint(v, 10) != s.Version {
		return errors.New("snapshot version must be canonical positive uint64 decimal")
	}
	return nil
}

// NewerSnapshot checks the whole stable Bot identity before comparing versions.
// A later epoch cannot resurrect an old version. Identical versions must retain
// the exact epoch and digest; they are receipt replay, not fresh publication.
func NewerSnapshot(current, next SnapshotRef) (bool, error) {
	if err := current.Validate(); err != nil {
		return false, err
	}
	if err := next.Validate(); err != nil {
		return false, err
	}
	if current.BotID != next.BotID {
		return false, errors.New("snapshot Bot identity changed")
	}
	oldVersion, _ := strconv.ParseUint(current.Version, 10, 64)
	newVersion, _ := strconv.ParseUint(next.Version, 10, 64)
	if newVersion < oldVersion {
		return false, errors.New("stale snapshot version")
	}
	if newVersion == oldVersion {
		if current != next {
			return false, errors.New("snapshot version reused with different content or epoch")
		}
		return false, nil
	}
	return true, nil
}

func (l Lease) Validate() error {
	if l.BotID == "" || l.NodeID == "" || !validBackend(l.Backend) || l.Epoch == "" || l.ExpiresAt.IsZero() || l.TTLMs <= 0 || l.TTLMs > DefaultLeaseExpiry.Milliseconds() {
		return errors.New("invalid Bot lease")
	}
	return nil
}

// Deadline uses the local monotonic component of requestStarted. Never start
// the TTL at receipt time or grant authority from an informational wall expiry.
func (l Lease) Deadline(requestStarted time.Time, stopMargin time.Duration) (time.Time, error) {
	if err := l.Validate(); err != nil {
		return time.Time{}, err
	}
	if requestStarted.IsZero() || stopMargin < 0 || stopMargin >= time.Duration(l.TTLMs)*time.Millisecond {
		return time.Time{}, errors.New("invalid lease deadline margin")
	}
	return requestStarted.Add(time.Duration(l.TTLMs)*time.Millisecond - stopMargin), nil
}

func (r RuntimeProof) ValidateFor(l Lease) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if !r.Controllable || r.NodeID != l.NodeID || r.Backend != l.Backend || r.Epoch != l.Epoch {
		return errors.New("runtime proof does not attest the exact lease")
	}
	return nil
}

func ValidateOperationRef(r api.NodeOperationRef) error {
	if r.NodeID == "" || !validBackend(r.Backend) || r.OperationID == "" || !validDigest(r.RequestDigest) {
		return errors.New("operation requires exact node, runtime, original ID and SHA256 digest")
	}
	return nil
}

// MatchesEdit is the completion CAS. A newer view, another selected node or a
// different runtime cannot consume an older asynchronous result.
func MatchesEdit(current, captured api.NodeEditGuard) bool {
	return current.NodeID != "" && validBackend(current.Backend) && current.Revision != "" && current == captured
}

// ManagementDigest encodes a closed semantic intent, excluding its journal ID.
// Each ordered string is decimal UTF-8 byte length + ':' + exact UTF-8 bytes.
// This encoding is shared with the renderer; JSON escaping/order never enters
// identity. New payload fields require an explicitly versioned digest format.
func ManagementDigest(r ManagementRequest) (string, error) {
	if r.Guard.NodeID == "" || !validBackend(r.Guard.Backend) || r.Guard.Revision == "" {
		return "", errors.New("management edit guard is incomplete")
	}
	if (r.Change == nil) == (r.Installation == nil) {
		return "", errors.New("management requires exactly one semantic payload")
	}
	fields := []string{"node-management-v1", r.Guard.NodeID, string(r.Guard.Backend), r.Guard.Revision}
	if c := r.Change; c != nil {
		fields = append(fields, "configuration", c.Action, c.ID, c.Name, c.Description, c.Selection.Model, c.Selection.Effort, c.Selection.ServiceTier, c.ExpectedRevision)
	} else {
		i := r.Installation
		fields = append(fields, "installation", string(i.Action), i.Version, i.ExpectedVersion)
	}
	h := sha256.New()
	for _, f := range fields {
		if !utf8.ValidString(f) {
			return "", errors.New("management payload must be valid UTF-8")
		}
		fmt.Fprintf(h, "%d:", len(f))
		h.Write([]byte(f))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ValidateManagementRequest(r ManagementRequest) error {
	if err := ValidateOperationRef(r.Ref); err != nil {
		return err
	}
	if r.Guard.NodeID != r.Ref.NodeID || r.Guard.Backend != r.Ref.Backend {
		return errors.New("operation does not belong to edited node and runtime")
	}
	digest, err := ManagementDigest(r)
	if err != nil {
		return err
	}
	if digest != r.Ref.RequestDigest {
		return errors.New("operation payload digest mismatch")
	}
	if c := r.Change; c != nil {
		if c.ExpectedRevision != r.Guard.Revision {
			return errors.New("configuration revision differs from edit guard")
		}
		switch c.Action {
		case "main", "bind", "reset", "create-role", "delete-role", "save-set", "apply-set", "delete-set", "remove-model", "disconnect-agent", "conversation-model", "worker-model":
		default:
			return errors.New("unsupported configuration action")
		}
	} else {
		switch r.Installation.Action {
		case api.NodeInstall, api.NodeUpdate, api.NodeCheckUpdate, api.NodeDetect:
		default:
			return errors.New("unsupported installation action")
		}
	}
	return nil
}
