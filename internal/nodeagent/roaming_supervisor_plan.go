package nodeagent

import (
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
)

type RoamingSupervisorPlan struct {
	Version      int                       `json:"version"`
	PlanID       string                    `json:"planId"`
	OperationID  string                    `json:"operationId"`
	NodeID       string                    `json:"nodeId"`
	Helper       string                    `json:"helper"`
	HelperSHA256 string                    `json:"helperSha256"`
	Directory    string                    `json:"directory"`
	IPCDirectory string                    `json:"ipcDirectory,omitempty"`
	Broker       *RoamingBrokerDeployment  `json:"broker"`
	Managed      *RoamingManagedDeployment `json:"managed"`
}
type RoamingBrokerDeployment struct {
	BotID, NodeID, Profile, Socket, PeersFile, BootstrapPeersFile, PreferredNodeID string
}
type RoamingManagedDeployment struct {
	BotID, NodeID, Backend, AgentDirectory, GenerationRoot, AuthFile, CodexBinary, RuntimeDirectory string
	AgentSocket                                                                                     string `json:",omitempty"`
	CaelisBinary, CaelisStore, Model                                                                string
	BrokerNodeID, BrokerSocket, BrokerSSHDestination, BrokerHelper, WorkersFile                     string
	JoinSSHDestination, JoinHelper, JoinDirectory                                                   string
	CoordinatorIdentity                                                                             *NativeEnrollmentIdentity `json:",omitempty"`
}

func ValidateRoamingSupervisor(p RoamingSupervisorPlan, filename string) error {
	digest, e := hex.DecodeString(p.PlanID)
	helperDigest, helperErr := hex.DecodeString(p.HelperSHA256)
	if helperErr != nil || len(helperDigest) != 32 || e != nil || len(digest) != 32 || p.Version != 1 || !identifier.MatchString(p.OperationID) || p.NodeID == "" || p.Broker == nil && p.Managed == nil || p.Directory != filepath.Dir(filename) || filename != filepath.Join(p.Directory, "supervisor.json") || !filepath.IsAbs(p.Helper) || len(p.HelperSHA256) != 64 {
		return errors.New("exact approved supervisor identity required")
	}
	inside := func(path string) bool {
		r, e := filepath.Rel(p.Directory, path)
		return e == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && filepath.IsAbs(path) && filepath.Clean(path) == path
	}
	if p.IPCDirectory != "" && (p.IPCDirectory == p.Directory || p.IPCDirectory != RoamingIPCDirectory(filepath.Dir(p.Directory), p.OperationID) || !filepath.IsAbs(p.IPCDirectory) || filepath.Clean(p.IPCDirectory) != p.IPCDirectory) {
		return errors.New("IPC directory must match exact enrolled HOME and original operation")
	}
	insideIPC := func(path string) bool {
		return p.IPCDirectory != "" && filepath.Dir(path) == p.IPCDirectory && validSocket(path)
	}
	if p.Broker != nil {
		v := p.Broker
		if v.NodeID != p.NodeID || v.BotID == "" || !inside(v.Profile) || (!inside(v.Socket) && !insideIPC(v.Socket)) || !inside(v.PeersFile) || !inside(v.BootstrapPeersFile) {
			return errors.New("broker paths do not match exact approved native slot")
		}
	}
	if p.Managed != nil {
		v := p.Managed
		if v.CoordinatorIdentity != nil {
			if err := ValidateRoamingCoordinatorIdentity(*v); err != nil {
				return err
			}
		}
		if v.NodeID != p.NodeID || v.BotID == "" || (v.Backend != "codex" && v.Backend != "caelis") || v.Backend == "caelis" && (!filepath.IsAbs(v.CaelisBinary) || !filepath.IsAbs(v.CaelisStore)) || v.BrokerNodeID == "" || !inside(v.AgentDirectory) || !inside(v.GenerationRoot) || !inside(v.AuthFile) || v.WorkersFile != "" && !inside(v.WorkersFile) || !filepath.IsAbs(v.BrokerSocket) || v.BrokerSSHDestination != "" && (!filepath.IsAbs(v.BrokerHelper) || v.JoinSSHDestination == "" || !filepath.IsAbs(v.JoinDirectory)) {
			return errors.New("managed paths do not match exact approved native slot")
		}
		if v.AgentSocket != "" && (!insideIPC(v.AgentSocket) || filepath.Base(v.AgentSocket) != "agent.sock") {
			return errors.New("managed IPC socket does not match approved native slot")
		}
		if p.Broker != nil && (v.BrokerNodeID != p.NodeID || v.BrokerSocket != p.Broker.Socket) {
			return errors.New("local broker/managed pairing mismatch")
		}
		if p.Broker != nil && v.BotID != p.Broker.BotID {
			return errors.New("broker and managed Bot identities differ")
		}
	}
	return nil
}
