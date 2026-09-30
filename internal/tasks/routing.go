package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func localTarget(provider string) api.WorkTarget {
	return api.WorkTarget{NodeID: api.LocalNodeID, Backend: provider, Role: api.RoleWorker}
}
func targetPointer(target api.WorkTarget) *api.WorkTarget { return &target }
func copyTask(task api.Task) api.Task {
	if task.Target != nil {
		task.Target = targetPointer(*task.Target)
	}
	return task
}
func validateWorkerTarget(target api.WorkTarget) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if target.Role != api.RoleWorker {
		return errors.New("task target must have the worker role")
	}
	return nil
}

func requestDigest(id string, target api.WorkTarget, workspace, fingerprint string, source api.WorkDispatchSource) string {
	b, _ := json.Marshal(struct {
		ID                     string
		Target                 api.WorkTarget
		Workspace, Fingerprint string
		Source                 api.WorkDispatchSource
	}{id, target, workspace, fingerprint, source})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (m *Manager) runtimeFor(target api.WorkTarget) (api.WorkRuntime, error) {
	if err := validateWorkerTarget(target); err != nil {
		return nil, err
	}
	if target == localTarget(m.provider) {
		return m.work, nil
	}
	if m.router == nil {
		return nil, errors.New("selected worker target is unavailable")
	}
	return m.router.WorkRuntimeFor(target)
}

func (m *Manager) recordRuntime(id string) (api.WorkRuntime, error) {
	m.mu.Lock()
	r := m.state.Records[id]
	if r == nil || r.Provider != m.provider {
		m.mu.Unlock()
		return nil, errors.New(m.text("host.onlyOperateBotCreatedTasks"))
	}
	target := r.Target
	m.mu.Unlock()
	return m.runtimeFor(target)
}

// WorkRoutes exposes host ports for composition (approvals/resources), never
// native credentials or an Engine whose resident lifecycle could be confused
// with a Worker. The local route remains direct even without a registry.
func (m *Manager) WorkRoutes() []api.WorkRoute {
	out := []api.WorkRoute{{Target: localTarget(m.provider), Runtime: m.work}}
	if m.router != nil {
		for _, route := range m.router.WorkRoutes() {
			if route.Target != localTarget(m.provider) {
				out = append(out, route)
			}
		}
	}
	return out
}

// OwnsWorkTarget is a read-only ledger check for host approval/resource routing.
// It neither adopts native work nor polls a Worker or reads its transcript.
func (m *Manager) OwnsWorkTarget(id string, target api.WorkTarget) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.state.Records[id]
	return r != nil && r.Provider == m.provider && r.Target == target
}

func (m *Manager) WorkTargets() []api.WorkTargetInfo {
	if catalog, ok := m.router.(api.WorkTargetCatalog); ok {
		return catalog.WorkTargets()
	}
	return []api.WorkTargetInfo{{Target: localTarget(m.provider), Label: "This machine", State: "ready"}}
}

// bindMessage commits remote continuation intent before any native call. A
// request ID cannot move between tasks, change prompt or acquire a new source
// on retry; the native adapter reconciles the original receipt.
func (m *Manager) bindMessage(in api.TaskMessage, target api.WorkTarget) (api.TaskMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if previous, ok := m.state.Messages[in.RequestID]; ok {
		if previous.TaskID != in.ID || previous.Fingerprint != hash(in.Prompt) {
			return in, errors.New("same request ID names different worker continuation")
		}
		in.Source, in.RequestDigest = previous.Source, previous.RequestDigest
		return in, nil
	}
	if m.state.Messages == nil {
		m.state.Messages = map[string]messageIntent{}
	}
	r := m.state.Records[in.ID]
	in.RequestDigest = requestDigest(in.RequestID, target, r.View.Workspace, hash(in.ID, hash(in.Prompt)), in.Source)
	m.state.Messages[in.RequestID] = messageIntent{TaskID: in.ID, Fingerprint: hash(in.Prompt), Source: in.Source, RequestDigest: in.RequestDigest}
	if err := m.write(); err != nil {
		delete(m.state.Messages, in.RequestID)
		return in, err
	}
	return in, nil
}

func (m *Manager) workStates() ([]api.WorkState, error) {
	var out []api.WorkState
	for _, route := range m.WorkRoutes() {
		if err := validateWorkerTarget(route.Target); err != nil {
			return nil, err
		}
		if route.Runtime == nil {
			return nil, errors.New("worker route has no native port")
		}
		for _, state := range route.Runtime.WorkStates() {
			if state.Target != (api.WorkTarget{}) && state.Target != route.Target || state.Task.Target != nil && *state.Task.Target != route.Target {
				return nil, errors.New("native worker target binding changed")
			}
			state.Target = route.Target
			state.Task.Target = targetPointer(route.Target)
			out = append(out, state)
		}
	}
	return out, nil
}

func (m *Manager) authorizeWork(ctx context.Context, target api.WorkTarget) (api.WorkDispatchSource, error) {
	// The default local path retains its existing native admission semantics.
	if target == localTarget(m.provider) {
		return api.WorkDispatchSource{}, m.work.WorkAdmission(ctx)
	}
	authorizer := m.authorizer
	if authorizer == nil {
		authorizer, _ = m.work.(api.WorkSourceProvider)
	}
	if authorizer == nil {
		return api.WorkDispatchSource{}, errors.New("resident driver cannot attest worker dispatch source")
	}
	source, err := authorizer.WorkDispatchSource(ctx)
	if err != nil {
		return api.WorkDispatchSource{}, err
	}
	if err = source.Validate(); err != nil {
		return api.WorkDispatchSource{}, err
	}
	if source.NodeID != api.LocalNodeID || source.Backend != m.provider {
		return api.WorkDispatchSource{}, errors.New("worker source does not match resident driver")
	}
	return source, nil
}
