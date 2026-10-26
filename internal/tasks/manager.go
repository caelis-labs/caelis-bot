// Package tasks owns Bot workspaces, task admission and finite completion
// reports. Runtime adapters retain native execution facts and approval policy.
package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

type record struct {
	FinishedExecution   string                             `json:"finishedExecution,omitempty"`
	FileStorageVersion  int                                `json:"fileStorageVersion,omitempty"`
	InputReferences     map[string][]api.TaskFileReference `json:"inputReferences,omitempty"`
	InputPaths          map[string][]string                `json:"inputPaths,omitempty"`
	Operations          map[string]api.WorkOperationQuery  `json:"operations,omitempty"`
	Requests            []string                           `json:"requests,omitempty"`
	ConfirmedRequests   map[string]bool                    `json:"confirmedRequests,omitempty"`
	RequestOutcomes     map[string]string                  `json:"requestOutcomes,omitempty"`
	RequestFingerprints map[string]string                  `json:"requestFingerprints,omitempty"`
	RetiredExecutions   []string                           `json:"retiredExecutions,omitempty"`
	PreviousReports     []reportReceipt                    `json:"previousReports,omitempty"`
	Runtime             string                             `json:"runtime,omitempty"`
	Sequence            int64                              `json:"sequence,omitempty"`
	CreatedAt           int64                              `json:"createdAt,omitempty"`
	Locked              bool                               `json:"locked,omitempty"`
	CompletedAt         int64                              `json:"completedAt,omitempty"`
	ActiveAt            int64                              `json:"activeAt,omitempty"`
	Pinned              *bool                              `json:"pinned,omitempty"`
	ExplicitPin         bool                               `json:"explicitPin,omitempty"`
	OriginalPrompt      string                             `json:"originalPrompt,omitempty"`
	View                api.Task                           `json:"view"`
	Provider            string                             `json:"provider"`
	Fingerprint         string                             `json:"fingerprint,omitempty"`
	Execution           string                             `json:"execution,omitempty"`
	ReportText          string                             `json:"reportText,omitempty"`
	ReportID            string                             `json:"reportId,omitempty"`
	ReportState         string                             `json:"reportState,omitempty"`
	StopResolution      string                             `json:"stopResolution,omitempty"`
	UnknownSince        int64                              `json:"unknownSince,omitempty"`
	UnknownNotice       string                             `json:"unknownNotice,omitempty"`
	Activity            string                             `json:"-"`
}
type reportReceipt struct {
	ID    string `json:"id"`
	State string `json:"state"`
}
type state struct {
	WatchOrder map[string][]string `json:"watchOrder,omitempty"`
	Sequence   int64               `json:"sequence,omitempty"`
	Version    int                 `json:"version"`
	Records    map[string]*record  `json:"records"`
}

type Manager struct {
	finished             func(api.WorkState) error
	operationObserver    func(api.WorkOperation) error
	environmentMu        sync.Mutex
	environmentOps       map[string]*environmentOperation // live target locks, no execution state
	inputMu              sync.Mutex
	inputActive          int
	inputCreated         map[string]os.FileInfo // guarded by inputMu; unpublished cache identities only
	fileStorageUncertain bool                   // guarded by mu; never execution state
	fileCopies           *workerfiles.Outputs
	fileCopiesOnce       sync.Once
	quarantined          map[string]*record
	inputResolver        func(context.Context, string) (api.WorkFileContent, error)
	now                  func() time.Time
	maxRunning           func() int
	watchlistChanged     func([]api.TaskPreview)
	started              func(context.Context, api.Task)
	mu                   sync.Mutex
	op                   sync.Mutex
	observe              sync.Mutex      // serialize native observation through ledger projection
	inflight             map[string]bool // protected by mu; op still serializes submissions
	paused               bool            // protected by op; updater admission fence
	path, root, provider string
	work                 api.WorkRuntime
	reports              api.ReportSubmitter
	snapshot             func() api.Snapshot
	localeMu             sync.RWMutex
	locale               func() i18n.Locale
	state                state
	persisted            string
	write                func() error
}

func (m *Manager) SetLocale(f func() i18n.Locale) {
	if m == nil {
		return
	}
	m.localeMu.Lock()
	defer m.localeMu.Unlock()
	m.locale = f
}

func (m *Manager) currentLocale() i18n.Locale {
	if m == nil {
		return i18n.DefaultLocale
	}
	m.localeMu.RLock()
	f := m.locale
	m.localeMu.RUnlock()
	// Invoke host callbacks without holding the locale lock or acquiring the task lock.
	if f != nil {
		if l := f(); l != "" {
			return l
		}
	}
	return i18n.DefaultLocale
}

func (m *Manager) text(key string, args ...map[string]any) string {
	l := m.currentLocale()
	var a map[string]any
	if len(args) > 0 {
		a = args[0]
	}
	return i18n.Text(l, key, a)
}

