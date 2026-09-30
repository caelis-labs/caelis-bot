package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type workerRouteFixture struct {
	routes []api.WorkRoute
	owned  map[string]api.WorkTarget
}

func (o *workerRouteFixture) WorkRoutes() []api.WorkRoute { return o.routes }
func (o *workerRouteFixture) OwnsWorkTarget(id string, target api.WorkTarget) bool {
	owned, exists := o.owned[id]
	return exists && target == owned
}
func (o *workerRouteFixture) WorkTargets() []api.WorkTargetInfo {
	var out []api.WorkTargetInfo
	for _, route := range o.routes {
		out = append(out, api.WorkTargetInfo{Target: route.Target, Label: "Fixture Worker", State: "ready"})
	}
	return out
}

type interactionWorker struct {
	api.WorkRuntime
	approvals              []api.WorkApproval
	artifacts              []api.WorkArtifactRef
	data                   api.WorkArtifact
	decisions              int
	decision               api.Decision
	taskRead, artifactRead string
	afterRead              func()
}

func (w *interactionWorker) WorkApprovals() []api.WorkApproval { return w.approvals }
func (w *interactionWorker) DecideWork(_ context.Context, work api.WorkApproval, decision api.Decision) error {
	if len(w.approvals) == 0 || !reflect.DeepEqual(w.approvals[0], work) {
		return errors.New("native approval changed")
	}
	w.decisions++
	w.decision = decision
	return nil
}
func (w *interactionWorker) WorkArtifacts() []api.WorkArtifactRef { return w.artifacts }
func (w *interactionWorker) ReadWorkArtifact(_ context.Context, taskID, artifactID string) (api.WorkArtifact, error) {
	w.taskRead, w.artifactRead = taskID, artifactID
	if w.afterRead != nil {
		w.afterRead()
	}
	return w.data, nil
}

func interactionFixture(t *testing.T) (*Service, *workerRouteFixture, *interactionWorker, api.WorkTarget) {
	t.Helper()
	target := api.WorkTarget{NodeID: "rocky", Backend: "caelis", Role: api.RoleWorker}
	w := &interactionWorker{approvals: []api.WorkApproval{{TaskID: "owned-task", Target: target, Approval: api.Approval{ID: "native-request", TaskTitle: "Compile", Title: "Approve", Status: "pending", Target: "exact-command", Choices: []api.Choice{{ID: "once", Label: "Allow once"}}}}}}
	owner := &workerRouteFixture{routes: []api.WorkRoute{{Target: target, Runtime: w}}, owned: map[string]api.WorkTarget{"owned-task": target}}
	s := NewService(snapshotEngine{}, nil, nil, nil, nil)
	s.ConfigureWorkRoutes(owner, filepath.Join(t.TempDir(), "Artifacts"))
	return s, owner, w, target
}

func TestWorkerApprovalProjectionRoutesExactNativeChoiceAndRejectsStaleTargets(t *testing.T) {
	s, owner, w, target := interactionFixture(t)
	snapshot := s.Snapshot()
	if len(snapshot.Approvals) != 1 || !isWorkerApproval(snapshot.Approvals[0].ID) || snapshot.Approvals[0].ID == "native-request" || snapshot.Approvals[0].TaskTitle != "Compile · Fixture Worker" {
		t.Fatal("worker approval not projected into product surface", snapshot)
	}
	id := snapshot.Approvals[0].ID
	if err := s.Decide(t.Context(), api.Decision{ID: id, Choice: "all"}); err == nil || w.decisions != 0 {
		t.Fatal("unoffered choice reached native Worker")
	}
	if err := s.Decide(t.Context(), api.Decision{ID: id, Choice: "once"}); err != nil || w.decisions != 1 || w.decision.ID != "native-request" {
		t.Fatal("choice lost exact native target", err, w.decision)
	}
	w.approvals[0].Approval.Choices = []api.Choice{{ID: "deny"}}
	if err := s.Decide(t.Context(), api.Decision{ID: id, Choice: "once"}); err == nil || w.decisions != 1 {
		t.Fatal("stale choice was routed")
	}
	current := s.Snapshot().Approvals[0].ID
	replacement := &interactionWorker{approvals: w.approvals}
	owner.routes = []api.WorkRoute{{Target: target, Runtime: replacement}}
	if err := s.Decide(t.Context(), api.Decision{ID: current, Choice: "deny"}); err == nil || replacement.decisions != 0 {
		t.Fatal("old connection handle revived on new native owner")
	}
	delete(owner.owned, "owned-task")
	if len(s.Snapshot().Approvals) != 0 {
		t.Fatal("foreign native work adopted as product task")
	}
}

