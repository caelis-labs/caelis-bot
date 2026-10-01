package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

func TestDefaultConstructorsAssembleEnrolledWorkerLookup(t *testing.T) {
	for _, owned := range []bool{false, true} {
		root := t.TempDir()
		var a *Application
		var err error
		if owned {
			a, err = NewOwnedResident(t.Context(), root, Host{}, "fixture-owner", "/fixture/caelis-node")
		} else {
			a, err = New(root, Host{})
		}
		if err != nil {
			t.Fatal(err)
		}
		if a.registeredWorkers == nil || a.started {
			t.Fatal("ordinary composition lacks dormant native lookup")
		}
		if err = a.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Explicit local acceptance: a real installed Codex with an empty isolated
// CODEX_HOME talks only to the synthetic loopback provider. SSH executes locally
// inside this test, while both product commands and target owner are production.
func TestProductionEnrolledWorkerAndNotebookComposition(t *testing.T) {
	helper, binary := os.Getenv("CAELIS_BOT_COMPOSITION_HELPER"), os.Getenv("CAELIS_BOT_TEST_NATIVE_PRIMARY")
	if helper == "" || binary == "" {
		t.Skip("requires explicit local helper and native CLI; no remote nodes")
	}
	root, err := os.MkdirTemp("/tmp", "cb-compose-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home := filepath.Join(root, "home")
	_ = os.Mkdir(home, 0700)
	t.Setenv("HOME", home)
	primary := openNativePrimaryFixture(t, binary, "NODE-COMPOSITION")
	// t.TempDir on macOS may contain an ancestor alias. Production rsync requires
	// the actual profile; normalize only this contained harness profile.
	primary.app.root, err = filepath.EvalSymlinks(primary.app.root)
	if err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(root, "bin")
	_ = os.Mkdir(tools, 0700)
	script := "#!/bin/sh\nif [ \"$1\" = -G ]; then\n printf 'hostname fixture\\nuser fixture\\nport 22\\nuserknownhostsfile /dev/null\\nglobalknownhostsfile /dev/null\\n'; exit 0\nfi\nwhile [ \"$#\" -gt 0 ]; do case \"$1\" in -F|-o) shift 2;; -T) shift;; --) shift; break;; *) break;; esac; done\nshift\nexec /bin/sh -c \"$*\"\n"

	if err = os.WriteFile(filepath.Join(tools, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+":/usr/bin:/bin")
	directory := filepath.Join(root, "node")
	_ = os.Mkdir(directory, 0700)
	if _, err = nodeagent.New(nodeagent.Options{Directory: directory, NodeID: primary.pair.Target.NodeID, Join: api.NodeSSH}); err != nil {
		t.Fatal(err)
	}
	// The fixture machine is macOS; the existing remote agent command includes
	// the Linux installer directory. This contained wrapper adapts only that OS
	// flag while retaining the real serve-agent protocol and configuration owner.
	agentHelper := filepath.Join(root, "agent")
	agentScript := "#!/bin/sh\nexec " + gateQuote(helper) + " serve-agent --stdio --directory " + gateQuote(directory) + " --node-id " + gateQuote(primary.pair.Target.NodeID) + " --native-health=false\n"
	if err = os.WriteFile(agentHelper, []byte(agentScript), 0700); err != nil {
		t.Fatal(err)
	}
	settings := api.RuntimeSettings{Runtime: "codex", CLIPath: binary}
	if err = localstate.Write(filepath.Join(directory, "runtime-codex.json"), settings); err != nil {
		t.Fatal(err)
	}
	controller, err := backend.NativeNodeManagementController(primary.app.Backend)
	if err != nil {
		t.Fatal(err)
	}
	native := controller.(*nodeManagement).agent.(*nativeNodeManagement)
	native.mu.Lock()
	native.document.Nodes = []NodeRegistration{{ID: primary.pair.Target.NodeID, Label: "Contained node", Join: api.NodeSSH, SSHDestination: "fixture", Directory: directory, HelperPath: agentHelper, HostHelperPath: helper}}
	err = writeNodeManagementDocument(filepath.Join(native.directory, "config.json"), native.document)
	native.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	// Only our own freshly started owner PID is ever signalled during cleanup.
	t.Cleanup(func() {
		entries, _ := os.ReadDir(directory)
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "w") {
				continue
			}
			var marker map[string]string
			b, _ := os.ReadFile(filepath.Join(directory, entry.Name(), "owner-start.json"))
			_ = json.Unmarshal(b, &marker)
			pid, _ := strconv.Atoi(marker["pid"])
			if pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGTERM)
			}
		}
	})
	port := productrpc.ServicePort{Service: primary.app.Backend}
	opts := productrpc.Options{NodeID: "composition-source", BotID: "composition-bot", Token: strings.Repeat("c", 64), JournalFile: filepath.Join(root, "wire-receipts.json")}
	server, err := productrpc.NewServer(port, opts)
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(server)
	t.Cleanup(host.Close)
	client, err := productrpc.NewClient(productrpc.ClientOptions{URL: host.URL, ExpectedNode: opts.NodeID, ExpectedBot: opts.BotID, Token: opts.Token})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	if _, err = client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	selected := productrpc.RegisteredWorkerSelection{NodeID: primary.pair.Target.NodeID, Backend: api.NodeCodex, Revision: primary.app.Backend.WorkerNodes().Revision}
	saved, err := client.SaveWorkerNode(ctx, "composition-save-worker", selected)
	if err != nil || saved.Outcome != "accepted" {
		t.Fatal("formal Worker selection", saved.Outcome, err)
	}
	selected.Revision = saved.NodeManagement.Workers.Revision
	connected, err := client.ConnectWorkerTarget(ctx, "composition-connect-worker", selected)
	if err != nil || connected.Outcome != "accepted" || !connected.NodeManagement.Workers.Nodes[0].Connected {
		agent, lookupErr := primary.app.lookupEnrolledWorker(ctx, primary.pair)
		if lookupErr != nil {
			t.Fatal("default lookup", lookupErr)
		}
		stream, streamErr := agent.OpenWorkerStream(ctx, primary.pair)
		if streamErr != nil {
			t.Fatal("actual SSH stream", streamErr)
		}
		probe, helloErr := workerwire.NewClient(ctx, primary.pair, primary.source, stream)
		if probe != nil {
			probe.Close()
		} else {
			_ = stream.Close()
		}
		t.Fatal("default production Worker connect", connected.Outcome, err, "native hello", helloErr)
	}
	target := primary.pair.Target
	task, err := primary.app.tasks.StartTask(ctx, api.TaskStart{RequestID: "composition-native-task", Title: "Contained Worker", Prompt: "Respond briefly through the synthetic provider.", Target: &target})
	if err != nil || task.Target == nil || *task.Target != target || !strings.HasPrefix(task.Workspace, directory+"/") {
		t.Fatal("actual target Runtime task", task.Outcome, err)
	}
	notebook := backend.NotebookSyncSettings{Enabled: true, IntervalMinutes: 1, Targets: []backend.NotebookBackupTarget{{NodeID: target.NodeID, Backend: api.NodeCodex}}}
	prepared, err := client.SaveNotebookSyncSettings(ctx, "composition-save-notebook", notebook)
	if err != nil || prepared.Outcome != "accepted" || !prepared.NodeManagement.NotebookSettings.Enabled {
		_, nativeErr := primary.app.Backend.SaveNotebookSyncSettings(ctx, notebook)
		t.Fatal("production Notebook prepare", prepared.Outcome, err, "native", nativeErr)
	}
	source := filepath.Join(primary.app.root, "Notebook")
	if err = os.WriteFile(filepath.Join(source, "composition-note.md"), []byte("contained ordinary Notebook bytes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"private.sqlite", "credentials.json"} {
		if err = os.WriteFile(filepath.Join(source, name), []byte("synthetic excluded fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	synced, err := client.SyncNotebook(ctx, "composition-sync-notebook", target.NodeID)
	if err != nil || synced.Outcome != "accepted" {
		_, nativeErr := primary.app.Backend.SyncNotebook(ctx, target.NodeID)
		t.Fatal("production rsync", synced.Outcome, err, "native", nativeErr)
	}
	destination := filepath.Join(nodeagent.NotebookOwnerProfile(directory), "Notebook")
	b, err := os.ReadFile(filepath.Join(destination, "composition-note.md"))
	if err != nil || string(b) != "contained ordinary Notebook bytes\n" {
		t.Fatal("portable bytes missing", err)
	}
	for _, name := range []string{"private.sqlite", "credentials.json"} {
		if _, err = os.Stat(filepath.Join(destination, name)); !os.IsNotExist(err) {
			t.Fatal("nonportable file copied", name)
		}
	}
	state, err := primary.app.Backend.NotebookSyncState()
	if err != nil || state.Targets[0].LastSuccess == "" {
		t.Fatal("production sync status missing", err)
	}
	if _, err = os.Stat(filepath.Join(nodeagent.NotebookOwnerProfile(directory), "conversation.json")); !os.IsNotExist(err) {
		t.Fatal("standby acquired old conversation")
	}
	t.Log("ordinary APP -> authenticated settings wire -> existing SSH enrollment -> complete native Worker task; Notebook prepare, real rsync bytes and latest status passed")
}
