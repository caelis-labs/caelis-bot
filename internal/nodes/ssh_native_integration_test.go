package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
)

// This opt-in test uses actual strict OpenSSH and an official target-native
// Caelis Host. Only the primary-source attestation and loopback model provider
// are synthetic; no real provider login, user Store or billable model is used.
type nativeSSHFixtureSource struct{ source api.WorkDispatchSource }

func (s nativeSSHFixtureSource) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	return s.source, nil
}
func TestSSHNativeWorkerIntegration(t *testing.T) {
	configPath := os.Getenv("CAELIS_BOT_SSH_FIXTURE_CONFIG")
	if configPath == "" {
		t.Skip("set CAELIS_BOT_SSH_FIXTURE_CONFIG for approved isolated strict SSH acceptance")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("private fixture config unavailable")
	}
	var config struct {
		Target, Root, Helper, Store, Endpoint string
		WorkspaceRoot                         string `json:"workspace_root"`
	}
	if json.Unmarshal(raw, &config) != nil || !strings.HasPrefix(config.Root, "/tmp/caelis-bot-issue47-worker-") {
		t.Fatal("invalid isolated fixture config")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	target := api.WorkTarget{NodeID: "local-VM", Backend: "caelis", Role: api.RoleWorker}
	source := api.WorkDispatchSource{NodeID: api.LocalNodeID, Backend: "codex", BindingID: "synthetic-native-primary", OperationID: "synthetic-native-activation", Kind: "native_activation"}
	directory := filepath.Join(t.TempDir(), "private-worker")
	var transport *SSHWorker
	var worker *caelis.WorkerClient
	open := func() {
		var e error
		transport, e = NewSSHWorker(SSHConfig{Target: config.Target, Helper: config.Helper, Store: config.Store, WorkspaceRoot: config.WorkspaceRoot, Binary: "/usr/bin/ssh"})
		if e != nil {
			t.Fatal(e)
		}
		ready, probeErr := transport.Probe(ctx)
		if probeErr != nil || ready.OS != "linux" || ready.Arch != "arm64" || ready.StoreID == "" || ready.InstanceID == "" || ready.PrincipalID == "" {
			t.Fatal("actual target-native public initialization provenance unavailable")
		}
		for _, required := range []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-resource-transfer-v1"} {
			found := false
			for _, capability := range ready.Capabilities {
				if capability == required {
					found = true
				}
			}
			if !found {
				t.Fatal("actual target-native minimum capability unavailable", required)
			}
		}
		worker = caelis.NewWorker(caelis.WorkerOptions{Target: target, Directory: directory, Endpoint: transport.Endpoint, Source: nativeSSHFixtureSource{source}, Workspace: transport})
		if e = worker.Connect(ctx); e != nil {
			t.Fatal("native SSH scoped connect:", e)
		}
	}
	open()
	defer func() { worker.Close(context.Background()); transport.Close() }()
	if configured, report := worker.ModelReadiness(); !configured || report == "reported_missing" {
		t.Fatal("synthetic model configuration missing", configured, report)
	}
	// A separate strict SSH tunnel reaches the test-only target-local control
	// endpoint. It exports statistics/release only, never Host management bytes.
	controller, e := transport.openTunnel(config.Endpoint)
	if e != nil {
		t.Fatal(e)
	}
	defer controller.Close()
	control := func(method, path string) map[string]any {
		req, e := http.NewRequestWithContext(ctx, method, controller.Origin()+"/fixture/"+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal("fixture control unavailable")
		}
		defer res.Body.Close()
		var v map[string]any
		if json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&v) != nil {
			t.Fatal("fixture control incompatible")
		}
		return v
	}
	wait := func(label string, check func() bool) {
		t.Helper()
		stage, stageCancel := context.WithTimeout(ctx, 30*time.Second)
		defer stageCancel()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for !check() {
			select {
			case <-stage.Done():
				for _, state := range worker.WorkStates() {
					t.Logf("synthetic task %s status=%s outcome=%s native_generation_present=%t", state.Task.ID, state.Task.Status, state.Task.Outcome, state.ExecutionKey != "")
				}
				t.Logf("projection phase=%s message=%s", worker.Snapshot().Phase, worker.Snapshot().Message)
				t.Fatal("native SSH timeout:", label)
			case <-tick.C:
			}
		}
	}
	digest := func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	start := func(id, marker string) api.WorkStart {
		workspace, e := worker.ResolveWorkWorkspace(ctx, id, "")
		if e != nil {
			t.Fatal("target workspace resolution:", e)
		}
		in := api.WorkStart{ID: id, Workspace: workspace, Source: source, RequestDigest: digest(id + marker), TaskStart: api.TaskStart{RequestID: "fixture-" + id, Title: id, Prompt: marker, Target: &target}}
		// Explicit durable host ledger before target workspace mutation.
		ledger, _ := json.Marshal(struct {
			Source api.WorkDispatchSource
			Intent api.WorkStart
		}{source, in})
		if e = os.WriteFile(filepath.Join(directory, id+"-intent.json"), ledger, 0600); e != nil {
			t.Fatal(e)
		}
		if e = worker.PrepareWorkWorkspace(ctx, id, workspace, false); e != nil {
			t.Fatal("target workspace prepare:", e)
		}
		_, _ = worker.StartWork(ctx, in)
		wait("start receipt "+id, func() bool { _, e := worker.StartWork(ctx, in); return e == nil })
		return in
	}
	state := func(id string) api.Task {
		v, e := worker.ReadWork(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	settled := func(prefix string) bool {
		raw, e := os.ReadFile(filepath.Join(directory, "worker-application.json"))
		if e != nil {
			return false
		}
		var ledger struct {
			Operations map[string]struct {
				Outcome string `json:"outcome"`
			} `json:"operations"`
		}
		if json.Unmarshal(raw, &ledger) != nil {
			return false
		}
		for id, op := range ledger.Operations {
			if strings.HasPrefix(id, prefix) && op.Outcome == "committed" {
				return true
			}
		}
		return false
	}
	start("artifact", "CASE_SSH_ARTIFACT")
	wait("artifact task complete", func() bool {
		v := state("artifact")
		if v.Status == "failed" {
			t.Fatal("native artifact task failed:", v.Result)
		}
		return v.Status == "completed"
	})
	t.Run("canonical_artifact_transfer", func(t *testing.T) {
		// Preserve the actual 0.65.0 Worker gate: application resource support
		// currently does not install PublishArtifact on shared native Workers.
		// A native failed tool result cannot become an artifact from prose/path.
		evidence := control("GET", "artifact-status")
		if evidence["native_version"] == "0.65.0" && evidence["tool"] == "PublishArtifact" && evidence["status"] == "failed" && evidence["error_code"] == "not_found" {
			if len(worker.WorkArtifacts()) != 0 {
				t.Fatal("failed native publication invented an artifact")
			}
			t.Skip("official Core 0.65.0 shared Worker PublishArtifact is absent; native artifact acceptance blocked")
		}
		var artifact api.WorkArtifactRef
		wait("canonical artifact catalog", func() bool {
			for _, a := range worker.WorkArtifacts() {
				if a.TaskID == "artifact" {
					artifact = a
					return true
				}
			}
			return false
		})
		bytes, e := worker.ReadWorkArtifact(ctx, "artifact", artifact.Artifact.ID)
		if e != nil || string(bytes.Bytes) != "SSH_NATIVE_ARTIFACT_SENTINEL" {
			t.Fatal("native artifact ownership/checksum download failed", e)
		}
		if _, e = worker.ReadWorkArtifact(ctx, "other", artifact.Artifact.ID); e == nil {
			t.Fatal("cross-task artifact accepted")
		}
	})
	stats := control("GET", "state")
	if stats["request_counts"].(map[string]any)["prompt"].(float64) != 1 {
		t.Fatal("lost native prompt redispatched")
	}
	t.Log("actual SSH: target-side enrollment, Worker-only start, lost prompt receipt passed; artifact gate separately reported")
	start("approval", "CASE_SSH_APPROVAL")
	t.Run("native_human_approval_receipt", func(t *testing.T) {
		evidence := control("GET", "approval-status")
		if evidence["native_version"] == "0.65.0" && evidence["status"] == "failed" && evidence["error_code"] == "approval_unavailable" {
			if len(worker.WorkApprovals()) != 0 {
				t.Fatal("native failed automatic review invented a human approval")
			}
			t.Skip("official Core 0.65.0 shared Worker automatic review has no configured fixture reviewer; manual approval/lost approval reply remains unverified")
		}
		var approval api.WorkApproval
		wait("native worker approval", func() bool {
			for _, a := range worker.WorkApprovals() {
				if a.TaskID == "approval" {
					approval = a
					return true
				}
			}
			return false
		})
		var choice string
		for _, c := range approval.Approval.Choices {
			if strings.Contains(strings.ToLower(c.ID+" "+c.Label), "allow") || strings.Contains(strings.ToLower(c.ID), "approve") {
				choice = c.ID
				break
			}
		}
		if choice == "" {
			t.Fatal("native approval has no allow choice")
		}
		wrong := approval
		wrong.TaskID = "other"
		decision := api.Decision{ID: approval.Approval.ID, Choice: choice}
		if worker.DecideWork(ctx, wrong, decision) == nil {
			t.Fatal("cross-task approval accepted")
		}
		_ = worker.DecideWork(ctx, approval, decision)
		wait("lost approval receipt", func() bool { return settled("approval-") })
		wait("approved task complete", func() bool { return state("approval").Status == "completed" })
		t.Log("actual SSH: exact Worker approval and lost native approval receipt passed")
	})
	start("cancel", "CASE_SSH_CANCEL")
	wait("provider entered cancel", func() bool { return control("GET", "state")["model_counts"].(map[string]any)["CASE_SSH_CANCEL"] != nil })
	_, _ = worker.StopWork(ctx, "cancel")
	t.Run("lost_native_cancel_receipt", func(t *testing.T) {
		evidence := control("GET", "cancel-receipt")
		if evidence["native_version"] == "0.65.0" && evidence["native_response_outcome"] == "committed" && evidence["native_recovery_http"] == float64(404) && evidence["response_dropped"] == true {
			if settled("cancel-") {
				t.Fatal("missing native receipt was falsely settled")
			}
			t.Skip("official Core 0.65.0 committed cancel missing from scoped application operation recovery; exact receipt remains unknown")
		}
		wait("lost cancel receipt", func() bool { return settled("cancel-") })
	})
	wait("native cancelled target", func() bool { return state("cancel").Status == "interrupted" })
	control("POST", "release/CASE_SSH_CANCEL")
	t.Log("actual SSH: exact Worker cancellation observed; lost cancel receipt gate separately reported")
	start("detach", "CASE_SSH_DETACH")
	wait("provider entered detached task", func() bool { return control("GET", "state")["model_counts"].(map[string]any)["CASE_SSH_DETACH"] != nil })
	worker.Close(context.Background())
	transport.Close()
	if control("GET", "state")["host_alive"] != true {
		t.Fatal("detach stopped shared Host")
	}
	control("POST", "release/CASE_SSH_DETACH")
	open()
	wait("same-grant reconnect completion", func() bool { return state("detach").Status == "completed" })
	stats = control("GET", "state")
	if stats["request_counts"].(map[string]any)["create"].(float64) != 4 {
		t.Fatal("reconnect replaced native worker grant")
	}
	if stats["request_counts"].(map[string]any)["cancel"].(float64) != 1 {
		t.Fatal("uncertain native mutations redispatched")
	}
	t.Log("actual SSH: detach left Host/task alive; reconnect retained all grants without redispatch; synthetic provider only")
}
