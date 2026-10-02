package caelis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureWorkSource struct{ source api.WorkDispatchSource }

func (f fixtureWorkSource) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	return f.source, nil
}

// Synthetic public Control Host: no CLI, real model, or remote machine is used.
func TestWorkerOnlyScopedEnrollmentReceiptsApprovalCancelAndReconnect(t *testing.T) {
	ctx := t.Context()
	directory := filepath.Join(t.TempDir(), "private")
	source := api.WorkDispatchSource{NodeID: "local", Backend: "codex", BindingID: "native-thread", OperationID: "native-user-turn", Kind: "native_activation"}
	var enrolled, creates, prompts, resolves, cancels, steers, downloads atomic.Int32
	var corruptArtifact atomic.Bool
	var missingGrant atomic.Bool
	var instance atomic.Value
	instance.Store("instance")
	artifactBytes := []byte("bounded native artifact")
	var secret string
	approval := testApproval()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("request did not use independent application scope")
		}
		p := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		switch {
		case p == "/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer(instance.Load().(string)), Capabilities: workerRequired})
		case p == "/application/connection":
			writeFixture(w, wire.ApplicationConnection{ApplicationId: "worker-app", ConnectionId: "worker-connection", PrincipalId: "owner", ExpiresAt: time.Now().Add(time.Hour)})
		case p == "/application/workers" && r.Method == "POST":
			creates.Add(1)
			var req wire.CreateWorkerRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Cwd != "/remote/linux/workspace" {
				t.Error("lost target-owned workspace")
			}
			writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "committed", SessionId: pointer("worker")})
		case p == "/application/workers":
			if missingGrant.Load() {
				writeFixture(w, []wire.ApplicationWorker{})
				return
			}
			writeFixture(w, []wire.ApplicationWorker{{SessionId: "worker", ApplicationId: "worker-app", ConnectionId: "worker-connection", PrincipalId: "owner"}})
		case p == "/application/sessions/worker/resources/file/content":
			downloads.Add(1)
			sha := digest(artifactBytes)
			if corruptArtifact.Load() {
				sha = "invalid"
			}
			writeFixture(w, wire.ApplicationResourceContent{Data: base64.StdEncoding.EncodeToString(artifactBytes), Resource: wire.ApplicationResource{Id: "file", SessionId: "worker", Name: "artifact.txt", MediaType: "text/plain", Size: len(artifactBytes), Sha256: sha}})
		case p == "/sessions/worker/steer":
			steers.Add(1)
			drop(w)
		case strings.HasPrefix(p, "/application/operations/work-send-") || strings.HasPrefix(p, "/application/operations/cancel-") || strings.HasPrefix(p, "/application/operations/approval-"):
			op := strings.TrimPrefix(p, "/application/operations/")
			writeFixture(w, wire.ApplicationOperation{OperationId: op, Outcome: "committed", Result: &wire.CommandResult{OperationId: op, Outcome: "committed"}})
		case p == "/sessions/worker/prompt":
			prompts.Add(1)
			drop(w)
		case strings.HasPrefix(p, "/application/operations/work-prompt-"):
			op := strings.TrimPrefix(p, "/application/operations/")
			writeFixture(w, wire.ApplicationOperation{OperationId: op, Outcome: "committed", Result: &wire.CommandResult{OperationId: op, Outcome: "committed"}})
		case p == "/sessions/worker/state":
			writeFixture(w, wire.SessionState{SessionId: "worker", Run: wire.RunState{Active: pointer(true), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("t")}, Approval: wire.ApprovalState{Active: approval}})
		case strings.HasSuffix(p, "/resolve"):
			resolves.Add(1)
			var req wire.ResolveApprovalRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Target != *approval.Target || value(req.OptionId) != "native-allow" {
				t.Error("approval target changed")
			}
			drop(w)
		case p == "/sessions/worker/cancel":
			cancels.Add(1)
			var req wire.CancelRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Target != *approval.Target {
				t.Error("cancel target changed")
			}
			drop(w)
		case strings.Contains(p, "/events"):
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			t.Errorf("unexpected endpoint: %s", p)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	endpoint := func(context.Context) (WorkerEndpoint, error) {
		return WorkerEndpoint{Capabilities: workerRequired, Origin: server.URL, StoreID: "store", InstanceID: instance.Load().(string), PrincipalID: "owner", Execution: api.WorkExecutionSettings{Model: "fixture"}, ModelConfigured: true, ModelAuth: "reported_ready", Enroll: func(_ context.Context, op, token string) (wire.ApplicationConnection, error) {
			raw, err := privateRead(workerSecretPath(filepath.Join(directory, "worker-application.json")), 65536)
			if err != nil || !strings.Contains(string(raw), token) {
				t.Error("enrollment dispatched before durable credential")
			}
			secret = token
			enrolled.Add(1)
			return wire.ApplicationConnection{ApplicationId: "worker-app", ConnectionId: "worker-connection", PrincipalId: "owner", ExpiresAt: time.Now().Add(time.Hour)}, nil
		}}, nil
	}
	target := api.WorkTarget{NodeID: "remote", Backend: "caelis", Role: api.RoleWorker}
	newClient := func() *WorkerClient {
		return NewWorker(WorkerOptions{Directory: directory, Target: target, Endpoint: endpoint, Source: fixtureWorkSource{source}})
	}
	worker := newClient()
	s := worker.engine
	if err := s.connectWorker(ctx); err != nil {
		t.Fatal(err)
	}
	s.connected = true
	if s.state.Session.SessionId != "" || len(s.state.Views) != 0 {
		t.Fatal("invented resident Session")
	}
	in := api.WorkStart{TaskStart: api.TaskStart{RequestID: "request", Title: "test", Prompt: "test", Target: &target}, ID: "task", Workspace: "/remote/linux/workspace", Source: source, RequestDigest: digest([]byte("fixture-start"))}
	forged := in
	forged.Source.OperationID = "forged"
	if _, err := worker.StartWork(ctx, forged); err == nil {
		t.Fatal("unattested source accepted")
	}
	task, err := worker.StartWork(ctx, in)
	if err == nil || task.Outcome != "unknown" || creates.Load() != 1 || prompts.Load() != 1 {
		t.Fatal("lost response not preserved", task, err)
	}
	if err = s.recoverOperations(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.StartWork(ctx, in); err != nil {
		t.Fatal(err)
	}
	if prompts.Load() != 1 || creates.Load() != 1 {
		t.Fatal("uncertain native work redispatched")
	}
	if err = s.refreshWorkers(ctx, s.client); err != nil {
		t.Fatal(err)
	}
	snap := worker.Snapshot()
	if len(snap.Approvals) != 1 {
		t.Fatal("worker approval missing", snap)
	}
	approvals := worker.WorkApprovals()
	if len(approvals) != 1 || approvals[0].Target != target || approvals[0].TaskID != "task" {
		t.Fatal("task approval ownership lost")
	}
	decision := api.Decision{ID: snap.Approvals[0].ID, Choice: "native-allow"}
	wrong := approvals[0]
	wrong.TaskID = "other"
	if err = worker.DecideWork(ctx, wrong, decision); err == nil || resolves.Load() != 0 {
		t.Fatal("approval crossed task ownership")
	}
	if err = worker.DecideWork(ctx, approvals[0], decision); err == nil {
		t.Fatal("lost approval response accepted")
	}
	if err = s.recoverOperations(ctx); err != nil {
		t.Fatal(err)
	}
	if err = worker.DecideWork(ctx, approvals[0], decision); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.StopWork(ctx, "task"); err == nil {
		t.Fatal("lost cancel response accepted")
	}
	if err = s.recoverOperations(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.StopWork(ctx, "task"); err != nil {
		t.Fatal(err)
	}
	currentSource := source
	currentSource.OperationID = "next-native-turn"
	s.workerSource = func(context.Context) (api.WorkDispatchSource, error) { return currentSource, nil }
	if _, err = worker.StartWork(ctx, in); err != nil {
		t.Fatal("recorded start lost original provenance", err)
	}
	fresh := in
	fresh.ID = "new-task"
	if _, err = worker.StartWork(ctx, fresh); err == nil {
		t.Fatal("fresh mutation reused old source")
	}
	message := api.TaskMessage{ID: "task", RequestID: "continue", Prompt: "continue", Source: currentSource, RequestDigest: digest([]byte("continuation-digest"))}
	if _, err = worker.SendWork(ctx, message); err == nil {
		t.Fatal("lost steer response accepted")
	}
	currentSource.OperationID = "later-native-turn"
	if err = s.recoverOperations(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.SendWork(ctx, message); err != nil || steers.Load() != 1 {
		t.Fatal("recorded continuation replay lost source or redispatched", err)
	}
	changed := message
	changed.Prompt = "changed"
	if _, err = worker.SendWork(ctx, changed); err == nil {
		t.Fatal("recorded continuation changed intent")
	}
	artifactID := "resource:worker:file"
	s.mu.Lock()
	s.state.Views["worker"].Items = append(s.state.Views["worker"].Items, api.Item{Artifacts: []api.Artifact{{ID: artifactID}}})
	s.mu.Unlock()
	artifacts := worker.WorkArtifacts()
	if len(artifacts) != 1 || artifacts[0].TaskID != "task" || artifacts[0].Target != target || artifacts[0].Artifact.ID != artifactID {
		t.Fatal("canonical artifact ownership lost")
	}
	if _, err = worker.ReadWorkArtifact(ctx, "other", artifactID); err == nil || downloads.Load() != 0 {
		t.Fatal("artifact crossed task ownership")
	}
	artifact, err := worker.ReadWorkArtifact(ctx, "task", artifactID)
	if err != nil || string(artifact.Bytes) != string(artifactBytes) || artifact.SHA256 != digest(artifactBytes) {
		t.Fatal("artifact integrity metadata", err)
	}
	corruptArtifact.Store(true)
	if _, err = worker.ReadWorkArtifact(ctx, "task", artifactID); err == nil {
		t.Fatal("artifact checksum mismatch admitted")
	}

	if resolves.Load() != 1 || cancels.Load() != 1 {
		t.Fatal("native operations missing")
	}
	worker.Close(ctx)
	restored := newClient()
	if err = restored.engine.connectWorker(ctx); err != nil {
		t.Fatal(err)
	}
	defer restored.Close(ctx)
	if enrolled.Load() != 1 || restored.engine.state.Workers["task"].Binding.SessionId != "worker" {
		t.Fatal("reconnect replaced scoped/native binding")
	}
	missingGrant.Store(true)
	ungranted := newClient()
	if err = ungranted.engine.connectWorker(ctx); err == nil || enrolled.Load() != 1 {
		t.Fatal("reconnect replaced original native grant")
	}
	missingGrant.Store(false)
	wrongTarget := NewWorker(WorkerOptions{Directory: directory, Target: api.WorkTarget{NodeID: "another", Backend: "caelis", Role: api.RoleWorker}, Endpoint: endpoint, Source: fixtureWorkSource{source}})
	if err = wrongTarget.engine.connectWorker(ctx); err == nil || enrolled.Load() != 1 {
		t.Fatal("private Worker connection adopted another target")
	}
	instance.Store("restarted-instance")
	restarted := newClient()
	if err = restarted.engine.connectWorker(ctx); err != nil || enrolled.Load() != 1 || restarted.engine.state.InstanceID != "restarted-instance" {
		t.Fatal("same Store restart rejected or changed enrollment", err)
	}
	defer restarted.Close(ctx)
	changedHost := NewWorker(WorkerOptions{Directory: directory, Target: target, Endpoint: func(ctx context.Context) (WorkerEndpoint, error) {
		ep, err := endpoint(ctx)
		ep.InstanceID = "replacement"
		return ep, err
	}})
	if err = changedHost.engine.connectWorker(ctx); err == nil || enrolled.Load() != 1 {
		t.Fatal("reconnect trusted replaced Host instance")
	}
	info, err := os.Stat(workerSecretPath(s.path))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential not private")
	}
}
func TestResidentWorkSourceExportsOnlyAttestedMetadata(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) {})
	s.state.PrincipalID = "owner"
	if _, err := s.WorkDispatchSource(t.Context()); !errors.Is(err, api.ErrWorkSourceInactive) {
		t.Fatal("connected missing callback not classified as idle", err)
	}
	call := wire.ApplicationCall{ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", SessionId: "main", Source: wire.ApplicationSource{Kind: "user", OperationId: "user-op"}}
	source, err := s.WorkDispatchSource(context.WithValue(t.Context(), invocationKey{}, call))
	if err != nil || source.BindingID != "main" || source.OperationID != "user-op" {
		t.Fatal(source, err)
	}
	call.Source.Kind = "application_summary"
	if _, err = s.WorkDispatchSource(context.WithValue(t.Context(), invocationKey{}, call)); err == nil || errors.Is(err, api.ErrWorkSourceInactive) {
		t.Fatal("summary promoted to idle authority", err)
	}
}

