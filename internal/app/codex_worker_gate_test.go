package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type nativeWorkerGate struct {
	PrimaryBinary string `json:"primaryBinary"`
	SSH           string `json:"ssh"`
	Directory     string `json:"directory"`
	NativeBinary  string `json:"nativeBinary"`
}

func gateSSHArgs(destination string) []string {
	return []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UpdateHostKeys=no", "-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ConnectTimeout=15", "--", destination}
}
func gateQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// Opt-in paid evidence: the target uses only its existing native login and the
// user-authorized gpt-6-luna / medium. The primary native provider stays loopback.
// Setup address/path input is a private harness file, never model/renderer input.
func TestNativeCodexWorkerSSHGate(t *testing.T) {
	filename := os.Getenv("CAELIS_BOT_TEST_NATIVE_WORKER_GATE")
	if filename == "" {
		t.Skip("requires explicit prepared private native Worker gate configuration")
	}
	bytes, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var gate nativeWorkerGate
	if json.Unmarshal(bytes, &gate) != nil || !workerSSH.MatchString(gate.SSH) || strings.HasPrefix(gate.SSH, "-") || !regexp.MustCompile(`^/tmp/caelis-worker-gate-[0-9a-f]{16}$`).MatchString(gate.Directory) || !filepath.IsAbs(gate.NativeBinary) || !filepath.IsAbs(gate.PrimaryBinary) {
		t.Fatal("invalid explicit native gate configuration")
	}
	primary := openNativePrimaryFixture(t, gate.PrimaryBinary, "linux-gate")
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	// One foreground supervisor owns the native CLI, separately from proxy observers.
	process := exec.Command("/usr/bin/ssh", append(gateSSHArgs(gate.SSH), "exec python3 "+gateQuote(gate.Directory+"/supervisor.py")+" "+gateQuote(gate.Directory))...)
	input, err := process.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	process.Stderr = io.Discard
	if err = process.Start(); err != nil {
		t.Fatal("owned native gate transport failed")
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	reader := bufio.NewReader(output)
	read := func(value any) error {
		line, e := reader.ReadBytes('\n')
		if e != nil {
			return e
		}
		if len(line) > 16384 {
			return errors.New("native gate metadata limit")
		}
		return json.Unmarshal(line, value)
	}
	var stopped struct {
		OwnerStopped, NativeChildrenStopped bool
		OwnedNativeChildren                 int
		ModelSettings                       []struct{ Model, Effort string }
	}
	cleanup := func() {
		_, _ = io.WriteString(input, "{\"stop\":true}\n")
		_ = input.Close()
		if e := read(&stopped); e != nil {
			t.Error("native gate shutdown metadata unavailable")
		}
		select {
		case e := <-done:
			if e != nil {
				t.Error("native gate supervisor exit failed")
			}
		case <-time.After(45 * time.Second):
			_ = process.Process.Kill()
			t.Error("native gate supervisor shutdown exceeded bound")
		}
		if !stopped.OwnerStopped || !stopped.NativeChildrenStopped || stopped.OwnedNativeChildren < 1 {
			t.Error("owned native Worker shutdown was not confirmed")
		}
		for _, settings := range stopped.ModelSettings {
			if settings.Model != "gpt-6-luna" || settings.Effort != "medium" {
				t.Error("native execution differed from authorized model/effort")
			}
		}
		t.Log("owned supervisor and captured native children stopped", stopped.OwnerStopped, stopped.NativeChildrenStopped, "authorized model", "gpt-6-luna", "effort", "medium")
	}
	t.Cleanup(cleanup)
	config := struct {
		Version   int                       `json:"version"`
		Pair      workerwire.Pair           `json:"pair"`
		Directory string                    `json:"directory"`
		Binary    string                    `json:"binary"`
		Socket    string                    `json:"socket"`
		Execution api.WorkExecutionSettings `json:"execution"`
	}{1, primary.pair, gate.Directory + "/owner", gate.NativeBinary, gate.Directory + "/owner/worker.sock", api.WorkExecutionSettings{Model: "gpt-6-luna", Effort: "medium"}}
	if err = json.NewEncoder(input).Encode(config); err != nil {
		t.Fatal("native configuration delivery failed")
	}
	var ready struct {
		Version int
		Socket  string
		Pair    workerwire.Pair
	}
	if err = read(&ready); err != nil || ready.Version != 1 || ready.Socket != config.Socket || ready.Pair != primary.pair {
		t.Fatal("native Worker readiness mismatch")
	}
	node := backend.WorkerNodeConfig{ID: "linux-gate", Label: "Isolated native Worker", Backend: "codex", SSH: gate.SSH, Helper: gate.Directory + "/node", Socket: config.Socket, WorkspaceRoot: config.Directory + "/Tasks"}
	setup, err := primary.app.Backend.SaveWorkerNode(node, primary.app.Backend.WorkerNodes().Revision)
	if err != nil {
		t.Fatal("explicit APP node setup failed", err)
	}
	setup, err = primary.app.Backend.ConnectWorkerNode(ctx, node.ID, setup.Revision)
	if err != nil || len(setup.Nodes) != 1 || setup.Nodes[0].State != "ready" {
		t.Fatal("exact paired APP route unavailable")
	}
	target := primary.pair.Target
	request := api.TaskStart{Target: &target, RequestID: "native-artifact-original", Title: "Native artifact gate", Prompt: "Create artifact.txt in this task workspace with exactly the text CODEX_NATIVE_WORKER_ARTIFACT and a trailing newline. Use apply_patch to create it so the native file change is recorded. Then give a brief completion message. Do not read credentials, contact the network, or modify any other directory."}
	task, err := primary.app.tasks.StartTask(ctx, request)
	if err != nil || task.ID == "" || task.Target == nil || *task.Target != target {
		t.Fatal("actual native-source Worker dispatch failed", err)
	}
	initialID := task.ID
	// Disconnect the APP observer while native execution owns the request.
	setup, err = primary.app.Backend.DisconnectWorkerNode(ctx, node.ID, setup.Revision)
	if err != nil {
		t.Fatal("APP observer detach failed")
	}
	setup, err = primary.app.Backend.ConnectWorkerNode(ctx, node.ID, setup.Revision)
	if err != nil || setup.Nodes[0].State != "ready" {
		t.Fatal("exact native owner reconnect failed")
	}
	replay, err := primary.app.tasks.StartTask(ctx, request)
	if err != nil || replay.ID != initialID {
		t.Fatal("original native request did not reconcile", err)
	}
	changed := request
	changed.Prompt += " changed"
	if _, err = primary.app.tasks.StartTask(ctx, changed); err == nil {
		t.Fatal("changed original intent was admitted")
	}
	route, err := primary.app.nodeRegistry.WorkRuntimeFor(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = route.ReadWork(ctx, "task-"+strings.Repeat("f", 32)); err == nil {
		t.Fatal("unowned task read accepted")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err = primary.app.tasks.ReadTask(ctx, initialID)
		if err != nil {
			t.Fatal("native receipt observation failed", err)
		}
		if task.Status == "completed" {
			break
		}
		if task.Status == "failed" || task.Status == "interrupted" {
			t.Fatal("native task ended without artifact completion", task.Status)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("native task completion deadline", task.Status, task.Outcome)
		}
	}
	refs := route.(api.WorkArtifactCatalog).WorkArtifacts()
	var artifact api.WorkArtifact
	for _, ref := range refs {
		if ref.TaskID == initialID && ref.Target == target && ref.Artifact.Name == "artifact.txt" {
			artifact, err = route.(api.WorkArtifactProvider).ReadWorkArtifact(ctx, initialID, ref.Artifact.ID)
			if err != nil {
				t.Fatal("owned canonical artifact read failed", err)
			}
		}
	}
	if string(artifact.Bytes) != "CODEX_NATIVE_WORKER_ARTIFACT\n" || artifact.SHA256 != nativeIDHash(string(artifact.Bytes)) || artifact.Size != int64(len(artifact.Bytes)) {
		t.Fatal("canonical native artifact missing or integrity changed")
	}
	// End the original native activation. Original receipts remain readable, but
	// no fresh mutation may use its stale native authority.
	if err = primary.app.engine.Interrupt(ctx); err != nil {
		t.Fatal("primary native stop failed", err)
	}
	if _, err = primary.source.WorkDispatchSource(ctx); err == nil {
		t.Fatal("ended primary activation still authorized fresh mutations")
	}
	replay, err = primary.app.tasks.StartTask(ctx, request)
	if err != nil || replay.ID != initialID {
		t.Fatal("ended primary source rejected original receipt reconciliation", err)
	}
	ledgerBytes, err := os.ReadFile(filepath.Join(primary.app.root, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ledger struct {
		Records map[string]struct {
			RequestDigest string
			Source        api.WorkDispatchSource
		}
	}
	if err = json.Unmarshal(ledgerBytes, &ledger); err != nil {
		t.Fatal(err)
	}
	stored, exists := ledger.Records[initialID]
	if !exists || stored.Source.Validate() != nil || stored.RequestDigest == "" {
		t.Fatal("original native source/digest not retained")
	}
	original := api.WorkStart{TaskStart: request, ID: initialID, Workspace: task.Workspace, Source: stored.Source, RequestDigest: stored.RequestDigest, Instructions: botpolicy.WorkerInstructions}
	originalReceipt, err := route.StartWork(ctx, original)
	if err != nil || originalReceipt.ID != initialID {
		t.Fatal("native target rejected original receipt after primary end", err)
	}
	original.RequestDigest = strings.Repeat("0", 64)
	if _, err = route.StartWork(ctx, original); err == nil {
		t.Fatal("changed original native digest accepted")
	}

	fresh := request
	fresh.RequestID = "must-not-dispatch-after-primary-end"
	if _, err = primary.app.tasks.StartTask(ctx, fresh); err == nil {
		t.Fatal("fresh mutation admitted after primary native source ended")
	}
	t.Log(fmt.Sprintf("native APP→strictSSH→Worker artifact gate passed; target=codex/worker, originalTaskDigest=%s, artifactSHA256=%s, detach/reconnect=true, originalReplay=true, changedIntentRejected=true, foreignReadRejected=true, endedSourceFreshRejected=true", nativeIDHash(initialID), artifact.SHA256))
}
