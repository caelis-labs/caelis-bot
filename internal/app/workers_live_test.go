package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in real inference through private WorkOwners, without touching daily Bot
// bindings or changing global runtime configuration/account credentials.
func TestLocalNativeDefaultSwitch(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_LOCAL_WORKERS") != "1" {
		t.Skip("set CAELIS_BOT_TEST_LOCAL_WORKERS for native acceptance")
	}
	root := t.TempDir()
	if base := os.Getenv("CAELIS_BOT_TEST_LOCAL_EVIDENCE"); base != "" {
		if e := os.MkdirAll(base, 0700); e != nil {
			t.Fatal(e)
		}
		var e error
		root, e = os.MkdirTemp(base, "native-")
		if e != nil {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	codexDir := filepath.Join(root, "codex")
	caelisDir := filepath.Join(root, "caelis")
	c, e := codex.NewWorkOwner(codex.SessionOptions{Directory: codexDir, StateFile: filepath.Join(codexDir, "bindings.json"), WorkRoot: filepath.Join(root, "Tasks"), Execution: api.ExecutionSettings{ApprovalMode: "auto"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Connect(ctx); e != nil {
		t.Fatal("Codex connection", e)
	}
	a, e := caelis.NewWorkOwner(caelis.Options{Directory: caelisDir, Settings: api.RuntimeSettings{Runtime: "caelis", CaelisStore: os.Getenv("CAELIS_BOT_TEST_LOCAL_CAELIS_STORE")}})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close(context.Background())
	defer a.Close(context.Background())
	w := &localWorkers{root: root, path: filepath.Join(root, "worker-runtime.json"), active: "codex", defaultRuntime: "codex", resident: c, routes: map[string]string{}, owners: map[string]retainedWorker{}, open: func(string) (retainedWorker, error) { return a, nil }}
	ids := map[string]string{}
	bindings := map[string]api.TerminalTarget{}
	for _, runtime := range []string{"codex", "caelis"} {
		view, err := w.InspectLocalWorker(ctx, runtime)
		if err != nil {
			e = err
			t.Fatal("local default", runtime, e)
		}
		t.Logf("runtime=%s native default=%+v", runtime, view.RuntimeDefault)
		model := os.Getenv("CAELIS_BOT_TEST_LOCAL_" + map[string]string{"codex": "CODEX", "caelis": "CAELIS"}[runtime] + "_MODEL")
		if model != "" {
			selection := api.WorkExecutionSettings{Model: model}
			for _, option := range view.Models {
				if option.Model == model {
					selection.Effort = option.DefaultEffort
				}
			}
			if _, e = w.SaveLocalWorkerModel(ctx, selection); e != nil {
				t.Fatal("private worker model", e)
			}
		}
		sum := sha256.Sum256([]byte(runtime + time.Now().String()))
		id := "task-" + hex.EncodeToString(sum[:16])
		ids[runtime] = id
		workspace := filepath.Join(root, "Tasks", id)
		if e = os.MkdirAll(workspace, 0700); e != nil {
			t.Fatal(e)
		}
		in := api.WorkStart{TaskStart: api.TaskStart{RequestID: "local-switch-" + runtime, Title: "Local default acceptance", Prompt: "Run exactly one shell command: sleep 4; printf 'LOCAL_SWITCH_OK\\n' > switch.txt. Do not create other files. Reply done."}, ID: id, Workspace: workspace, Instructions: "Perform this one-command native acceptance precisely."}
		if _, e = w.BindWork(ctx, in.TaskStart, id, ""); e != nil {
			t.Fatal(e)
		}
		if _, e = w.StartWork(ctx, in); e != nil {
			t.Fatal("native task start", runtime, e)
		}
		bindings[runtime], e = w.WorkTerminal(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
	}
	wait := func(id string) {
		t.Helper()
		for {
			v, e := w.ReadWork(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			if v.Status == "completed" {
				bytes, e := os.ReadFile(filepath.Join(v.Workspace, "switch.txt"))
				if e != nil || string(bytes) != "LOCAL_SWITCH_OK\n" {
					t.Fatal("exact artifact readback", e)
				}
				return
			}
			if v.Status == "failed" || v.Status == "waiting_approval" || v.Status == "awaiting_approval" {
				t.Fatalf("runtime=%s unexpected native status=%s result=%q", w.routes[id], v.Status, v.Result)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(time.Second):
			}
		}
	}
	for _, id := range ids {
		wait(id)
	}
	if _, e = w.InspectLocalWorker(ctx, "codex"); e != nil {
		t.Fatal("reverse default switch", e)
	}
	if _, e = w.SendWork(ctx, api.TaskMessage{ID: ids["caelis"], RequestID: "continue-local-caelis", Prompt: "Read switch.txt and report its exact content. Do not write files."}); e != nil {
		t.Fatal("old native continuation", e)
	}
	wait(ids["caelis"])
	for runtime, id := range ids {
		native, e := w.WorkTerminal(ctx, id)
		if e != nil || native.Runtime != runtime || native.Thread != bindings[runtime].Thread || native.Session != bindings[runtime].Session {
			t.Fatal("original local target replaced", runtime, e)
		}
	}
	t.Log("local native Codex/Caelis defaults, simultaneous owned tasks, exact bytes, old Caelis continuation under Codex default and original terminals passed")
}
