package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
)

// Actual APP/native-primary/strict-SSH gate. Both providers are isolated
// synthetic loopback services; no user Store/login or paid model is used.
func TestNativeCaelisWorkerAPPGate(t *testing.T) {
	filename, binary := os.Getenv("CAELIS_BOT_SSH_FIXTURE_CONFIG"), os.Getenv("CAELIS_BOT_TEST_NATIVE_PRIMARY")
	if filename == "" || binary == "" {
		t.Skip("requires prepared private SSH fixture and installed native primary CLI")
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 16384 {
		t.Fatal("private SSH fixture descriptor unavailable")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Target, Root, Helper, Store, Endpoint string
		WorkspaceRoot                         string `json:"workspace_root"`
	}
	if json.Unmarshal(raw, &config) != nil || !regexp.MustCompile(`^/tmp/caelis-bot-issue47-worker-[a-f0-9]{16}$`).MatchString(config.Root) || !workerSSH.MatchString(config.Target) || strings.HasPrefix(config.Target, "-") || config.Helper != config.Root+"/worker-bootstrap" || config.Store != config.Root+"/store" || config.WorkspaceRoot != config.Root+"/workspaces" {
		t.Fatal("invalid owned native fixture descriptor")
	}
	primary := openNativePrimaryFixture(t, binary, "rocky-app-gate")
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	actual, err := primary.source.WorkDispatchSource(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Control carries only fixture counts/release, never the target management token.
	control := func(method, path string) map[string]any {
		t.Helper()
		quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
		code := "import http.client,json,urllib.parse; u=urllib.parse.urlparse(" + stringJSON(config.Endpoint) + "); c=http.client.HTTPConnection(u.hostname,u.port,timeout=10); c.request(" + stringJSON(method) + ",'/fixture/'+" + stringJSON(path) + "); r=c.getresponse(); assert r.status==200; print(r.read().decode())"
		args := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UpdateHostKeys=no", "-o", "ForwardAgent=no", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ConnectTimeout=15", "--", config.Target, "python3 -c " + quote(code)}
		command := exec.CommandContext(ctx, "/usr/bin/ssh", args...)
		command.Stderr = io.Discard
		bytes, e := command.Output()
		if e != nil || len(bytes) > 65536 {
			t.Fatal("isolated fixture control unavailable")
		}
		var out map[string]any
		if json.Unmarshal(bytes, &out) != nil {
			t.Fatal("isolated fixture control malformed")
		}
		return out
	}
	node := backend.WorkerNodeConfig{ID: "rocky-app-gate", Label: "Isolated Caelis native gate", Backend: "caelis", SSH: config.Target, Helper: config.Helper, Store: config.Store, WorkspaceRoot: config.WorkspaceRoot}
	setup, err := primary.app.Backend.SaveWorkerNode(node, primary.app.Backend.WorkerNodes().Revision)
	if err != nil {
		t.Fatal(err)
	}
	setup, err = primary.app.Backend.ConnectWorkerTarget(ctx, configuredWorkerTarget(node), setup.Revision)
	if err != nil || len(setup.Nodes) != 1 || setup.Nodes[0].State != "ready" {
		t.Fatal("actual APP native Worker route not ready", err)
	}
	target := api.WorkTarget{NodeID: node.ID, Backend: "caelis", Role: api.RoleWorker}
	request := api.TaskStart{Target: &target, RequestID: "caelis-app-native-artifact", Title: "Native APP artifact", Prompt: "CASE_SSH_ARTIFACT"}
	task, startErr := primary.app.tasks.StartTask(ctx, request)
	if task.ID == "" || task.Target == nil || *task.Target != target {
		t.Fatal("APP native-source start lost original intent", startErr)
	}
	initialID := task.ID
	// The real proxy drops the committed native prompt response. The APP observer
	// may already reconcile its exact operation before StartTask returns.
	stats := control("GET", "state")
	if stats["host_alive"] != true || stats["artifact_completion_held"] != true {
		t.Fatal("owned native artifact execution was not held during dispatch")
	}
	dropped, _ := stats["dropped"].([]any)
	found := false
	for _, v := range dropped {
		if v == "prompt" {
			found = true
		}
	}
	if !found {
		t.Fatal("actual committed prompt reply loss was not exercised")
	}
	setup, err = primary.app.Backend.DisconnectWorkerTarget(ctx, configuredWorkerTarget(node), setup.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if control("GET", "state")["host_alive"] != true {
		t.Fatal("APP detach stopped native Host")
	}
	setup, err = primary.app.Backend.ConnectWorkerTarget(ctx, configuredWorkerTarget(node), setup.Revision)
	if err != nil || setup.Nodes[0].State != "ready" {
		t.Fatal("APP same-owner reconnect failed", err)
	}
	replay, err := primary.app.tasks.StartTask(ctx, request)
	if err != nil || replay.ID != initialID {
		t.Fatal("APP original receipt moved", err)
	}
	changed := request
	changed.Prompt += " changed"
	if _, err = primary.app.tasks.StartTask(ctx, changed); err == nil {
		t.Fatal("changed original intent accepted")
	}
	route, err := primary.app.nodeRegistry.WorkRuntimeFor(target)
	if err != nil {
		t.Fatal(err)
	}
	activeDeadline := time.After(10 * time.Second)
	for {
		active, activeErr := primary.app.tasks.ReadTask(ctx, initialID)
		if activeErr == nil && active.Status == "working" {
			break
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-activeDeadline:
			t.Fatal("APP detach did not preserve active native task", active.Status, activeErr)
		}
	}
	control("POST", "release/CASE_SSH_ARTIFACT")
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err = primary.app.tasks.ReadTask(ctx, initialID)
		if err == nil && task.Status == "completed" {
			break
		}
		if task.Status == "failed" || task.Status == "interrupted" {
			t.Fatal("native artifact task did not complete", task.Status)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("native artifact deadline", task.Status, task.Outcome)
		}
	}
	var artifact api.WorkArtifact
	artifactDeadline := time.After(20 * time.Second)
	for artifact.ID == "" {
		for _, ref := range route.(api.WorkArtifactCatalog).WorkArtifacts() {
			if ref.TaskID == initialID && ref.Target == target {
				artifact, err = route.(api.WorkArtifactProvider).ReadWorkArtifact(ctx, initialID, ref.Artifact.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		if artifact.ID != "" {
			break
		}
		select {
		case <-ticker.C:
		case <-artifactDeadline:
			t.Fatal("canonical native artifact catalog deadline", "references", len(route.(api.WorkArtifactCatalog).WorkArtifacts()), "taskStatus", task.Status)
		}
	}
	if string(artifact.Bytes) != "SSH_NATIVE_ARTIFACT_SENTINEL" || artifact.SHA256 != nativeIDHash(string(artifact.Bytes)) || artifact.Size != int64(len(artifact.Bytes)) {
		t.Fatal("canonical native artifact missing or integrity changed")
	}
	if _, err = route.(api.WorkArtifactProvider).ReadWorkArtifact(ctx, "unowned", artifact.ID); err == nil {
		t.Fatal("artifact changed owned task")
	}
	// Compare actual primary attestation with both product intent and native
	// adapter journal; no static source or raw native IDs enter public evidence.
	ledgerRaw, err := os.ReadFile(filepath.Join(primary.app.root, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ledger struct {
		Records map[string]struct {
			Source        api.WorkDispatchSource
			RequestDigest string
		}
	}
	if json.Unmarshal(ledgerRaw, &ledger) != nil {
		t.Fatal("product intent unreadable")
	}
	retained := ledger.Records[initialID]
	if retained.Source != actual || retained.RequestDigest == "" {
		t.Fatal("product intent replaced actual native primary source")
	}
	privateDir := filepath.Join(primary.app.root, "worker-nodes", node.ID)
	adapterRaw, err := os.ReadFile(filepath.Join(privateDir, "worker-application.json"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Workers map[string]struct {
			Source        api.WorkDispatchSource
			RequestDigest string
			Native        bool
			Binding       struct {
				SessionID string `json:"session_id"`
			}
		}
	}
	if json.Unmarshal(adapterRaw, &adapter) != nil {
		t.Fatal("native adapter journal unreadable")
	}
	binding := adapter.Workers[initialID]
	if len(adapter.Workers) != 1 || binding.Source != actual || binding.RequestDigest != retained.RequestDigest || binding.Native || binding.Binding.SessionID == "" {
		t.Fatal("bounded native binding/source drift")
	}
	nativeSession := binding.Binding.SessionID
	if err = primary.app.engine.Interrupt(ctx); err != nil {
		t.Fatal("actual primary interrupt failed", err)
	}
	if _, err = primary.source.WorkDispatchSource(ctx); err == nil {
		t.Fatal("ended native primary still attested source")
	}
	original := api.WorkStart{TaskStart: request, ID: initialID, Workspace: task.Workspace, Instructions: botpolicy.WorkerInstructions, Source: retained.Source, RequestDigest: retained.RequestDigest}
	if v, e := route.StartWork(ctx, original); e != nil || v.ID != initialID {
		t.Fatal("original native receipt requires new activation", e)
	}
	original.RequestDigest = strings.Repeat("0", 64)
	if _, err = route.StartWork(ctx, original); err == nil {
		t.Fatal("changed original digest accepted")
	}
	fresh := request
	fresh.RequestID = "must-not-dispatch-ended-primary"
	if _, err = primary.app.tasks.StartTask(ctx, fresh); err == nil {
		t.Fatal("fresh Worker admitted ended primary")
	}
	stats = control("GET", "state")
	counts := stats["request_counts"].(map[string]any)
	if counts["create"] != float64(1) || counts["prompt"] != float64(1) || stats["host_alive"] != true {
		t.Fatal("native replay replaced or resent task")
	}
	t.Log("actual APP background→native Codex source→strictSSH→bounded Caelis artifact passed", "primaryBindingSHA256", nativeIDHash(actual.BindingID), "primaryOperationSHA256", nativeIDHash(actual.OperationID), "taskSHA256", nativeIDHash(initialID), "nativeSessionSHA256", nativeIDHash(nativeSession), "artifactSHA256", artifact.SHA256, "createCount", 1, "promptCount", 1, "detachReconnect", true, "realModel", false, "GUI", false)
}
func stringJSON(v string) string { b, _ := json.Marshal(v); return string(b) }
