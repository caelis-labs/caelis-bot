//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type unavailableWorkerLeaseReader struct{}

func (unavailableWorkerLeaseReader) ReadWorkerLease(context.Context, nodeplane.WorkLeaseRef) (nodeplane.Lease, error) {
	return nodeplane.Lease{}, errors.New("contained source lease unavailable")
}
func testRoamingAgentOwnedWorkerDetach(t *testing.T, nodeID string) {
	root := canonicalWorkerTestRoot(t)
	executable, err := verifiedRoamingExecutable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	pidfile := filepath.Join(root, "worker.pid")
	binary := filepath.Join(root, "codex")
	if err = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestWorkerCLINativeHelper$' -- "+quote(pidfile)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "agent")
	if err = os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	c := roamingCommand{NodeID: nodeID, BotID: "raw-native-bot", Backend: "codex", AgentDirectory: directory, CodexBinary: binary, BrokerNodeID: "paired-broker"}
	plan := roamingWorkerPlan{Nodes: []backend.WorkerNodeConfig{{ID: "primary-node", Backend: "caelis", Transport: "registered-agent", Label: "Approved primary"}}}
	life, cancel := context.WithCancel(t.Context())
	defer cancel()
	workers := newRoamingOwnedWorkers(life, c, plan, executable, unavailableWorkerLeaseReader{}, func(context.Context, func(), func()) (func(), error) { return func() {}, nil })
	defer func() {
		if err := workers.Close(); err != nil {
			t.Error(err)
		}
	}()
	proxy := nodeagent.NewNativeWorkerProxy(life, workers.resolve)
	agent, err := nodeagent.New(nodeagent.Options{Directory: directory, NodeID: c.NodeID, WorkerProxy: proxy})
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(directory, "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	go func() { _ = nodeagent.Serve(life, listener, agent) }()
	client, err := nodeagent.Dial(t.Context(), socket, c.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: c.NodeID, Backend: "codex", Role: api.RoleWorker}, BotID: api.ProfileBotID(c.BotID), SourceNode: "primary-node", SourceBackend: "caelis"}
	wrong := pair
	wrong.SourceNode = "foreign-node"
	if _, err = client.OpenWorkerStream(t.Context(), wrong); err == nil {
		t.Fatal("unapproved source opened native Worker")
	}
	if _, err = os.Stat(pidfile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected source spawned native process")
	}
	open := func() *workerwire.Client {
		stream, err := client.OpenWorkerStream(t.Context(), pair)
		if err != nil {
			t.Fatal(err)
		}
		worker, err := workerwire.NewClient(t.Context(), pair, noActivation{}, stream)
		if err != nil {
			t.Fatal(err)
		}
		return worker
	}
	worker := open()
	if !worker.LeaseAwareAdmission() {
		t.Fatal("actual Worker readiness omitted independent native lease admission")
	}
	workspace, err := worker.ResolveWorkWorkspace(t.Context(), "task-"+strings.Repeat("0", 32), "")
	if err != nil || !filepath.IsAbs(workspace) {
		t.Fatalf("canonical native workspace %s %v", workspace, err)
	}
	before, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(before))
	worker.Close()
	if err = syscall.Kill(pid, 0); err != nil {
		t.Fatalf("observer detach killed independently owned Worker: %v", err)
	}
	again := open()
	again.Close()
	after, _ := os.ReadFile(pidfile)
	if string(after) != string(before) {
		t.Fatal("reattach replaced original native owner")
	}
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	if _, err = worker.ResolveWorkWorkspace(ctx, "task-"+strings.Repeat("1", 32), ""); err == nil {
		t.Fatal("detached stream retained observer authority")
	}
}

func TestModernEmptyWorkerRuntimeApprovalRejectsBeforeLaunch(t *testing.T) {
	// Write actual JSON so omitempty cannot turn explicit [] into absence.
	base := `{"version":1,"nodes":[{"id":"linux-primary","label":"Approved source","backend":"codex","transport":"registered-agent"}],"agents":[{"nodeId":"linux-primary","backend":"codex","socket":"/private/source/agent.sock"}]`
	for name, metadata := range map[string]string{
		"empty runtimes with sources":         `,"sources":[{"nodeId":"linux-primary","backends":["codex","caelis"]}],"runtimes":[]`,
		"omitted runtimes with sources":       `,"sources":[{"nodeId":"linux-primary","backends":["codex","caelis"]}]`,
		"empty runtimes without sources":      `,"runtimes":[]`,
		"empty sources with omitted runtimes": `,"sources":[]`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			filename := filepath.Join(root, "workers.json")
			if err := os.WriteFile(filename, []byte(base+metadata+`}`), 0600); err != nil {
				t.Fatal(err)
			}
			plan, err := loadRoamingWorkerPlan(filename)
			if err != nil {
				t.Fatal(err)
			}
			for _, primaryBackend := range []string{"codex", "caelis"} {
				command := roamingCommand{NodeID: api.LocalNodeID, BotID: "native-bot", Backend: primaryBackend, BrokerNodeID: api.LocalNodeID, AgentDirectory: filepath.Join(root, "absent-agent"), CodexBinary: "/private/unapproved-codex", CaelisBinary: "/private/unapproved-caelis", CaelisStore: "/private/unapproved-store"}
				workers := newRoamingOwnedWorkers(t.Context(), command, plan, "/private/unapproved-helper", unavailableWorkerLeaseReader{}, func(context.Context, func(), func()) (func(), error) {
					t.Error("unapproved runtime reached native power binding")
					return func() {}, nil
				})
				if len(workers.runtimes) != 0 {
					t.Fatal("empty approval recreated primary Runtime", workers.runtimes)
				}
				for _, targetBackend := range []string{"codex", "caelis"} {
					pair := workerwire.Pair{Target: api.WorkTarget{NodeID: api.LocalNodeID, Backend: targetBackend, Role: api.RoleWorker}, BotID: api.ProfileBotID(command.BotID), SourceNode: "linux-primary", SourceBackend: "codex"}
					if _, err := workers.resolve(t.Context(), pair); err == nil || err.Error() != "target Worker backend has no approved native Runtime binding" {
						t.Fatal("unapproved Worker proceeded beyond binding preflight", pair, err)
					}
				}
				if len(workers.workers) != 0 {
					t.Fatal("rejected route allocated native Worker")
				}
				if _, err := os.Lstat(command.AgentDirectory); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("runtime denial wrote native generation", err)
				}
			}
		})
	}
}

func TestLegacyWorkerManifestAloneInheritsPrimaryRuntimeBinding(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "legacy-workers.json")
	if err := os.WriteFile(filename, []byte(`{"version":1,"nodes":[],"agents":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := loadRoamingWorkerPlan(filename)
	if err != nil || plan.Runtimes != nil || plan.Sources != nil {
		t.Fatal("legacy metadata changed", plan, err)
	}
	for _, backend := range []string{"codex", "caelis"} {
		command := roamingCommand{NodeID: api.LocalNodeID, Backend: backend, CodexBinary: "/native/codex", CaelisBinary: "/native/caelis", CaelisStore: "/native/store", Model: "owned-model"}
		workers := newRoamingOwnedWorkers(t.Context(), command, plan, "", nil, nil)
		want := roamingWorkerRuntime{Backend: backend, Binary: command.CodexBinary}
		if backend == "caelis" {
			want.Binary, want.Store, want.Model = command.CaelisBinary, command.CaelisStore, command.Model
		}
		if len(workers.runtimes) != 1 || workers.runtimes[backend] != want {
			t.Fatal("legacy primary binding changed", workers.runtimes, want)
		}
	}
}
