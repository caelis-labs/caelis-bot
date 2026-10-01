package app

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

func nativeRoamingIPCRoot(p NodeRoamingSupervisorPlan) string {
	if p.IPCDirectory != "" {
		return p.IPCDirectory
	}
	return p.Directory
}
func nativeManagedSocket(m NodeRoamingManagedDeployment) string {
	if m.AgentSocket != "" {
		return m.AgentSocket
	}
	return filepath.Join(m.AgentDirectory, "agent.sock")
}
func nativeRoamingNodeDirs(x roamingNativeNode) []string {
	dirs := []string{x.Plan.Directory}
	if x.Plan.IPCDirectory != "" {
		dirs = append(dirs, filepath.Dir(x.Plan.IPCDirectory), x.Plan.IPCDirectory)
	}
	if m := x.Plan.Managed; m != nil {
		dirs = append(dirs, m.AgentDirectory, m.GenerationRoot)
	}
	return dirs
}
func (s *roamingNativeSession) observationClients() map[string]*nodeagent.Client {
	all := make(map[string]*nodeagent.Client, len(s.peers)+len(s.management))
	for id, p := range s.peers {
		all[id] = p
	}
	for id, p := range s.management {
		all[id] = p
	}
	return all
}
func validateNativeRoamingSockets(p roamingNativePlan) error {
	paths := []string{p.BootstrapSocket}
	for _, x := range p.Nodes {
		if x.Plan.Managed != nil {
			paths = append(paths, x.AgentSocket, x.BrokerPeerSocket, x.Plan.Managed.BrokerSocket)
		}
		if x.Plan.Broker != nil {
			paths = append(paths, x.Plan.Broker.Socket)
		}
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) >= 100 || strings.ContainsAny(path, ":\x00\r\n") {
			return errors.New("native deployment private socket path exceeds platform limit or is incompatible")
		}
	}
	return nil
}

// Inspection is passive: missing IPC slots are permitted, existing slots must
// already be canonical, private and same-user. Deployment never repairs them.
func nativeRoamingInspectIPC(x roamingNativeNode) string {
	if x.Plan.IPCDirectory == "" {
		return "true"
	}
	base := filepath.Dir(x.Plan.IPCDirectory)
	home := filepath.Dir(base)
	script := "test \"$(cd " + nodeShellQuote(home) + " && pwd -P)\" = " + nodeShellQuote(home)
	for _, dir := range []string{base, x.Plan.IPCDirectory} {
		script += " && (if test -e " + nodeShellQuote(dir) + " || test -L " + nodeShellQuote(dir) + "; then " + nodeShellQuote(x.Registration.HelperPath) + " verify-join-directory --directory " + nodeShellQuote(dir) + " && test \"$(cd " + nodeShellQuote(dir) + " && pwd -P)\" = " + nodeShellQuote(dir) + "; fi)"
	}
	return script
}