func TestWorkerUnknownModelPermitsScopedSetupWithoutInventingAuthentication(t *testing.T) {
	target := api.WorkTarget{NodeID: "remote", Backend: "caelis", Role: api.RoleWorker}
	source := api.WorkDispatchSource{NodeID: "local", Backend: "codex", BindingID: "native-thread", OperationID: "native-user-turn", Kind: "native_activation"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/control/v1/initialize" {
			t.Error("setup unexpectedly dispatched work")
			w.WriteHeader(404)
			return
		}
		writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: workerRequired})
	}))
	defer server.Close()
	for _, auth := range []string{"unknown", "reported_missing"} {
		t.Run(auth, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "private")
			var enrolled atomic.Int32
			worker := NewWorker(WorkerOptions{Directory: directory, Target: target, Source: fixtureWorkSource{source}, Endpoint: func(context.Context) (WorkerEndpoint, error) {
				return WorkerEndpoint{Capabilities: workerRequired, Origin: server.URL, StoreID: "store", InstanceID: "instance", PrincipalID: "owner", ModelConfigured: true, ModelAuth: auth, Execution: api.WorkExecutionSettings{Model: "configured"}, Enroll: func(context.Context, string, string) (wire.ApplicationConnection, error) {
					enrolled.Add(1)
					return wire.ApplicationConnection{ApplicationId: "app", ConnectionId: "connection", PrincipalId: "owner", ExpiresAt: time.Now().Add(time.Hour)}, nil
				}}, nil
			}})
			if err := worker.Connect(t.Context()); err != nil || enrolled.Load() != 1 {
				t.Fatal("model metadata blocked native setup", err)
			}
			defer worker.Close(t.Context())
			configured, reported := worker.ModelReadiness()
			if !configured || reported != auth {
				t.Fatal("metadata promoted to verified login")
			}
			err := worker.WorkAdmission(t.Context())
			if auth == "unknown" && err != nil {
				t.Fatal("unknown metadata blocked explicit native authorization", err)
			}
			if auth == "reported_missing" && err == nil {
				t.Fatal("known missing authentication hidden")
			}
		})
	}
}

