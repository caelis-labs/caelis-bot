package nodeagent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRoamingIPCSeparatesDurableFilesAndPinsNativeNamespace(t *testing.T) {
	enrolled := "/home/admin/.local/share/caelis-bot/node-agent"
	operation := "original-enable"
	ipc := RoamingIPCDirectory(enrolled, operation)
	dir := filepath.Join(enrolled, "roaming-"+filepath.Base(ipc)[2:])
	long := filepath.Join(dir, "joins", strings.Repeat("b", 16), "agent.sock")
	short := filepath.Join(ipc, "joins", strings.Repeat("b", 16), "agent.sock")
	if len(long) != 105 || !validSocket(short) || len(short) >= len(long) {
		t.Fatal("actual short HOME path regression", long, len(long), short, len(short))
	}
	p := RoamingSupervisorPlan{Version: 1, PlanID: strings.Repeat("a", 64), OperationID: operation, NodeID: "coordinator", Directory: dir, IPCDirectory: ipc, Helper: "/native/verified-helper", HelperSHA256: strings.Repeat("c", 64), Broker: &RoamingBrokerDeployment{NodeID: "coordinator", BotID: "bot", Profile: filepath.Join(dir, "broker"), Socket: filepath.Join(ipc, "broker.sock"), PeersFile: filepath.Join(dir, "peers.json"), BootstrapPeersFile: filepath.Join(dir, "bootstrap-peers.json")}}
	filename := filepath.Join(dir, "supervisor.json")
	if e := ValidateRoamingSupervisor(p, filename); e != nil {
		t.Fatal(e)
	}
	original := p
	p.IPCDirectory = RoamingIPCDirectory(enrolled, "replacement-enable")
	if e := ValidateRoamingSupervisor(p, filename); e == nil {
		t.Fatal("replacement operation substituted IPC slot")
	}
	p = original
	p.Broker = new(RoamingBrokerDeployment)
	*p.Broker = *original.Broker
	p.Broker.Socket = "/arbitrary/private/broker.sock"
	if e := ValidateRoamingSupervisor(p, filename); e == nil {
		t.Fatal("arbitrary socket outside approved slot accepted")
	}
	p = original
	p.IPCDirectory = ""
	p.Broker = new(RoamingBrokerDeployment)
	*p.Broker = *original.Broker
	p.Broker.Socket = filepath.Join(dir, "broker.sock")
	if e := ValidateRoamingSupervisor(p, filename); e != nil {
		t.Fatal("legacy stored supervisor plan rejected", e)
	}
}
