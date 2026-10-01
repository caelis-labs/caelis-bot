package backend

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type WorkRouteOwner interface {
	DefaultWorkerTarget() api.WorkTarget
	WorkRoutes() []api.WorkRoute
	OwnsWorkTarget(string, api.WorkTarget) bool
	WorkTargets() []api.WorkTargetInfo
}

type workerApprovalBinding struct {
	work     api.WorkApproval
	provider api.WorkApprovalProvider
}

type workerArtifactBinding struct {
	ref      api.WorkArtifactRef
	provider api.WorkArtifactProvider
}

type workerGeneration struct {
	identity uintptr
	epoch    uint64
	runtime  api.WorkRuntime // retain ownership so pointer reuse cannot revive a handle
}

type workerInteractions struct {
	mu          sync.Mutex
	owner       WorkRouteOwner
	directory   string
	namespace   string
	epoch       uint64
	generations map[api.WorkTarget]workerGeneration
	approvals   map[string]workerApprovalBinding
	artifacts   map[string]workerArtifactBinding
}

func (s *Service) ConfigureWorkRoutes(owner WorkRouteOwner, artifactDirectory string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workInteractions = &workerInteractions{owner: owner, directory: artifactDirectory, namespace: rand.Text(), generations: map[api.WorkTarget]workerGeneration{}, approvals: map[string]workerApprovalBinding{}, artifacts: map[string]workerArtifactBinding{}}
}

func (s *Service) workerInteractions() *workerInteractions {
	if s.blockLocalConfiguration() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workInteractions
}

func (s *Service) hasWorkerInteractions() bool {
	workers := s.workerInteractions()
	if workers == nil || workers.owner == nil {
		return false
	}
	return slices.ContainsFunc(workers.owner.WorkTargets(), func(info api.WorkTargetInfo) bool { return info.Target != workers.owner.DefaultWorkerTarget() })
}

func isWorkerApproval(id string) bool { return strings.HasPrefix(id, "work-approval:") }
func isWorkerArtifact(id string) bool { return strings.HasPrefix(id, "work-artifact:") }

func (w *workerInteractions) handle(kind string, epoch uint64, value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("%s/%d/", w.namespace, epoch)), b...))
	return "work-" + kind + ":" + hex.EncodeToString(sum[:])
}

func cloneWorkerApproval(approval api.Approval) api.Approval {
	b, _ := json.Marshal(approval)
	var copy api.Approval
	_ = json.Unmarshal(b, &copy)
	return copy
}