func TestWorkerApprovalCannotCrossConfiguredMachineOrDetachedRoute(t *testing.T) {
	s, owner, w, _ := interactionFixture(t)
	id := s.Snapshot().Approvals[0].ID
	w.approvals[0].Target.NodeID = "different"
	if len(s.PetSnapshot().Approvals) != 0 || s.Decide(t.Context(), api.Decision{ID: id, Choice: "once"}) == nil {
		t.Fatal("approval adopted through wrong route")
	}
	owner.routes = nil
	if s.Decide(t.Context(), api.Decision{ID: id, Choice: "once"}) == nil || w.decisions != 0 {
		t.Fatal("detached route retained decision authority")
	}
}

func TestOwnedWorkerArtifactDownloadVerifiesBytesAndUsesPrivateDestination(t *testing.T) {
	s, owner, w, target := interactionFixture(t)
	data := []byte("owned output\n")
	sum := sha256.Sum256(data)
	w.data = api.WorkArtifact{ID: "native-artifact", Name: "output.txt", Size: int64(len(data)), Bytes: data, SHA256: hex.EncodeToString(sum[:])}
	w.artifacts = []api.WorkArtifactRef{{TaskID: "owned-task", Target: target, Artifact: api.Artifact{ID: "native-artifact", Name: "output.txt"}}}
	var revealed string
	s.reveal = func(value string) error { revealed = value; return nil }
	snapshot := s.Snapshot()
	if len(snapshot.Items) != 1 || len(snapshot.Items[0].Artifacts) != 1 {
		t.Fatal("canonical artifact not projected")
	}
	id := snapshot.Items[0].Artifacts[0].ID
	if err := s.RevealArtifact(id); err != nil || w.taskRead != "owned-task" || w.artifactRead != "native-artifact" {
		t.Fatal("download lost task-owned resource identity", err)
	}
	got, err := os.ReadFile(revealed)
	info, statErr := os.Stat(revealed)
	if err != nil || string(got) != string(data) || statErr != nil || info.Mode().Perm() != 0600 || !filepath.IsAbs(revealed) {
		t.Fatal("private artifact bytes not verified/saved", err, statErr)
	}
	for _, corrupt := range []api.WorkArtifact{
		{ID: "native-artifact", Name: "../escape", Bytes: data, Size: int64(len(data)), SHA256: w.data.SHA256},
		{ID: "native-artifact", Name: "..", Bytes: data, Size: int64(len(data)), SHA256: w.data.SHA256},
		{ID: "native-artifact", Name: "output.txt", Bytes: data, Size: int64(len(data)), SHA256: "wrong"},
		{ID: "another", Name: "output.txt", Bytes: data, Size: int64(len(data)), SHA256: w.data.SHA256},
		{ID: "native-artifact", Name: "output.txt", Bytes: data, Size: api.MaxWorkArtifactBytes + 1, SHA256: w.data.SHA256},
	} {
		w.data = corrupt
		revealed = ""
		if s.RevealArtifact(id) == nil || revealed != "" {
			t.Fatal("invalid artifact data escaped resource boundary", corrupt.Name)
		}
	}
	delete(owner.owned, "owned-task")
	if s.RevealArtifact(id) == nil {
		t.Fatal("unowned artifact remained downloadable")
	}
}

func TestProductArtifactUsesCurrentOpaqueOwnedResourceAndRejectsDetachDuringRead(t *testing.T) {
	s, owner, w, target := interactionFixture(t)
	bytes := []byte("bounded worker bytes")
	sum := sha256.Sum256(bytes)
	w.data = api.WorkArtifact{ID: "artifact", Name: "result.txt", Size: int64(len(bytes)), Bytes: bytes, SHA256: hex.EncodeToString(sum[:])}
	w.artifacts = []api.WorkArtifactRef{{TaskID: "owned-task", Target: target, Artifact: api.Artifact{ID: "artifact", Name: "result.txt"}}}
	handle := s.Snapshot().Items[0].Artifacts[0].ID
	if _, err := s.ReadProductArtifact(t.Context(), "artifact"); err == nil {
		t.Fatal("native identifier accepted as product resource")
	}
	if got, err := s.ReadProductArtifact(t.Context(), handle); err != nil || string(got.Bytes) != string(bytes) {
		t.Fatal("current exact resource unavailable", err)
	}
	w.data.Name = "different.txt"
	if _, err := s.ReadProductArtifact(t.Context(), handle); err == nil {
		t.Fatal("canonical filename changed during read")
	}
	w.data.Name = "result.txt"
	w.afterRead = func() { owner.routes = nil }
	if _, err := s.ReadProductArtifact(t.Context(), handle); err == nil {
		t.Fatal("detached route retained resource authority during read")
	}
}