func Open(path, root, provider string, work api.WorkRuntime, reports api.ReportSubmitter, snapshot func() api.Snapshot) (*Manager, error) {
	if !filepath.IsAbs(path) || !filepath.IsAbs(root) || provider == "" || work == nil || reports == nil || snapshot == nil {
		return nil, errors.New(i18n.Text(i18n.DefaultLocale, "host.taskHostConfigIncomplete", nil))
	}
	m := &Manager{now: time.Now, path: path, root: root, provider: provider, work: work, reports: reports, snapshot: snapshot, state: state{Version: 1, Records: map[string]*record{}}}
	if b, e := os.ReadFile(path); e == nil {
		if json.Unmarshal(b, &m.state) != nil || m.state.Version != 1 || m.state.Records == nil {
			return nil, errors.New(m.text("host.taskLedgerUnreadable"))
		}
		m.quarantined = map[string]*record{}
		for id, r := range m.state.Records {
			if r != nil && r.View.Status == "unavailable" {
				r.View.Status = "unknown"
			}
			if r == nil || r.View.ID != id || r.Provider == "" {
				m.quarantined[id] = r
				delete(m.state.Records, id)
			}
		}
		encoded, _ := json.MarshalIndent(m.state, "", "  ")
		m.persisted = string(encoded)
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	m.write = m.save
	if router, ok := work.(api.WorkRouter); ok {
		for id, r := range m.state.Records {
			runtime := r.Runtime
			if runtime == "" && r.View.Machine == "" {
				runtime = r.Provider
			}
			var err error
			resolved, bindErr := router.BindWork(context.Background(), api.TaskStart{Machine: r.View.Machine}, id, runtime)
			err = bindErr
			if err == nil {
				r.Runtime = resolved
			}
			if err != nil {
				r.View.Status = "unknown"
			}
		}
	}
	if e := m.refresh(); e != nil && !errors.As(e, new(*observationConflict)) {
		return nil, e
	}
	return m, nil
}

func hash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}
func terminal(s string) bool {
	return s == "completed" || s == "failed" || s == "cancelled" || s == "interrupted"
}
func valid(id, text string) bool {
	return len(id) >= 8 && len(id) <= 128 && strings.TrimSpace(text) != "" && len(text) <= 24000
}
func (m *Manager) save() error {
	state := m.state
	if len(m.quarantined) > 0 {
		state.Records = make(map[string]*record, len(m.state.Records)+len(m.quarantined))
		for id, r := range m.state.Records {
			state.Records[id] = r
		}
		for id, r := range m.quarantined {
			state.Records[id] = r
		}
	}
	b, e := json.MarshalIndent(state, "", "  ")
	if e != nil {
		return e
	}
	if string(b) == m.persisted {
		return nil
	}
	if e = localstate.Write(m.path, state); e == nil {
		m.persisted = string(b)
	}
	return e
}