// Projection can only expose an interaction already owned by the product
// ledger on this exact node/backend/role. It never adopts an unrelated Worker.
func (w *workerInteractions) project(snapshot api.Snapshot) api.Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.owner == nil {
		return snapshot
	}
	approvals := map[string]workerApprovalBinding{}
	artifacts := map[string]workerArtifactBinding{}
	active := map[api.WorkTarget]bool{}
	labels := map[api.WorkTarget]string{}
	for _, info := range w.owner.WorkTargets() {
		labels[info.Target] = info.Label
	}
	for _, route := range w.owner.WorkRoutes() {
		if route.Target == w.owner.DefaultWorkerTarget() || route.Runtime == nil {
			continue
		}
		value := reflect.ValueOf(route.Runtime)
		if value.Kind() != reflect.Pointer || value.IsNil() {
			continue // Native connection identity must have a stable owner.
		}
		identity := value.Pointer()
		generation, exists := w.generations[route.Target]
		if !exists || generation.identity != identity {
			w.epoch++
			generation = workerGeneration{identity: identity, epoch: w.epoch, runtime: route.Runtime}
			w.generations[route.Target] = generation
		}
		active[route.Target] = true
		if provider, ok := route.Runtime.(api.WorkApprovalProvider); ok {
			for _, work := range provider.WorkApprovals() {
				if work.Target != route.Target || work.TaskID == "" || work.Approval.ID == "" || work.Approval.Status == "resolved" || !w.owner.OwnsWorkTarget(work.TaskID, route.Target) {
					continue
				}
				work.Approval = cloneWorkerApproval(work.Approval)
				handle := w.handle("approval", generation.epoch, work)
				approvals[handle] = workerApprovalBinding{work: work, provider: provider}
				presentation := cloneWorkerApproval(work.Approval)
				presentation.ID = handle
				if label := labels[route.Target]; label != "" {
					if presentation.TaskTitle == "" {
						presentation.TaskTitle = label
					} else {
						presentation.TaskTitle += " · " + label
					}
				}
				snapshot.Approvals = append(slices.Clone(snapshot.Approvals), presentation)
			}
		}
		catalog, hasCatalog := route.Runtime.(api.WorkArtifactCatalog)
		provider, hasReader := route.Runtime.(api.WorkArtifactProvider)
		if hasCatalog && hasReader {
			for _, ref := range catalog.WorkArtifacts() {
				if ref.Target != route.Target || ref.TaskID == "" || ref.Artifact.ID == "" || !w.owner.OwnsWorkTarget(ref.TaskID, route.Target) {
					continue
				}
				handle := w.handle("artifact", generation.epoch, ref)
				artifacts[handle] = workerArtifactBinding{ref: ref, provider: provider}
				snapshot.Items = append(slices.Clone(snapshot.Items), api.Item{ID: handle, Kind: "assistant", Text: labels[route.Target], Status: "completed", Artifacts: []api.Artifact{{ID: handle, Name: ref.Artifact.Name}}})
			}
		}
	}
	for target := range w.generations {
		if !active[target] {
			delete(w.generations, target)
		}
	}
	w.approvals, w.artifacts = approvals, artifacts
	return snapshot
}

func (w *workerInteractions) approval(id string) (workerApprovalBinding, error) {
	w.project(api.Snapshot{})
	w.mu.Lock()
	defer w.mu.Unlock()
	binding, ok := w.approvals[id]
	if !ok || !w.owner.OwnsWorkTarget(binding.work.TaskID, binding.work.Target) {
		return workerApprovalBinding{}, errors.New("worker approval changed or is no longer owned")
	}
	return binding, nil
}

func (w *workerInteractions) decide(ctx context.Context, decision api.Decision) error {
	binding, err := w.approval(decision.ID)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(binding.work.Approval.Choices, func(choice api.Choice) bool { return choice.ID == decision.Choice }) {
		return errors.New("worker approval choice is no longer available")
	}
	decision.ID = binding.work.Approval.ID
	return binding.provider.DecideWork(ctx, binding.work, decision)
}

func (w *workerInteractions) approvalURL(id string) (string, error) {
	binding, err := w.approval(id)
	if err != nil || binding.work.Approval.URL == "" {
		return "", errors.New("worker approval link is no longer available")
	}
	return binding.work.Approval.URL, nil
}

func (s *Service) revealWorkerArtifact(id string) error {
	w := s.workerInteractions()
	if w == nil || s.reveal == nil {
		return errors.New("worker artifact is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	artifact, err := s.ReadProductArtifact(ctx, id)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(w.directory) {
		return errors.New("private worker artifact destination is unavailable")
	}
	if err := os.MkdirAll(w.directory, 0700); err != nil {
		return errors.New("worker artifact destination could not be prepared")
	}
	info, err := os.Lstat(w.directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("worker artifact destination must be private")
	}
	directory, err := os.MkdirTemp(w.directory, "artifact-*")
	if err != nil {
		return errors.New("worker artifact download could not be saved")
	}
	destination := filepath.Join(directory, artifact.Name)
	if err := os.WriteFile(destination, artifact.Bytes, 0600); err != nil {
		_ = os.RemoveAll(directory)
		return errors.New("worker artifact download could not be saved")
	}
	return s.reveal(destination)
}
