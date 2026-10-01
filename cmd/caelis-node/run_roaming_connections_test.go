package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

func TestRoamingSettingsMetadataSurvivesUnavailableWorkerWithoutGrantingRoute(t *testing.T) {
	c := roamingCommand{NodeID: "target-node", BotID: "native-bot", Backend: "codex", CodexBinary: "/native/codex"}
	binding := roamingWorkerRuntime{Backend: "caelis", Binary: "/native/caelis", Store: "/native/designated-store"}
	p := roamingWorkerPlan{Version: 1, Sources: []roamingWorkerSource{{NodeID: c.NodeID, Backends: []string{"codex"}}}, Runtimes: []roamingWorkerRuntime{}, SettingsRuntimes: []roamingWorkerRuntime{binding}}
	filename := filepath.Join(t.TempDir(), "workers.json")
	write := func() {
		t.Helper()
		body, e := json.Marshal(p)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filename, body, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write()
	loaded, e := loadRoamingWorkerPlan(filename)
	if e != nil {
		t.Fatal(e)
	}
	metadata, e := roamingNodeRuntimeSettings(c, loaded, api.NodeCaelis)
	if e != nil || metadata.Binary != binding.Binary || metadata.Store != binding.Store {
		t.Fatal("designated setup slot lost after Worker rejection", metadata, e)
	}
	workers := newRoamingOwnedWorkers(t.Context(), c, loaded, "", nil, nil)
	defer workers.Close()
	if len(workers.runtimes) != 0 {
		t.Fatal("settings metadata admitted a Runtime")
	}
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: c.NodeID, Backend: "caelis", Role: api.RoleWorker}, BotID: api.ProfileBotID(c.BotID), SourceNode: c.NodeID, SourceBackend: c.Backend}
	if _, e = workers.resolve(t.Context(), pair); e == nil {
		t.Fatal("metadata-only Caelis became a Worker")
	}
	p.SettingsRuntimes[0].Store = "relative-store"
	write()
	if _, e = loadRoamingWorkerPlan(filename); e == nil {
		t.Fatal("redirected setup slot accepted")
	}
	p.SettingsRuntimes[0] = binding
	p.Runtimes = []roamingWorkerRuntime{{Backend: "caelis", Binary: binding.Binary, Store: "/different/store"}}
	write()
	if _, e = loadRoamingWorkerPlan(filename); e == nil {
		t.Fatal("setup and admitted Runtime designation diverged")
	}
}