// Only already-owned executions can enter this ledger. The host router retains
// both providers; an unregistered provider remains inert, with no adopted IDs.
func (m *Manager) refresh() error {
	m.observe.Lock()
	defer m.observe.Unlock()
	states := m.work.WorkStates()
	for i := range states {
		if states[i].Task.Status == "unavailable" {
			states[i].Task.Status = "unknown"
			states[i].TaskEnded = false
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var conflict error
	for _, v := range states {
		if r := m.state.Records[v.Task.ID]; r != nil && (!m.owns(r) || r.View.Machine != v.Task.Machine || r.Runtime != "" && v.Runtime != "" && r.Runtime != v.Runtime) {
			// Preserve the original owner; one conflicting observation cannot
			// prevent unrelated tasks and the resident conversation from refreshing.
			r.View.Status = "unknown"
			r.Activity = ""
			conflict = &observationConflict{m.text("host.taskConflictOtherRuntime")}
		}
	}
	if _, ok := m.work.(api.WorkRouter); ok {
		observed := map[string]bool{}
		for _, v := range states {
			observed[v.Task.ID] = true
		}
		for id, r := range m.state.Records {
			if m.owns(r) && r.Runtime != "" && !observed[id] && !terminal(r.View.Status) {
				r.View.Status = "unknown"
				r.Activity = ""
			}
		}
	}
	for _, v := range states {
		if v.Task.ID == "" {
			continue
		}
		r := m.state.Records[v.Task.ID]
		if r == nil {
			r = &record{Runtime: v.Runtime, Provider: m.provider, Fingerprint: v.StartFingerprint, View: v.Task, Execution: v.ExecutionKey, ReportID: v.PreviousReportID, ReportState: v.PreviousReportState}
			m.state.Records[v.Task.ID] = r
		}
		if !m.owns(r) || r.View.Machine != v.Task.Machine || r.Runtime != "" && v.Runtime != "" && r.Runtime != v.Runtime {
			continue
		}
		if v.ExecutionKey != "" && slices.Contains(r.RetiredExecutions, v.ExecutionKey) {
			continue // A late old native snapshot cannot republish its report.
		}
		if r.Execution != "" && v.ExecutionKey != "" && r.Execution != v.ExecutionKey {
			r.RetiredExecutions = append(r.RetiredExecutions, r.Execution)
			if r.ReportID != "" {
				state := r.ReportState
				if state == "pending" {
					state = "observed" // superseded before dispatch
				}
				r.PreviousReports = append(r.PreviousReports, reportReceipt{ID: r.ReportID, State: state})
			}
		}
		if (r.Execution != "" && v.ExecutionKey != "" && r.Execution != v.ExecutionKey) || (terminal(r.View.Status) && !terminal(v.Task.Status)) {
			pin := true
			r.Pinned = &pin
			r.CompletedAt = 0
			r.ActiveAt = m.now().UnixMilli()
			m.promoteWatchLocked(v.Task.ID)
		}
		v.Task.Machine = r.View.Machine
		r.View = v.Task
		r.Activity = v.Activity
		r.StopResolution = v.StopResolution
		if r.OriginalPrompt == "" {
			r.OriginalPrompt = v.OriginalPrompt
		}
		if v.ExecutionKey != "" && (r.Execution != v.ExecutionKey || r.ReportID == "") {
			if r.Execution != v.ExecutionKey {
				// The uncertainty window belongs to the native execution, not
				// the reusable task handle. A new unknown run gets its own grace.
				r.UnknownSince = 0
			}
			r.Execution = v.ExecutionKey
			r.ReportID = "task-report-" + hash(r.Provider, v.Task.ID, v.ExecutionKey)
			r.ReportText = ""
			r.ReportState = "pending"
		}
		if v.StopAcknowledged && !v.StopPending && (v.Task.Status == "interrupted" || v.Task.Status == "cancelled" || v.Task.Status == "canceled") && r.ReportState == "pending" {
			r.ReportState = "observed"
		}
	}
	for _, r := range m.state.Records {
		if !m.owns(r) {
			continue
		}
		if r.View.Status == "unknown" {
			if r.UnknownSince == 0 {
				r.UnknownSince = m.now().UnixMilli()
			}
		} else {
			r.UnknownSince = 0
		}
	}
	m.metadataLocked()
	if err := m.write(); err != nil {
		return err
	}
	return conflict
}

type observationConflict struct{ message string }

func (e *observationConflict) Error() string { return e.message }

// A registered host route owns its retained backend independently of the
// resident adapter. Direct single-provider fixtures keep their existing scope.
func (m *Manager) owns(r *record) bool {
	if r == nil {
		return false
	}
	if r.Provider == m.provider || r.View.Machine != "" {
		return true
	}
	p, ok := m.work.(api.WorkRouter)
	return ok && r.Runtime != "" && p.OwnsWork(r.Runtime)
}

// HostReportIDs comes only from the native task ledger. A prior version may
// have submitted these exact IDs before the conversation binding tracked their
// origin. The resident adapter imports them before historical projection.
func (m *Manager) HostReportIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := []string{}
	for _, r := range m.state.Records {
		if r != nil && r.ReportID != "" && (r.ReportState == "delivered" || r.ReportState == "dispatching") {
			ids = append(ids, r.ReportID)
		}
		if r != nil {
			for _, report := range r.PreviousReports {
				if report.ID != "" && (report.State == "delivered" || report.State == "dispatching") {
					ids = append(ids, report.ID)
				}
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// TaskForReportID resolves only a recorded native task report receipt.
// It does not parse report prose or infer ownership from a current task.
func (m *Manager) TaskForReportID(reportID string) string {
	if reportID == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.state.Records {
		if r == nil || !m.owns(r) {
			continue
		}
		if r.ReportID == reportID {
			return r.View.ID
		}
		for _, previous := range r.PreviousReports {
			if previous.ID == reportID {
				return r.View.ID
			}
		}
	}
	return ""
}
func (m *Manager) ListTasks() []api.Task {
	// Projection is current even if persistence temporarily fails. Mutations and
	// report dispatch retry persistence and never use this read as admission.
	_ = m.refresh()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []api.Task{}
	for _, r := range m.state.Records {
		if m.owns(r) {
			v := r.View
			v.Result = ""
			if r.CreatedAt != 0 {
				v.CreatedAt = time.UnixMilli(r.CreatedAt).UTC().Format(time.RFC3339Nano)
			}
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) markInflight(id string, active bool) {
	m.mu.Lock()
	if active {
		if m.inflight == nil {
			m.inflight = map[string]bool{}
		}
		m.inflight[id] = true
	} else {
		delete(m.inflight, id)
	}
	m.mu.Unlock()
}

func (m *Manager) codexInflight(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.state.Records[id]
	return m.inflight[id] && r != nil &&
		(r.Runtime == "codex" || r.Runtime == "" && r.Provider == "codex")
}

func prepareWorkspace(root, id string, loc ...i18n.Locale) (string, error) {
	if e := localstate.EnsurePrivateDirectory(root); e != nil {
		return "", e
	}
	info, e := os.Lstat(root)
	if e != nil {
		return "", e
	}
	l := i18n.DefaultLocale
	if len(loc) > 0 && loc[0] != "" {
		l = loc[0]
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New(i18n.Text(l, "host.taskSymlinkRedirectForbidden", nil))
	}
	r, e := os.OpenRoot(root)
	if e != nil {
		return "", e
	}
	defer r.Close()
	if e = r.Mkdir(id, 0700); e != nil {
		return "", e
	}
	if e = localstate.ProtectDirectory(filepath.Join(root, id)); e != nil {
		return "", e
	}
	return filepath.Join(root, id), nil
}

func (m *Manager) StartTask(ctx context.Context, in api.TaskStart) (task api.Task, err error) {
	if !valid(in.RequestID, in.Prompt) || len(in.Title) > 160 {
		return api.Task{}, errors.New(m.text("host.taskRequiresParams"))
	}
	finishInputs := m.beginInputPreparation(len(in.Attachments) > 0 || len(in.Files) > 0)
	defer finishInputs()
	in.Files, err = m.inputFiles(ctx, in.Attachments, in.Files)
	if err != nil {
		return m.rejectStartInput(in.RequestID, err)
	}
	in.FileDigests, err = api.WorkInputDigests(in.Files)
	if err != nil {
		return m.rejectStartInput(in.RequestID, err)
	}
	in.Title = taskDisplayTitle(in.Title, in.Prompt)
	release, guardErr := m.environmentOperation(ctx, in.Machine)
	if guardErr != nil {
		return api.Task{}, guardErr
	}
	defer release()
	m.op.Lock()
	defer m.op.Unlock()
	if m.paused {
		return api.Task{}, errors.New(m.text("host.installingUpdateRetryLater"))
	}
	if e := m.refresh(); e != nil {
		return api.Task{}, e
	}
	id, fp := "task-"+hash(m.provider, in.RequestID), hash(in.Title, in.Prompt)
	if in.Machine != "" {
		id = "task-" + hash("remote", in.Machine, in.RequestID)
		fp = hash(in.Title, in.Prompt, in.Machine)
	}
	if in.Workspace != "" {
		fp = hash(in.Title, in.Prompt, in.Workspace)
		if in.Machine != "" {
			fp = hash(in.Title, in.Prompt, in.Workspace, in.Machine)
		}
	}
	if len(in.FileDigests) > 0 || len(in.Attachments) > 0 {
		fp = hash(fp, api.WorkFingerprint(in.FileDigests), api.WorkFingerprint(in.Attachments))
	}
	m.mu.Lock()
	if _, ok := m.work.(api.WorkRouter); ok && in.Machine == "" {
		// Stable requests survive a default/secretary switch. Legacy provider IDs
		// remain recognizable; a retry cannot allocate a second native Worker.
		id = "task-" + hash("local", in.RequestID)
		for _, provider := range []string{"codex", "caelis", m.provider} {
			candidate := "task-" + hash(provider, in.RequestID)
			if r := m.state.Records[candidate]; r != nil && m.owns(r) {
				id = candidate
				break
			}
		}
	}
	legacyID := "task-" + hash(in.RequestID)
	if r := m.state.Records[legacyID]; r != nil && m.owns(r) {
		id = legacyID
	}
	if r := m.state.Records[id]; r != nil {
		v := r.View
		owned := m.owns(r)
		same := r.Fingerprint == fp
		m.mu.Unlock()
		if !owned || !same {
			return v, errors.New(m.text("host.sameRequestIdDifferentTask"))
		}
		return v, nil
	}
	m.mu.Unlock()
	used, e := m.admissionUsage(ctx, in.Machine)
	if e != nil {
		return api.Task{}, e
	}
	if used >= m.maximumRunning() {
		return api.Task{}, errors.New(m.text("host.taskCapacityFull"))
	}
	if e := m.work.WorkAdmission(ctx); e != nil {
		return api.Task{}, e
	}
	runtime := ""
	if router, ok := m.work.(api.WorkRouter); ok {
		var err error
		runtime, err = router.BindWork(ctx, in, id, "")
		if err != nil {
			return api.Task{}, err
		}
	}
	// Until the task ledger is durable, failures are pre-dispatch. Release only
	// explicit reservations; legacy and unknown execution owners stay bound.
	ledgerWritten := false
	defer func() {
		if !ledgerWritten {
			if rollback, ok := m.work.(api.WorkPreparationRollback); ok {
				err = errors.Join(err, workerAction(m, in.Machine, func() error { return rollback.ReleaseWorkPreparation(id) }))
			}
		}
	}()
	workspace := filepath.Join(m.root, id)
	if in.Machine != "" {
		p, ok := m.work.(api.RemoteWorkspaceRuntime)
		if !ok {
			return api.Task{}, errors.New("remote work unavailable")
		}
		var err error
		workspace, err = workerIO(m, in.Machine, func() (string, error) { return p.PrepareRemoteWork(ctx, in, id) })
		if err != nil {
			return api.Task{}, err
		}
		if router, ok := m.work.(api.WorkRouter); ok {
			runtime, err = router.BindWork(ctx, in, id, "")
			if err != nil {
				return api.Task{}, err
			}
		}
	} else if in.Workspace != "" {
		var err error
		workspace, err = api.ResolveTaskWorkspace(in.Workspace)
		if err != nil {
			return api.Task{Outcome: "rejected"}, &api.WorkMessageRejection{Cause: err}
		}
	}
	if e := workerAction(m, in.Machine, func() error { return m.validateWorkerInputs(ctx, id, in.Files) }); e != nil {
		return api.Task{Outcome: "rejected"}, &api.WorkMessageRejection{Cause: e}
	}
	if m.paused {
		return api.Task{}, errors.New(m.text("host.installingUpdateRetryLater"))
	}
	nativeStart := api.WorkStart{TaskStart: in, ID: id, Workspace: workspace, Instructions: botpolicy.WorkerInstructions}
	nativeIntent := nativeStart
	nativeIntent.Machine = ""
	m.mu.Lock()
	pinned := true
	r := &record{Operations: map[string]api.WorkOperationQuery{in.RequestID: {TaskID: id, RequestID: in.RequestID, Kind: "start", Fingerprint: api.WorkFingerprint(nativeIntent)}}, Requests: []string{in.RequestID}, Runtime: runtime, CreatedAt: m.now().UnixMilli(), ActiveAt: m.now().UnixMilli(), Pinned: &pinned, Provider: m.provider, Fingerprint: fp, OriginalPrompt: in.Prompt, View: api.Task{ID: id, Machine: in.Machine, Title: in.Title, Workspace: workspace, Status: "unknown", Outcome: "unknown"}}
	r.FileStorageVersion = 1
	r.retainInputFiles(in.RequestID, in.Attachments, in.Files)
	m.state.Sequence++
	r.Sequence = m.state.Sequence
	m.promoteWatchLocked(id)
	m.state.Records[id] = r
	e = m.write()
	if e != nil {
		m.fileStorageUncertain = true
		delete(m.state.Records, id)
	}
	m.mu.Unlock()
	if e != nil {
		return api.Task{}, e
	}
	// Publish the durable pin even if native admission later becomes uncertain.
	ledgerWritten = true
	defer m.notifyWatchlist()
	if in.Workspace == "" && in.Machine == "" {
		workspace, e = prepareWorkspace(m.root, id, m.currentLocale())
	}
	if e != nil {
		m.mu.Lock()
		r.View.Status = "failed"
		r.View.Outcome = "rejected"
		delete(r.InputPaths, in.RequestID)
		delete(r.InputReferences, in.RequestID)
		saveErr := m.write()
		if saveErr != nil {
			m.fileStorageUncertain = true
		}
		v := r.View
		m.mu.Unlock()
		return v, &api.WorkMessageRejection{Cause: errors.Join(e, saveErr)}
	}
	m.markInflight(id, true)
	defer m.markInflight(id, false)
	v, e := workerIO(m, in.Machine, func() (api.Task, error) { return m.work.StartWork(ctx, nativeStart) })
	result, captureErr := m.capture(id, v, e)
	if result.Outcome == "rejected" {
		captureErr = errors.Join(captureErr, m.releaseRejectedInputFiles(id, in.RequestID))
	}
	if m.started != nil && result.ID == id && result.Outcome != "rejected" && (v.ID == "" || v.ID == id) {
		visible := result
		if captureErr != nil {
			visible.Status, visible.Outcome = "unknown", "unknown"
		}
		m.started(ctx, visible)
	}
	return result, captureErr
}

func taskDisplayTitle(title, prompt string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	line := ""
	for _, part := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(part)
		if line != "" {
			break
		}
	}
	runes := []rune(line)
	if len(runes) > 38 {
		return string(runes[:38]) + "…"
	}
	return line
}

// ObserveStarts emits one presentation event for a newly allocated task.
func (m *Manager) ObserveStarts(started func(api.Task)) {
	m.op.Lock()
	defer m.op.Unlock()
	m.started = func(_ context.Context, task api.Task) { started(task) }
}

// ObserveStartsWithContext preserves the original Bot tool invocation for
// presentation routing without making the Worker task a channel-specific object.
func (m *Manager) ObserveStartsWithContext(started func(context.Context, api.Task)) {
	m.op.Lock()
	defer m.op.Unlock()
	m.started = started
}

func (m *Manager) capture(id string, v api.Task, callErr error) (api.Task, error) {
	if e := m.refresh(); e != nil {
		callErr = errors.Join(callErr, e)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.state.Records[id]
	if v.ID != "" && v.ID != id {
		return r.View, errors.Join(callErr, errors.New(m.text("host.runtimeReturnedDifferentTask")))
	}
	if e := m.write(); e != nil {
		callErr = errors.Join(callErr, e)
	}
	out := r.View
	if v.ID == id && v.Outcome != "" {
		out.Outcome = v.Outcome
	}
	return out, callErr
}
func (m *Manager) owned(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.state.Records[id]
	if r == nil || !m.owns(r) {
		return errors.New(m.text("host.onlyOperateBotCreatedTasks"))
	}
	return nil
}
func (m *Manager) ReadTask(ctx context.Context, id string) (api.Task, error) {
	if m.codexInflight(id) {
		// The adapter's ReadWork shares its submission lock. During an admitted
		// submission, WorkStates is the non-dispatching live projection.
		if err := m.owned(id); err != nil {
			return api.Task{}, err
		}
		if err := m.refresh(); err != nil {
			return api.Task{}, err
		}
		m.mu.Lock()
		r := m.state.Records[id]
		v := r.View
		var err error
		if terminal(v.Status) && r.ReportState == "pending" {
			r.ReportState = "observed"
			err = m.write()
		}
		m.mu.Unlock()
		return v, err
	}
	machine := m.taskEnvironment(id)
	release, err := m.environmentOperation(ctx, machine)
	if err != nil {
		return api.Task{}, err
	}
	defer release()
	m.op.Lock()
	defer m.op.Unlock()
	if e := m.owned(id); e != nil {
		return api.Task{}, e
	}
	v, e := workerIO(m, machine, func() (api.Task, error) { return m.work.ReadWork(ctx, id) })
	v, e = m.capture(id, v, e)
	if e == nil && terminal(v.Status) {
		m.mu.Lock()
		if m.state.Records[id].ReportState == "pending" {
			m.state.Records[id].ReportState = "observed"
		}
		e = m.write()
		m.mu.Unlock()
	}
	return v, e
}

// captureMessage keeps this call's evidence separate from the task's older
// display outcome. An arbitrary owner/transport error remains uncertain.
func (m *Manager) captureMessage(id string, v api.Task, callErr error, reconciliation bool) (api.Task, error) {
	outcome := "unknown"
	var rejected *api.WorkMessageRejection
	if !reconciliation && (v.ID == "" || v.ID == id) && errors.As(callErr, &rejected) {
		outcome = "rejected"
	} else if callErr == nil && v.ID == id && (v.Outcome == "accepted" || v.Outcome == "rejected") {
		outcome = v.Outcome
	}
	out, err := m.capture(id, v, callErr)
	out.Outcome = outcome
	return out, err
}

func (m *Manager) rejectMessage(in api.TaskMessage, err error) (api.Task, error) {
	m.mu.Lock()
	r := m.state.Records[in.ID]
	recorded := r != nil && slices.Contains(r.Requests, in.RequestID)
	m.mu.Unlock()
	if recorded {
		// Refusing this reconciliation call says nothing about an earlier dispatch.
		return api.Task{ID: in.ID, Outcome: "unknown"}, err
	}
	return api.Task{Outcome: "rejected"}, &api.WorkMessageRejection{Cause: err}
}

func (m *Manager) SendTask(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	finishInputs := m.beginInputPreparation(len(in.Attachments) > 0 || len(in.Files) > 0)
	defer finishInputs()
	var fileErr error
	in.Files, fileErr = m.inputFiles(ctx, in.Attachments, in.Files)
	if fileErr != nil {
		return m.rejectMessage(in, fileErr)
	}
	in.FileDigests, fileErr = api.WorkInputDigests(in.Files)
	if fileErr != nil {
		return m.rejectMessage(in, fileErr)
	}
	if !valid(in.RequestID, in.Prompt) {
		return m.rejectMessage(in, errors.New(m.text("host.requiresRequestIdAndRequirements")))
	}
	machine := m.taskEnvironment(in.ID)
	release, guardErr := m.environmentOperation(ctx, machine)
	if guardErr != nil {
		return m.rejectMessage(in, guardErr)
	}
	defer release()
	m.op.Lock()
	defer m.op.Unlock()
	if m.paused {
		return m.rejectMessage(in, errors.New(m.text("host.installingUpdateRetryLater")))
	}
	if err := m.refresh(); err != nil {
		return m.rejectMessage(in, err)
	}
	if e := m.owned(in.ID); e != nil {
		return m.rejectMessage(in, e)
	}
	if err := m.reconcileFinishedBeforeInput(in.ID); err != nil {
		return m.rejectMessage(in, err)
	}
	m.mu.Lock()
	alreadyRecorded := slices.Contains(m.state.Records[in.ID].Requests, in.RequestID)
	m.mu.Unlock()
	if !alreadyRecorded {
		if e := workerAction(m, machine, func() error { return m.validateWorkerInputs(ctx, in.ID, in.Files) }); e != nil {
			return m.rejectMessage(in, e)
		}
	}
	m.mu.Lock()
	current := m.state.Records[in.ID]
	status := current.View.Status
	duplicate := slices.Contains(current.Requests, in.RequestID)
	if !duplicate && api.WorkerQuestionAnswer(ctx) != "" && current.FinishedExecution != "" && current.FinishedExecution == current.Execution {
		m.mu.Unlock()
		return m.rejectMessage(in, errors.New("original task explicitly finished"))
	}
	if duplicate {
		prior := current.View
		outcome := current.RequestOutcomes[in.RequestID]
		fingerprint, known := current.RequestFingerprints[in.RequestID]
		m.mu.Unlock()
		if known && fingerprint != messageFingerprint(in) {
			return prior, errors.New(m.text("host.sameRequestIdDifferentTask"))
		}
		if known && outcome != "" && outcome != "unknown" {
			prior.Outcome = outcome
			return prior, nil
		}
		if native, ok := m.work.(api.WorkOperationReader); ok {
			m.mu.Lock()
			query, has := current.Operations[in.RequestID]
			m.mu.Unlock()
			if has {
				receipt, readErr := workerIO(m, machine, func() (api.WorkOperation, error) { return native.ReadWorkOperation(ctx, query) })
				if readErr == nil {
					prior.Outcome = receipt.Outcome
					if receipt.Outcome == "accepted" || receipt.Outcome == "rejected" {
						m.mu.Lock()
						if current.RequestOutcomes == nil {
							current.RequestOutcomes = map[string]string{}
						}
						current.RequestOutcomes[in.RequestID] = receipt.Outcome
						readErr = m.write()
						m.mu.Unlock()
					}
					return prior, readErr
				}
				if !errors.Is(readErr, api.ErrWorkOperationUnsupported) {
					prior.Outcome = "unknown"
					return prior, readErr
				}
			}
		}
		// Older ledgers contain only a boolean confirmation, which cannot
		// distinguish acceptance from rejection. Reconcile the original native
		// receipt rather than inventing an accepted outcome.
		recorded, ok := m.work.(api.RecordedWorkMessage)
		if !ok || !func() bool {
			v, _ := workerIO(m, machine, func() (bool, error) { return recorded.WorkMessageRecorded(in), nil })
			return v
		}() {
			prior.Outcome = "unknown"
			return prior, errors.New("original submission receipt is not confirmed; no Worker prompt was replayed")
		}
		m.markInflight(in.ID, true)
		defer m.markInflight(in.ID, false)
		v, err := workerIO(m, machine, func() (api.Task, error) { return m.work.SendWork(ctx, in) })
		v, err = m.captureMessage(in.ID, v, err, true)
		if v.Outcome == "rejected" || err == nil && v.Outcome == "accepted" {
			m.mu.Lock()
			if current.RequestOutcomes == nil {
				current.RequestOutcomes = map[string]string{}
			}
			if current.RequestFingerprints == nil {
				current.RequestFingerprints = map[string]string{}
			}
			current.RequestOutcomes[in.RequestID] = v.Outcome
			current.RequestFingerprints[in.RequestID] = messageFingerprint(in)
			if current.Operations == nil {
				current.Operations = map[string]api.WorkOperationQuery{}
			}
			current.Operations[in.RequestID] = api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "send", Fingerprint: api.WorkFingerprint(in)}
			err = errors.Join(err, m.write())
			m.mu.Unlock()
		}
		return v, err
	}
	if status == "unknown" || status == "pending" {
		m.mu.Unlock()
		return m.rejectMessage(in, errors.New("original task turn or receipt unresolved; read the original task before continuing"))
	}
	restarting := terminal(status)
	m.mu.Unlock()
	used := 0
	if restarting {
		var err error
		used, err = m.admissionUsage(ctx, machine)
		if err != nil {
			return m.rejectMessage(in, err)
		}
	}
	if restarting && used >= m.maximumRunning() {
		replay, ok := m.work.(api.RecordedWorkMessage)
		if !ok || !func() bool {
			v, _ := workerIO(m, machine, func() (bool, error) { return replay.WorkMessageRecorded(in), nil })
			return v
		}() {
			return m.rejectMessage(in, errors.New(m.text("host.taskCapacityFull")))
		}
	}
	if m.paused {
		return m.rejectMessage(in, errors.New(m.text("host.installingUpdateRetryLater")))
	}
	m.mu.Lock()
	r := m.state.Records[in.ID]
	if !slices.Contains(r.Requests, in.RequestID) {
		r.Requests = append(r.Requests, in.RequestID)
		if r.RequestFingerprints == nil {
			r.RequestFingerprints = map[string]string{}
		}
		r.RequestFingerprints[in.RequestID] = messageFingerprint(in)
		if r.Operations == nil {
			r.Operations = map[string]api.WorkOperationQuery{}
		}
		r.Operations[in.RequestID] = api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "send", Fingerprint: api.WorkFingerprint(in)}
		r.retainInputFiles(in.RequestID, in.Attachments, in.Files)
		if e := m.write(); e != nil {
			m.fileStorageUncertain = true
			r.Requests = r.Requests[:len(r.Requests)-1]
			delete(r.RequestFingerprints, in.RequestID)
			m.mu.Unlock()
			return m.rejectMessage(in, e)
		}
	}
	m.mu.Unlock()
	defer m.notifyWatchlist()
	m.markInflight(in.ID, true)
	defer m.markInflight(in.ID, false)
	v, e := workerIO(m, machine, func() (api.Task, error) { return m.work.SendWork(ctx, in) })
	v, e = m.captureMessage(in.ID, v, e, false)
	if v.Outcome == "rejected" {
		e = errors.Join(e, m.releaseRejectedInputFiles(in.ID, in.RequestID))
	}
	if v.Outcome == "rejected" || e == nil && v.Outcome == "accepted" {
		m.mu.Lock()
		if r.RequestOutcomes == nil {
			r.RequestOutcomes = map[string]string{}
		}
		r.RequestOutcomes[in.RequestID] = v.Outcome
		e = errors.Join(e, m.write())
		m.mu.Unlock()
	}
	return v, e
}

// The operation lock spans this check and native dispatch, so parallel starts
// cannot claim the same last slot. Unknown work with unreadable activity keeps
// one possible slot each. Only when full do we try original-owner reads to
// release positively idle tasks; a failed read never frees its reservation.
func (m *Manager) admissionUsage(ctx context.Context, machine string) (int, error) {
	m.mu.Lock()
	used := m.environmentUsageLocked(machine)
	m.mu.Unlock()
	if used < m.maximumRunning() {
		return used, nil
	}
	m.mu.Lock()
	ids := make([]string, 0)
	for id, r := range m.state.Records {
		if m.owns(r) && r.View.Machine == machine && r.View.Status == "unknown" {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		// A read error leaves that task unknown and reserved. Other owners may
		// still provide idle capacity evidence without retrying any execution.
		_, _ = workerIO(m, machine, func() (api.Task, error) { return m.work.ReadWork(ctx, id) })
	}
	if err := m.refresh(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.environmentUsageLocked(machine), nil
}

func (m *Manager) StopTask(ctx context.Context, id string) (api.Task, error) {
	if m.codexInflight(id) {
		// StopWork cancels the adapter's capacity wait before acquiring its
		// submission lock. Do not wait behind this task's Manager submission.
		if err := m.owned(id); err != nil {
			return api.Task{}, err
		}
		v, err := m.stopWork(ctx, id)
		return m.capture(id, v, err)
	}
	m.op.Lock()
	defer m.op.Unlock()
	if e := m.owned(id); e != nil {
		return api.Task{}, e
	}
	v, e := workerIO(m, m.taskEnvironment(id), func() (api.Task, error) { return m.stopWork(ctx, id) })
	return m.capture(id, v, e)
}

// DeliverTaskReport is finite: native generation -> one durable dispatch.
// Uncertain delivery is only reconciled, never automatically resubmitted.
func (m *Manager) DeliverTaskReport(ctx context.Context) error {
	m.op.Lock()
	defer m.op.Unlock()
	if m.paused {
		return nil
	}
	if e := m.refresh(); e != nil {
		return e
	}
	s := m.snapshot()
	m.mu.Lock()
	for _, r := range m.state.Records {
		if m.owns(r) && r.ReportState == "dispatching" && s.LastReceipt.ID == r.ReportID && s.LastReceipt.Outcome == "accepted" {
			r.ReportState = "delivered"
		}
		if m.owns(r) && s.LastReceipt.Outcome == "accepted" {
			for i := range r.PreviousReports {
				if r.PreviousReports[i].State == "dispatching" && r.PreviousReports[i].ID == s.LastReceipt.ID {
					r.PreviousReports[i].State = "delivered"
				}
			}
		}
	}
	if e := m.write(); e != nil {
		m.mu.Unlock()
		return e
	}
	if !s.CanSend {
		m.mu.Unlock()
		return nil
	}
	ids := make([]string, 0, len(m.state.Records))
	for id := range m.state.Records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var selected *record
	for _, id := range ids {
		r := m.state.Records[id]
		if m.owns(r) && terminal(r.View.Status) && r.ReportState == "pending" && r.ReportID != "" {
			selected = r
			break
		}
	}
	if selected == nil {
		m.mu.Unlock()
		return nil
	}
	id := selected.ReportID
	text := fmt.Sprintf("Task %s is %s.", selected.View.ID, selected.View.Status)
	if selected.StopResolution == "interrupted" {
		text = fmt.Sprintf("Task %s: the original native turn ended interrupted after a stop request. Earlier tool effects remain subject to their native receipts.", selected.View.ID)
	} else if selected.StopResolution != "" {
		text = fmt.Sprintf("Task %s: stop was requested, but its original native turn ended %s without confirmed timely interruption. The native result may have taken effect.", selected.View.ID, selected.StopResolution)
	}
	if selected.View.Failure != nil && selected.View.Status == "failed" {
		failure, _ := json.Marshal(selected.View.Failure)
		text += " Failure: " + string(failure)
	}
	if selected.ReportText == "" {
		selected.ReportText = text
	}
	text = selected.ReportText
	selected.ReportState = "dispatching"
	if e := m.write(); e != nil {
		selected.ReportState = "pending"
		m.mu.Unlock()
		return e
	}
	m.mu.Unlock()
	receipt, e := m.reports.SubmitReport(ctx, api.Submission{ID: id, Text: text})
	m.mu.Lock()
	defer m.mu.Unlock()
	if selected.ReportID == id {
		if receipt.ID == id && receipt.Outcome == "accepted" {
			selected.ReportState = "delivered"
		} else if e == nil && receipt.ID == id && receipt.Outcome == "rejected" {
			selected.ReportState = "pending"
		}
	} else if receipt.ID == id && receipt.Outcome == "accepted" {
		for i := range selected.PreviousReports {
			if selected.PreviousReports[i].ID == id {
				selected.PreviousReports[i].State = "delivered"
			}
		}
	}
	return errors.Join(e, m.write())
}

var _ api.TaskProvider = (*Manager)(nil)
var _ api.TaskReporter = (*Manager)(nil)

func (m *Manager) TaskMachines() []api.TaskMachine {
	if p, ok := m.work.(api.TaskMachineProvider); ok {
		return p.TaskMachines()
	}
	return []api.TaskMachine{}
}

func (m *Manager) ObserveTaskExecution(id string) (api.Task, string, error) {
	if err := m.refresh(); err != nil {
		return api.Task{}, "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.state.Records[id]
	if r == nil || !m.owns(r) || r.Execution == "" {
		return api.Task{}, "", errors.New("original task execution unavailable")
	}
	v := r.View
	v.Result = ""
	return v, r.Execution, nil
}

// SSH configuration is a host file, never a Worker capability or a grant.
func (m *Manager) SSHConfiguration() (api.SSHConfiguration, error) {
	p, ok := m.work.(interface {
		SSHConfiguration() (api.SSHConfiguration, error)
	})
	if !ok {
		return api.SSHConfiguration{}, errors.New("ssh_unavailable")
	}
	return p.SSHConfiguration()
}
