package nodeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type RoamingCompanionChunk struct {
	SHA256, Architecture, SourceRevision string
	Offset, Total                        int64
	Bytes                                []byte
}

func roamingDeploymentKey(operationID string) string {
	sum := sha256.Sum256([]byte(operationID))
	return hex.EncodeToString(sum[:8])
}

func (s *Service) preflightJoinedDeployment(ctx context.Context, r RoamingDeploymentRequest, metadata RoamingDeploymentMetadata) error {
	if e := ValidateRoamingWorkers(r.Workers); e != nil {
		return e
	}
	if r.Preferences.Schema != 1 || r.Preferences.Revision < 1 {
		return errors.New("invalid target preferences")
	}
	var p RoamingSupervisorPlan
	d := json.NewDecoder(bytes.NewReader(r.Plan))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid closed native supervisor plan")
	}
	dir := filepath.Join(metadata.Directory, "roaming-"+roamingDeploymentKey(r.OperationID))
	if p.NodeID != r.NodeID || p.OperationID != r.OperationID || p.PlanID != r.PlanID || p.Directory != dir || p.Helper != metadata.Helper || metadata.HelperSHA256 != "" && p.HelperSHA256 != metadata.HelperSHA256 || p.Broker != nil || p.Managed == nil || ValidateRoamingSupervisor(p, filepath.Join(dir, "supervisor.json")) != nil {
		return errors.New("target native deployment pairing changed")
	}
	m := p.Managed
	if m.JoinSSHDestination != metadata.Route.Target || m.BrokerSSHDestination != metadata.Route.Target || m.JoinHelper != metadata.Route.Helper {
		return errors.New("existing outward pairing changed")
	}
	args, e := (SSHConfig{Target: metadata.Route.Target}).args()
	if e != nil {
		return e
	}
	verification, e := RoamingCoordinatorVerificationCommand(*m)
	if e != nil {
		return e
	}
	args = append(args, "-o", "ClearAllForwardings=yes", "--", metadata.Route.Target, verification)
	command := exec.CommandContext(ctx, "ssh", args...)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if command.Run() != nil {
		return errors.New("existing outbound authorization unavailable")
	}
	return nil
}

// Staging accepts bytes only for the fixed absent companion. The caller binds
// its independently verified release hash to the approved plan; EOF, wrong
// offset or ambiguous publication never causes a replay under another ID.
func (s *Service) stageRoamingCompanion(ctx context.Context, r RoamingDeploymentRequest, metadata RoamingDeploymentMetadata) (RoamingDeploymentReceipt, error) {
	result := RoamingDeploymentReceipt{NodeID: r.NodeID, OperationID: r.OperationID, PlanID: r.PlanID, Outcome: "unknown"}
	c := r.Companion
	if ctx.Err() != nil || c == nil || metadata.OS != "linux" || c.Architecture != metadata.Architecture || c.Total <= 0 || c.Total > 256<<20 || c.Offset < 0 || len(c.Bytes) == 0 || len(c.Bytes) > 256<<10 || c.Offset+int64(len(c.Bytes)) > c.Total {
		return result, errors.New("bounded reviewed companion required")
	}
	hash, e := hex.DecodeString(c.SHA256)
	revision, revisionErr := hex.DecodeString(c.SourceRevision)
	if e != nil || len(hash) != 32 || revisionErr != nil || len(revision) != 20 {
		return result, errors.New("reviewed companion artifact identity required")
	}
	if metadata.HelperSHA256 != "" {
		return result, errors.New("installed companion cannot be replaced")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(metadata.Directory, "host-stage-"+roamingDeploymentKey(r.OperationID))
	if c.Offset == 0 {
		if e := os.Mkdir(dir, 0700); e != nil {
			return result, errors.New("original companion stage exists; reconcile without replay")
		}
		intent := struct {
			OperationID, PlanID, SHA256, Architecture, SourceRevision string
			Total                                                     int64
		}{r.OperationID, r.PlanID, c.SHA256, c.Architecture, c.SourceRevision, c.Total}
		if e := WriteManagedPrivateJSON(filepath.Join(dir, "intent.json"), intent); e != nil {
			return result, e
		}
	}
	if CheckPrivateDirectory(dir) != nil {
		return result, errors.New("private companion stage unavailable")
	}
	var intent struct {
		OperationID, PlanID, SHA256, Architecture, SourceRevision string
		Total                                                     int64
	}
	if readPrivateJSON(filepath.Join(dir, "intent.json"), &intent) != nil || intent.OperationID != r.OperationID || intent.PlanID != r.PlanID || intent.SHA256 != c.SHA256 || intent.Architecture != c.Architecture || intent.SourceRevision != c.SourceRevision || intent.Total != c.Total {
		return result, errors.New("original companion intent changed")
	}
	path := filepath.Join(dir, "caelis-node")
	flags := os.O_WRONLY | os.O_APPEND
	if c.Offset == 0 {
		flags |= os.O_CREATE | os.O_EXCL
	} else {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != c.Offset {
			return result, errors.New("original companion offset unconfirmed")
		}
	}
	f, e := os.OpenFile(path, flags, 0600)
	if e != nil {
		return result, e
	}
	_, e = f.Write(c.Bytes)
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return result, e
	}
	if c.Offset+int64(len(c.Bytes)) == c.Total {
		if e = verifyExecutable(path, c.SHA256, c.Architecture, c.SourceRevision); e != nil {
			return result, e
		}
		if e = os.Chmod(path, 0700); e != nil {
			return result, e
		}
		// Exclusive hard-link publication cannot replace a prior helper or symlink.
		if e = os.Link(path, metadata.Helper); e != nil {
			return result, e
		}
		parent, e := os.Open(metadata.Directory)
		if e != nil {
			return result, e
		}
		e = parent.Sync()
		e = errors.Join(e, parent.Close())
		if e != nil {
			return result, e
		}
	}
	result.Outcome = "accepted"
	return result, nil
}