func TestWorkerDetachDuringEnrollmentDoesNotStartObservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: workerRequired})
	}))
	defer server.Close()
	entered, released := make(chan struct{}), make(chan struct{})
	worker := NewWorker(WorkerOptions{Directory: filepath.Join(t.TempDir(), "private"), Target: api.WorkTarget{NodeID: "remote", Backend: "caelis", Role: api.RoleWorker}, Endpoint: func(context.Context) (WorkerEndpoint, error) {
		return WorkerEndpoint{Capabilities: workerRequired, Origin: server.URL, StoreID: "store", InstanceID: "instance", PrincipalID: "owner", Enroll: func(context.Context, string, string) (wire.ApplicationConnection, error) {
			close(entered)
			<-released
			return wire.ApplicationConnection{ApplicationId: "app", ConnectionId: "connection", PrincipalId: "owner", ExpiresAt: time.Now().Add(time.Hour)}, nil
		}}, nil
	}})
	connected, detached := make(chan error, 1), make(chan error, 1)
	go func() { connected <- worker.Connect(ctx) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("enrollment not reached")
	}
	go func() { detached <- worker.Close(ctx) }()
	waitAcceptance(t, ctx, func() bool { worker.engine.mu.Lock(); defer worker.engine.mu.Unlock(); return worker.engine.closed })
	close(released)
	select {
	case err := <-connected:
		if err == nil {
			t.Fatal("detached connection became ready")
		}
	case <-ctx.Done():
		t.Fatal("connect did not finish")
	}
	select {
	case err := <-detached:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("detach did not finish")
	}
	worker.engine.mu.Lock()
	defer worker.engine.mu.Unlock()
	if worker.engine.cancel != nil || worker.engine.connected {
		t.Fatal("observation started after detach")
	}
}
