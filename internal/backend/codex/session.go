package codex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/contextseed"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type binding struct {
	// Exact native owner used by this binding. An update detaches its observer;
	// the next process must reconcile here before accepting new work.
	OwnerEndpoint      string                          `json:"ownerEndpoint,omitempty"`
	PendingApprovalIDs []string                        `json:"pendingApprovalIds,omitempty"`
	RuntimeVersion     string                          `json:"runtimeVersion,omitempty"`
	BackgroundResults  map[string]api.BackgroundResult `json:"backgroundResults,omitempty"`

	Context        contextseed.State      `json:"context,omitempty"`
	ContextInputs  map[string]int         `json:"contextInputs,omitempty"`
	Dreams         map[string]dreamRecord `json:"dreams,omitempty"`
	PastThreads    []string               `json:"pastThreads,omitempty"`
	RenewedBy      string                 `json:"renewedBy,omitempty"`
	Scheduled      map[string]string      `json:"scheduled,omitempty"`   // accepted client IDs to native turn IDs
	HostReports    map[string]bool        `json:"hostReports,omitempty"` // host-only completion inputs, keyed by exact submitted ID
	CleanupTargets []string               `json:"cleanupTargets,omitempty"`
	StopState      string                 `json:"stopState,omitempty"` // prepared is safe to retry; attempted/legacy is not.
	StopRuns       map[string]string      `json:"stopRuns,omitempty"`

	Tasks          map[string]*taskRecord `json:"tasks,omitempty"`
	DelegationText string                 `json:"delegationText,omitempty"`
	Children       []string               `json:"children,omitempty"`
	Version        int                    `json:"version"`
	ThreadID       string                 `json:"threadId"`
	// True only for a newly created thread that has never reached native turn/start.
	LastReceipt *api.Receipt       `json:"lastReceipt,omitempty"`
	Unsubmitted bool               `json:"unsubmitted,omitempty"`
	Pending     *pendingSubmission `json:"pending,omitempty"`
}
type pendingSubmission struct {
	ID     string `json:"id"`
	TurnID string `json:"turnId"`
}
type SessionOptions struct {
	Diagnostics                          *diagnosticlog.Logger
	WorkExecution                        api.WorkExecutionSettings
	Execution                            api.ExecutionSettings
	Binary, Socket, Directory, StateFile string
	WorkRoot                             string
	RequiredSocket                       bool
	// RequireApproval tightens policy for isolated acceptance runs. The desktop
	// defaults to on-request; this flag can never weaken its sandbox.
	RequireApproval bool
	// Host-only MCP config; never supplied by the renderer.
	BotTools *api.ToolConnection
}

// Session projects one internally bound conversation. Native facts remain
// authoritative; a UI fetch, hidden window or character asset cannot execute it.
type Session struct {
	workerOnly             bool // trusted target owner; no resident API is exposed
	backgroundResultsDirty bool

	residentExecution   api.WorkExecutionSettings
	usage               api.ContextUsage
	usageTurn           string
	usageTotal          int64
	historyMu           sync.Mutex
	op                  sync.Mutex
	mu                  sync.Mutex
	opts                SessionOptions
	client              *Client
	retainedOwner       *Client
	forceNewOwner       bool
	bound               bool
	epoch               uint64
	reconnectSeq        uint64
	reconnectActive     bool
	reconnectCancel     context.CancelFunc
	reconnectAttempts   int
	reconnectDelay      func(int) time.Duration
	lastConnectCause    error
	state               api.Snapshot
	binding             binding
	loadErr             error
	loading             bool
	buffer              []Notification
	bufferBytes         int
	run                 string
	runs                map[string]string
	items               map[string]int
	nativeItems         map[string]nativeItem
	reviewOrigins       map[string]reviewOrigin
	prompts             map[string]*prompt
	promptHandles       map[string]string
	instance            string
	refs                map[string]nativeReference
	artifacts           map[string]string
	historyCursor       string
	historyThread       string
	historyPrevious     int
	lastTurn            string
	historyPaged        bool
	children            map[string]bool
	childRuns           map[string]string
	childTerminals      map[string]bool
	childTerminalStatus map[string]string
	childWatching       map[string]bool
	childSubscribed     map[string]bool
	childRetireSeq      map[string]uint64
	childRetired        map[string]bool
	workerIdleDelay     time.Duration
	retireUnsupported   bool
	retireFailures      uint64
	retiredWorkers      uint64
	nativeUnloads       uint64
	nativeResources     nativeResourceSnapshot
	childRevision       map[string]uint64
	loginID             string
	loginStarting       bool
	earlyLogin          map[string]bool
	changed             chan struct{}
	closed              bool
	closing             bool
	life                context.Context
	cancelLife          context.CancelFunc
	start               func(context.Context, Options) (*Client, error)
}

func NewSession(opts SessionOptions) *Session {
	s := &Session{opts: opts, binding: binding{Version: 1}, changed: make(chan struct{}), instance: rand.Text(), start: Start}
	s.opts.BotTools = opts.BotTools.Clone()
	s.life, s.cancelLife = context.WithCancel(context.Background())
	s.resetProjection()
	s.state.Connection = "offline"
	s.state.Phase = "idle"
	if b, err := os.ReadFile(opts.StateFile); err == nil {
		if json.Unmarshal(b, &s.binding) != nil || s.binding.Version != 1 {
			s.loadErr = errors.New("对话记录无法读取，请保留该文件并重试")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		s.loadErr = errors.New("无法读取本地对话记录")
	}
	if s.binding.Scheduled == nil {
		s.binding.Scheduled = map[string]string{}
	}
	if s.binding.HostReports == nil {
		s.binding.HostReports = map[string]bool{}
	}
	// Older adapter task records carry typed report identities. The product task
	// ledger is imported separately because it can derive a different report ID.
	for _, task := range s.binding.Tasks {
		if task != nil && task.ReportID != "" {
			s.binding.HostReports[task.ReportID] = true
		}
	}
	if s.binding.LastReceipt != nil {
		s.state.LastReceipt = *s.binding.LastReceipt
	}
	if s.opts.BotTools != nil {
		for _, t := range s.binding.Tasks {
			if t != nil && t.Instructions == "" {
				t.Instructions = s.opts.BotTools.WorkerInstructions
			}
		}
	}
	if err := validateExecution(opts.Execution); err != nil {
		s.loadErr = err
	}
	if s.loadErr != nil {
		s.state.Message = s.loadErr.Error()
	}
	return s
}

// ImportHostReportIDs migrates submitted report identities from the native
// product task ledger before conversation history is projected. It is not a
// renderer command and never infers origin from prose or an ID pattern.
func (s *Session) ImportHostReportIDs(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	added := []string{}
	for _, id := range ids {
		if id != "" && !s.binding.HostReports[id] {
			s.binding.HostReports[id] = true
			added = append(added, id)
		}
	}
	if len(added) > 0 {
		if err := s.save(); err != nil {
			for _, id := range added {
				delete(s.binding.HostReports, id)
			}
			return err
		}
	}
	return nil
}
func (s *Session) resetProjection() {
	s.usage, s.usageTurn, s.usageTotal = api.ContextUsage{}, "", 0
	s.runs = map[string]string{}
	s.items = map[string]int{}
	s.nativeItems = map[string]nativeItem{}
	s.prompts = map[string]*prompt{}
	s.promptHandles = map[string]string{}
	s.refs = map[string]nativeReference{}
	s.artifacts = map[string]string{}
	s.children = map[string]bool{}
	for _, id := range s.binding.Children {
		s.children[id] = true
	}
	for _, task := range s.binding.Tasks {
		if task.Thread != "" {
			s.children[task.Thread] = true
		}
	}
	s.childRuns = map[string]string{}
	s.childTerminals = map[string]bool{}
	s.childTerminalStatus = map[string]string{}
	s.childWatching = map[string]bool{}
	s.childSubscribed = map[string]bool{}
	s.childRetireSeq = map[string]uint64{}
	s.childRetired = map[string]bool{}
	s.retireUnsupported = false
	s.nativeResources = nativeResourceSnapshot{}
	if s.workerIdleDelay == 0 {
		s.workerIdleDelay = workerRetirementDelay
	}
	s.childRevision = map[string]uint64{}
	s.state.Items = []api.Item{}
	s.state.Approvals = []api.Approval{}
	s.state.Reviews = []api.Review{}
	s.reviewOrigins = map[string]reviewOrigin{}
	s.state.References = []api.Reference{}
	s.historyCursor = ""
	s.historyThread = s.binding.ThreadID
	s.historyPrevious = len(s.binding.PastThreads) - 1
	s.lastTurn = ""
	s.historyPaged = false
	s.state.HasEarlier = false
	s.run = ""
}
func opaque(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:16])
}
func (s *Session) update() {
	if s.hasConversationPrompt() && s.state.Connection == "ready" && s.state.Phase != "unknown" {
		s.state.Phase = "attention"
	} else if s.hasBlockingChildren() && s.state.Phase != "unknown" && s.state.Phase != "interrupting" {
		s.state.Phase = "working"
	}
	s.state.CurrentTurn = ""
	if s.run != "" {
		s.state.CurrentTurn = opaque(s.run)
	}
	if s.captureBackgroundResults() {
		s.backgroundResultsDirty = true
	}
	if s.backgroundResultsDirty {
		if err := s.save(); err != nil {
			s.state.Phase = "unknown"
			s.state.Message = "后台结果记录保存失败，请恢复连接核对"
		} else {
			s.backgroundResultsDirty = false
		}
	}
	s.state.Revision++
	s.state.CanSend = s.state.Connection == "ready" && s.binding.Pending == nil && len(s.binding.CleanupTargets) == 0 && s.run == "" && !s.hasBlockingChildren() && !s.hasConversationPrompt() && s.state.Phase != "unknown" && !s.closed && !s.closing
	s.state.CanSteer = s.state.Connection == "ready" && s.binding.Pending == nil && len(s.binding.CleanupTargets) == 0 && s.run != "" && s.state.Phase == "working" && !s.hasConversationPrompt() && !s.closed && !s.closing
	s.state.CanInterrupt = s.state.Connection == "ready" && (len(s.binding.CleanupTargets) == 0 || s.binding.StopState == "prepared") && (s.run != "" || s.hasBlockingChildren()) && !s.closed && !s.closing
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *Session) Snapshot() api.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Return owned immutable data to concurrent Wails JSON encoders.
	b, _ := json.Marshal(s.state)
	var out api.Snapshot
	_ = json.Unmarshal(b, &out)
	return s.presentScheduled(out)
}
func (s *Session) save() error {
	if s.opts.StateFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.opts.StateFile), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(s.binding)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.opts.StateFile), ".binding-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.opts.StateFile)
	}
	if err == nil {
		err = localstate.SyncParent(s.opts.StateFile)
	}
	return err
}
func (s *Session) Connect(ctx context.Context) error {
	s.mu.Lock()
	s.reconnectSeq++
	if s.reconnectCancel != nil {
		s.reconnectCancel()
	}
	s.reconnectActive = false
	s.mu.Unlock()
	s.op.Lock()
	defer s.op.Unlock()
	return s.connect(ctx)
}

func (s *Session) RecoveryState() api.RecoveryState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return api.RecoveryState{
		Fence:     fmt.Sprintf("codex:%s:%d", s.instance, s.epoch),
		Automatic: s.reconnectActive,
		Manual:    s.epoch > 0 && s.state.Connection == "offline" && !s.reconnectActive && !s.closed && !s.closing,
	}
}
func (s *Session) connect(ctx context.Context) error {
	ctx, cancel := s.operation(ctx, 30*time.Second)
	defer cancel()
	s.mu.Lock()
	if s.closed || s.closing {
		s.mu.Unlock()
		return errors.New("应用正在退出")
	}
	if s.loadErr != nil {
		err := s.loadErr
		s.mu.Unlock()
		return err
	}
	if s.client != nil && s.client.Err() == nil && s.state.Connection == "ready" {
		c, id, pending := s.client, s.binding.ThreadID, s.binding.Pending != nil
		cleanup := append([]string(nil), s.binding.CleanupTargets...)
		s.recoverUnresolvedChildren(c, s.epoch)
		s.mu.Unlock()
		if len(cleanup) > 0 {
			if err := s.reconcileTerminalCleanup(ctx, c); err != nil {
				return err
			}
		}
		if !pending {
			return nil
		}
		// Observe a live owner in place. Restarting it could abandon a dispatched tool.
		var response struct {
			Thread nativeThread `json:"thread"`
		}
		if err := callDecode(ctx, c, "thread/read", map[string]any{"threadId": id, "includeTurns": true}, &response); err != nil || response.Thread.ID != id {
			return errors.New("暂时无法核对发送结果，请稍后重新连接")
		}
		s.mu.Lock()
		for _, turn := range response.Thread.Turns {
			if terminal(turn.Status) {
				s.applyTurn(turn, true)
				continue
			}
			if terminal(s.runs[turn.ID]) {
				continue
			}
			// A read snapshot may precede events already consumed on this live owner.
			// Reconcile identities/new items without overwriting streamed item contents.
			if s.run == "" {
				s.run = turn.ID
				s.runs[turn.ID] = turn.Status
			}
			for _, item := range turn.Items {
				if _, exists := s.nativeItems[opaque(turn.ID, item.ID)]; !exists || item.Type == "userMessage" {
					s.applyItem(turn.ID, item, false)
				}
			}
		}
		if s.binding.Pending == nil {
			s.state.Message = ""
			if s.run != "" {
				if s.hasConversationPrompt() {
					s.state.Phase = "attention"
				} else {
					s.state.Phase = "working"
				}
			} else {
				s.state.Phase = "idle"
			}
		}
		s.update()
		s.mu.Unlock()
		return nil
	}
	old := s.client
	retained := s.retainedOwner
	if s.forceNewOwner {
		retained = nil
		s.forceNewOwner = false
	}
	if old != nil && old.owner != nil && retained != nil {
		retained = old
		s.retainedOwner = old
	}
	// A live owner in another state may still own background tools.
	if old != nil && old.Err() == nil && s.bound {
		if !old.UsesSharedServer() && s.hasUnresolvedTasks() {
			s.mu.Unlock()
			return errors.New("独立任务仍在运行，暂不替换其连接；请等待任务状态核对")
		}
		cleanupTargets := []string{s.binding.ThreadID}
		for id := range s.childRuns {
			if s.taskByThread(id) == nil {
				cleanupTargets = append(cleanupTargets, id)
			}
		}
		s.mu.Unlock()
		if err := s.cleanTerminals(ctx, old, cleanupTargets...); err != nil {
			return errors.New("原连接的后台工具尚未确认清理，请重试")
		}
		s.mu.Lock()
	}
	s.loginID = ""
	s.lastConnectCause = nil
	s.state.LoginPending = false
	s.client = nil
	s.epoch++
	epoch := s.epoch
	s.state.Connection = "connecting"
	s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "codex", Code: "recovery_started", Generation: epoch, Phase: "binding_recovery"})
	s.state.ConnectionIssue = ""
	s.state.Message = ""
	s.loading = true
	s.buffer = nil
	s.bufferBytes = 0
	s.update()
	s.mu.Unlock()
	if old != nil {
		if retained != nil && old == retained {
			old.detachForReconnect()
		} else {
			old.Close()
		}
	}
	if err := os.MkdirAll(s.opts.Directory, 0700); err != nil {
		return s.connectionError("无法准备工作文件夹", err)
	}
	startOptions := Options{Diagnostics: s.opts.Diagnostics, Binary: s.opts.Binary, Socket: s.opts.Socket, RequiredSocket: s.opts.RequiredSocket, Directory: s.opts.Directory, Experimental: true, HandleRequests: true, Attachable: true}
	if s.binding.OwnerEndpoint != "" {
		startOptions.Socket = strings.TrimPrefix(s.binding.OwnerEndpoint, "unix://")
		startOptions.RequiredSocket = true
	} else if retained != nil && retained.retainedSocket() != "" {
		startOptions.Socket = strings.TrimPrefix(retained.retainedSocket(), "unix://")
		startOptions.RequiredSocket = true // Never replace an unavailable original owner.
	}
	c, err := s.start(ctx, startOptions)
	if err != nil {
		return s.connectionError("无法连接本机 Codex，请检查连接设置后重试", err)
	}
	if retained != nil {
		c.adoptOwner(retained)
	}
	closeAttempt := func() {
		if retained != nil {
			c.detachForReconnect() // A failed handshake does not stop the original owner.
		} else {
			c.Close()
		}
	}
	s.mu.Lock()
	s.client = c
	if c.owner != nil {
		s.retainedOwner = c
	}
	s.bound = false
	c.rpc.sessionEpoch.Store(epoch)
	s.mu.Unlock()
	go s.listen(c, epoch)
	auth, err := c.ReadAuthStatus(ctx)
	if err != nil {
		closeAttempt()
		return s.connectionError("无法读取连接状态，请重新连接", err)
	}
	if err := s.rememberNativeOwner(c); err != nil {
		closeAttempt()
		return s.connectionError("无法保存原生连接入口，暂不发送消息", err)
	}
	if auth.RequiresOpenAIAuth && !auth.AccountPresent {
		s.mu.Lock()
		s.loading = false
		s.state.Connection = "login"
		s.state.Message = "登录 Codex 后即可开始"
		s.update()
		s.mu.Unlock()
		return nil
	}
	if s.workerOnly {
		s.mu.Lock()
		s.state.Phase, s.loading = "idle", false
		for _, event := range s.buffer {
			s.applyEvent(event)
		}
		s.buffer = nil
		s.bufferBytes = 0
		s.mu.Unlock()
		if !s.reconcileRecoveryWorkers(c, epoch) {
			c.detachForReconnect()
			return s.connectionError("后台工作订阅尚未确认；原任务已保留", ErrIO)
		}
		s.mu.Lock()
		if s.client != c || s.epoch != epoch || c.Err() != nil {
			s.mu.Unlock()
			return s.connectionError("恢复期间连接再次断开", ErrClosed)
		}
		s.state.Connection = "ready"
		s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "codex", Code: "recovery_ready", Generation: epoch, SessionEpoch: epoch, TransportGeneration: c.rpc.generation, Phase: "worker_reconciliation"})
		s.update()
		s.mu.Unlock()
		return nil
	}
	s.mu.Lock()
	threadID := s.binding.ThreadID
	unused := s.binding.Unsubmitted && s.binding.Pending == nil && len(s.binding.Tasks) == 0 && len(s.binding.Children) == 0
	s.mu.Unlock()
	params := s.connectionParams()
	method := "thread/start"
	paged := false
	if threadID != "" {
		method = "thread/resume"
		params["threadId"] = threadID
		// Probe the optional read interface, not the user's CLI release number.
		if _, pageErr := readTurnPage(ctx, c, threadID, ""); pageErr == nil {
			paged = true
			params["excludeTurns"] = true
		} else if !unsupportedHistory(pageErr) && !nativeThreadError(pageErr, "thread not loaded: ", threadID) {
			closeAttempt()
			return s.connectionError("暂时无法读取最近消息，请重新连接", pageErr)
		}
	}
	var response threadExecutionResponse
	err = callDecode(ctx, c, method, params, &response)
	// Codex may not persist a thread until its first turn. Only an explicit
	// never-submitted receipt permits replacing that missing, empty binding.
	if err != nil && unused && nativeThreadError(err, "no rollout found for thread id ", threadID) {
		method, threadID, paged = "thread/start", "", false
		params = s.connectionParams()
		err = callDecode(ctx, c, method, params, &response)
	}
	if err != nil {
		closeAttempt()
		return s.connectionError("无法恢复对话；草稿已保留，请重新连接", err)
	}
	if response.Thread.ID == "" || (threadID != "" && response.Thread.ID != threadID) {
		closeAttempt()
		return s.connectionError("后端返回了不匹配的对话", ErrProtocol)
	}
	var firstPage turnPage
	if paged {
		// Read after subscribing so concurrent live events are buffered and replayed.
		firstPage, err = readTurnPage(ctx, c, threadID, "")
		if err != nil {
			closeAttempt()
			return s.connectionError("暂时无法读取最近消息，请重新连接", err)
		}
		response.Thread.Turns = chronological(firstPage.Data)
	}
	s.mu.Lock()
	s.resetProjection()
	s.residentExecution = *response.execution()
	s.historyPaged, s.historyCursor = paged, firstPage.NextCursor
	s.state.HasEarlier = firstPage.NextCursor != "" || len(s.binding.PastThreads) > 0
	s.binding.ThreadID = response.Thread.ID
	if method == "thread/start" && s.opts.BotTools != nil {
		s.binding.RuntimeVersion = s.opts.BotTools.RuntimeVersion
	}
	s.historyThread = response.Thread.ID
	s.binding.Unsubmitted = (method == "thread/start" || unused) && len(response.Thread.Turns) == 0
	s.bound = true
	if err = s.save(); err != nil {
		s.mu.Unlock()
		closeAttempt()
		return s.connectionError("无法保存对话绑定，暂不发送消息", err)
	}
	for _, turn := range response.Thread.Turns {
		s.applyTurn(turn, true)
	}
	s.state.Connection = "connecting"
	s.state.Message = ""
	s.state.Phase = "idle"
	if turns := response.Thread.Turns; len(turns) > 0 && terminal(turns[len(turns)-1].Status) {
		s.state.Phase = turns[len(turns)-1].Status
	}
	if s.run != "" {
		s.state.Phase = "working"
	}
	s.loading = false
	for _, event := range s.buffer {
		s.applyEvent(event)
	}
	// Restoration/replay never establishes a warm-cache opportunity.
	s.usage, s.usageTurn, s.usageTotal = api.ContextUsage{}, "", 0
	s.buffer = nil
	s.bufferBytes = 0
	s.cleanupContextLocked()
	if s.binding.Pending != nil {
		s.state.Phase = "unknown"
		s.state.Message = "上次发送结果尚未确认。请重新连接核对，草稿已保留，不会自动重发。"
	}
	s.update()
	needsCleanup := len(s.binding.CleanupTargets) > 0
	s.mu.Unlock()
	if !s.reconcileRecoveryWorkers(c, epoch) {
		c.detachForReconnect()
		return s.connectionError("后台工作订阅尚未确认；原任务已保留", ErrIO)
	}
	if needsCleanup {
		// Exact cleanup targets fence new input. A definitely-unsent stop
		// remains explicitly retryable while its original turn is read.
		s.mu.Lock()
		if s.client != c || s.epoch != epoch || c.Err() != nil {
			s.mu.Unlock()
			return s.connectionError("恢复期间连接再次断开", ErrClosed)
		}
		s.state.Connection = "ready"
		s.update()
		s.mu.Unlock()
		if err := s.reconcileTerminalCleanup(ctx, c); err != nil {
			return err
		}
	}
	s.mu.Lock()
	if s.client != c || s.epoch != epoch || c.Err() != nil {
		s.mu.Unlock()
		return s.connectionError("恢复期间连接再次断开", ErrClosed)
	}
	s.state.Connection = "ready"
	s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "codex", Code: "recovery_ready", Generation: epoch, SessionEpoch: epoch, TransportGeneration: c.rpc.generation, Phase: "receipt_and_worker_reconciliation"})
	s.update()
	s.mu.Unlock()
	go s.observeNativeResources(c, epoch)
	go func() {
		ctx, cancel := s.operation(context.Background(), 8*time.Second)
		defer cancel()
		s.refreshReferences(ctx, c)
	}()
	return nil
}
func (s *Session) connectionError(message string, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loading = false
	s.state.Connection = "offline"
	s.usage, s.usageTurn = api.ContextUsage{}, ""
	s.state.ConnectionIssue = "connection"
	s.lastConnectCause = cause
	if errors.Is(cause, errRuntimeMissing) {
		s.state.ConnectionIssue = "runtime_missing"
		message = "未找到本机 Codex。安装后点击重新检测；已有登录和对话会继续保留。"
	} else if incompatibleProtocol(cause) {
		s.state.ConnectionIssue = "runtime_protocol"
		message = "本机 Codex 未能提供当前需要的连接接口。请更新 Codex 或选择兼容的安装后重新检测；原有对话和待确认发送会继续保留。"
	} else if resourceExhausted(cause) {
		s.state.ConnectionIssue = "resource_exhausted"
		message = "本机连接资源暂时不足；原任务和待确认结果已保留。请释放资源后重新连接核对。"
	} else if errors.Is(cause, errExistingServer) {
		s.state.ConnectionIssue = "existing_server"
		message = "发现了本机 Codex 连接入口，但暂时无法握手。请确认提供入口的应用仍在运行，再重新连接。"
	}
	s.state.Message = message
	if s.run != "" || s.binding.Pending != nil {
		s.state.Phase = "unknown"
	}
	phase := "awaiting_manual_reconnect"
	if s.reconnectActive {
		phase = "reconnect_retry"
	}
	s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: "recovery_failed", Reason: transportCode(cause), Generation: s.epoch, SessionEpoch: s.epoch, Phase: phase})
	s.update()
	return errors.New(message) // No native error payloads in Wails logs.
}
func callDecode(ctx context.Context, c *Client, method string, params, result any) error {
	b, err := c.rpc.call(ctx, method, params)
	if err != nil {
		return err
	}
	if result != nil {
		if err := json.Unmarshal(b, result); err != nil {
			c.rpc.diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: "response_decode_failed", Method: method, Reason: diagnosticlog.DecodeReason(err), Fingerprint: diagnosticlog.Fingerprint(b), Bytes: len(b)})
			return ErrProtocol
		}
	}
	return nil
}
func (s *Session) listen(c *Client, epoch uint64) {
	for event := range c.Notifications() {
		s.mu.Lock()
		started := time.Now()
		lag := time.Duration(0)
		if !event.ReceivedAt.IsZero() {
			lag = time.Since(event.ReceivedAt)
		}
		if epoch != s.epoch {
			s.mu.Unlock()
			continue
		}
		var finished func()
		if !s.loading && event.Method == "turn/completed" && s.opts.BotTools != nil {
			var target struct {
				ThreadID string `json:"threadId"`
			}
			if json.Unmarshal(event.Params, &target) == nil && target.ThreadID == s.binding.ThreadID {
				finished = s.opts.BotTools.FinishTurn
			}
		}
		if s.loading {
			size := len(event.Method) + len(event.Params) + len(event.RequestID)
			if len(s.buffer) >= maxQueuedEvents || s.bufferBytes+size > maxQueuedEventBytes {
				s.mu.Unlock()
				c.rpc.failWith(ErrEventOverflow, "projection_buffer", size)
				continue
			}
			s.buffer = append(s.buffer, event)
			s.bufferBytes += size
		} else {
			s.applyEvent(event)
			s.update()
		}
		s.mu.Unlock()
		processing := time.Since(started)
		if processing >= 100*time.Millisecond || lag >= 250*time.Millisecond {
			s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "codex", Code: "event_consumer_lag", Method: event.Method,
				Generation: epoch, SessionEpoch: epoch, TransportGeneration: c.rpc.generation, Phase: eventKindNames[eventKind(event)], LagMS: lag.Milliseconds(), ProcessingMS: processing.Milliseconds()})
		}
		if finished != nil {
			finished()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.epoch || s.closed {
		return
	}
	s.state.Connection = "offline"
	s.state.Message = "连接已断开；请重新连接核对结果。"
	s.usage, s.usageTurn = api.ContextUsage{}, ""
	s.state.ConnectionIssue = "connection"
	if resourceExhausted(c.Err()) {
		s.state.ConnectionIssue = "resource_exhausted"
		s.state.Message = "本机连接资源暂时不足；原任务和待确认结果已保留，请检查资源后重新连接。"
	}
	if s.run != "" || s.binding.Pending != nil {
		s.state.Phase = "unknown"
	}
	s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: "session_offline", Reason: transportCode(c.Err()), Generation: epoch, SessionEpoch: epoch, TransportGeneration: c.rpc.generation, Phase: "reconnect_pending"})
	for id, p := range s.prompts {
		p.view.Status = "unavailable"
		s.replacePrompt(id, p.view)
	}
	s.update()
	s.scheduleAutoReconnectLocked(c, epoch)
}
func (s *Session) Submit(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	return s.submit(ctx, in, files, false)
}
func (s *Session) submit(ctx context.Context, in api.Submission, files []api.InputFile, onlyIfIdle bool) (api.Receipt, error) {
	return s.submitWithSource(ctx, in, files, onlyIfIdle, false)
}
func (s *Session) submitWithSource(ctx context.Context, in api.Submission, files []api.InputFile, onlyIfIdle, report bool) (api.Receipt, error) {
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 45*time.Second)
	defer cancel()
	r := api.Receipt{ID: in.ID, Outcome: "rejected"}
	if !in.Scheduled && legacyWakeID.MatchString(in.ID) || len(in.ID) < 8 || len(in.ID) > 128 || len(in.Text) > 128*1024 || (strings.TrimSpace(in.Text) == "" && len(files) == 0) {
		r.Message = "请输入消息，或添加文件"
		return r, nil
	}
	s.mu.Lock()
	if in.NativeIngressFence != "" && (s.reconnectActive || in.NativeIngressFence != fmt.Sprintf("codex:%s:%d", s.instance, s.epoch)) {
		s.mu.Unlock()
		return api.Receipt{}, api.ErrRecoveryPending
	}
	if s.state.LastReceipt.ID == in.ID {
		r = s.state.LastReceipt
		s.mu.Unlock()
		return r, nil
	}
	if (!s.state.CanSend && !s.state.CanSteer) || ((onlyIfIdle || in.Scheduled) && !s.state.CanSend) {
		s.mu.Unlock()
		r.Message = "当前无法发送，请先处理待确认事项或恢复连接"
		return r, nil
	}
	c, threadID, run := s.client, s.binding.ThreadID, s.run
	refs := make([]nativeReference, 0, len(in.ReferenceIDs))
	for _, id := range in.ReferenceIDs {
		ref, ok := s.refs[id]
		if !ok {
			s.mu.Unlock()
			r.Message = "引用已不可用，请重新选择"
			return r, nil
		}
		refs = append(refs, ref)
	}
	s.mu.Unlock()
	if in.ScreenInput {
		capability, err := s.ImageInput(ctx)
		if err != nil || capability.State != "supported" {
			r.Message = "当前 Bot 模型的图片能力不可用，请切换到支持图片的模型"
			return r, nil
		}
	}
	if s.opts.BotTools != nil && s.opts.BotTools.PrepareTurn != nil {
		if err := s.opts.BotTools.PrepareTurn(ctx); err != nil {
			r.Message = "笔记目录暂不可用，消息未发送"
			return r, nil
		}
	}
	input, err := s.prepareInput(in, files, refs)
	if err != nil {
		r.Message = err.Error()
		return r, nil
	}
	s.mu.Lock()
	input, err = s.prepareContextLocked(ctx, in.ID, input)
	if err != nil {
		s.mu.Unlock()
		r.Message = "上下文暂不可用，消息未发送"
		return r, nil
	}
	s.binding.Unsubmitted = false
	s.binding.Pending = &pendingSubmission{ID: in.ID, TurnID: run}
	if report {
		if s.binding.HostReports == nil {
			s.binding.HostReports = map[string]bool{}
		}
		s.binding.HostReports[in.ID] = true
	}
	if in.Scheduled {
		s.binding.Scheduled[in.ID] = ""
	}
	if in.Dream {
		if s.binding.Dreams == nil {
			s.binding.Dreams = map[string]dreamRecord{}
		}
		s.binding.Dreams[in.ID] = dreamRecord{Thread: threadID}
	}
	if !report {
		s.binding.DelegationText = in.Text
	}
	if err = s.save(); err != nil {
		s.binding.Pending = nil
		if report {
			delete(s.binding.HostReports, in.ID)
		}
		s.binding.Context.Resolve(in.ID, "rejected")
		s.mu.Unlock()
		r.Message = "无法保存发送记录，消息未发送"
		return r, nil
	}
	s.state.Phase = "sending"
	s.state.Reviews = []api.Review{}
	s.reviewOrigins = map[string]reviewOrigin{}
	s.state.Message = ""
	s.update()
	s.mu.Unlock()
	params := map[string]any{"threadId": threadID, "clientUserMessageId": in.ID, "input": input}
	method := "turn/start"
	if run == "" {
		s.applyExecution(params, false)
	}
	if run != "" {
		method = "turn/steer"
		params["expectedTurnId"] = run
	}
	var response struct {
		Turn   nativeTurn `json:"turn"`
		TurnID string     `json:"turnId"`
	}
	err = callDecode(ctx, c, method, params, &response)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		if run != "" && response.TurnID != run {
			err = ErrProtocol
		} else if run == "" && response.Turn.ID == "" {
			err = ErrProtocol
		}
	}
	if err == nil {
		r.Outcome = "accepted"
		if run == "" {
			if in.Scheduled {
				s.binding.Scheduled[in.ID] = response.Turn.ID
			}
			s.lastTurn = response.Turn.ID
			s.applyTurn(response.Turn, false)
		}
		s.binding.Pending = nil
	} else {
		var request *RequestError
		if errors.As(err, &request) && !request.OutcomeUnknown {
			s.binding.Pending = nil
			r.Message = "消息未发送，请重试"
		} else {
			var native *NativeError
			if errors.As(err, &native) {
				s.binding.Pending = nil
				r.Message = "Codex 未接受这次发送，请检查连接后重试"
			} else {
				r.Outcome = "unknown"
				r.Message = "发送结果未知，草稿已保留；请重新连接核对，不要重复发送"
				s.state.Phase = "unknown"
			}
		}
	}
	// A correlated native user message may already have proved acceptance.
	if s.state.LastReceipt.ID == in.ID && s.state.LastReceipt.Outcome == "accepted" {
		r = s.state.LastReceipt
		s.binding.Pending = nil
	}
	s.binding.LastReceipt = &r
	s.binding.Context.Resolve(in.ID, r.Outcome)
	if err := s.save(); err != nil {
		s.state.Message = "对话记录未能保存，请勿重复发送；下次启动需核对历史"
	} else {
		s.state.Message = r.Message
		s.cleanupContextLocked()
	}
	s.state.LastReceipt = r
	if r.Outcome != "unknown" {
		if s.run != "" {
			if s.hasConversationPrompt() {
				s.state.Phase = "attention"
			} else {
				s.state.Phase = "working"
			}
		} else if s.state.Phase == "sending" {
			s.state.Phase = "idle"
		}
	}
	s.update()
	return r, nil
}
func (s *Session) Interrupt(ctx context.Context) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	retryUnsent := len(s.binding.CleanupTargets) > 0 && s.binding.StopState == "prepared"
	if len(s.binding.CleanupTargets) > 0 && !retryUnsent {
		s.mu.Unlock()
		return errors.New("上次停止与清理结果尚未确认，请重新连接核对")
	}
	targets := s.conversationTargets()
	if retryUnsent {
		targets = maps.Clone(s.binding.StopRuns)
	}
	if len(targets) == 0 && !retryUnsent {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	s.mu.Lock()
	current := s.client
	s.mu.Unlock()
	threadIDs := make([]string, 0, len(targets))
	for id := range targets {
		threadIDs = append(threadIDs, id)
	}
	slices.Sort(threadIDs)
	if !retryUnsent {
		s.mu.Lock()
		s.binding.CleanupTargets = append([]string(nil), threadIDs...)
		s.binding.StopState = "prepared"
		s.binding.StopRuns = maps.Clone(targets)
		if err := s.save(); err != nil {
			s.binding.CleanupTargets = nil
			s.binding.StopState = ""
			s.binding.StopRuns = nil
			s.mu.Unlock()
			return err
		}
		s.mu.Unlock()
	}
	if current != nil && !retryUnsent {
		preCtx, cancelPre := s.operation(ctx, 2*time.Second)
		s.cancelPendingElicitations(preCtx, current, targets)
		_ = s.cleanTerminals(preCtx, current, threadIDs...)
		cancelPre()
	}
	ctx, cancel := s.operation(ctx, 20*time.Second)
	defer cancel()
	if err := s.interruptWithStopRecord(ctx, false, true, targets); err != nil {
		s.mu.Lock()
		attempted := s.binding.StopState == "attempted"
		if !attempted {
			s.state.Phase = "working"
			s.state.Message = "停止请求尚未下发，请重试停止"
			s.update()
		}
		s.mu.Unlock()
		if !attempted {
			return errors.New("停止请求尚未下发，请重试停止")
		}
		s.markCleanupUnknown("停止结果尚未确认，后台工具清理也尚未确认")
		return err
	}
	for {
		s.mu.Lock()
		active, c, changed := s.run != "" || s.hasBlockingChildren(), s.client, s.changed
		independent := s.hasUnresolvedTasks()
		s.mu.Unlock()
		if !active {
			if c == nil {
				s.markCleanupUnknown("工作已停止，但后台工具清理尚未确认")
				return errors.New("连接已断开，后台工具清理尚未确认")
			}
			s.mu.Lock()
			unsent := s.binding.StopState == "prepared"
			s.mu.Unlock()
			if unsent {
				return s.reconcileTerminalCleanup(ctx, c)
			}
			if c.UsesSharedServer() || independent {
				// Shared server lifecycle belongs to its host. Native cleanup is
				// scoped to our bound threads; never recycle that process.
				cleanupErr := s.reconcileTerminalCleanup(ctx, c)
				s.mu.Lock()
				if cleanupErr != nil {
					s.state.Message = "工作已停止，但后台工具清理尚未确认"
					s.state.Phase = "unknown"
				}
				s.update()
				s.mu.Unlock()
				if cleanupErr != nil {
					return errors.New("后台工具清理尚未确认")
				}
				return nil
			}
			// Codex 0.153.4 can acknowledge interruption before a just-started
			// foreground tool is registered for backgroundTerminals/clean. The macOS
			// process owner captures descendants before interruption/EOF. Recycle
			// only this owned server after the native cleanup attempt, then restore
			// observation of the same binding. Never replay the interrupted turn.
			c.captureTools()
			_ = s.cleanTerminals(ctx, c, threadIDs...)
			s.mu.Lock()
			s.client = nil
			s.epoch++
			s.state.Connection = "connecting"
			s.state.Phase = "interrupting"
			s.update()
			s.mu.Unlock()
			c.Close()
			if c.toolCleanupError() != nil {
				return s.connectionError("任务已中断，但工具清理未能确认，请重新连接核对", c.toolCleanupError())
			}
			if err := s.connect(ctx); err != nil {
				return err
			}
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			s.markCleanupUnknown("停止结果尚未确认，后台工具清理也尚未确认")
			return errors.New("停止结果尚未确认，请重新连接核对")
		case <-c.Done():
			s.markCleanupUnknown("停止结果尚未确认，后台工具清理也尚未确认")
			return errors.New("连接已断开，停止结果尚未确认")
		}
	}
}

// conversationTargets is called with s.mu held. A task has its own stop and
// report lifecycle, even when it uses a child thread of the resident Bot.
func (s *Session) conversationTargets() map[string]string {
	targets := map[string]string{}
	if s.run != "" {
		targets[s.binding.ThreadID] = s.run
	}
	for id, run := range s.childRuns {
		if s.taskByThread(id) == nil {
			targets[id] = run
		}
	}
	return targets
}

func (s *Session) interrupt(ctx context.Context, all bool) error {
	return s.interruptWithStopRecord(ctx, all, false, nil)
}

func (s *Session) interruptWithStopRecord(ctx context.Context, all, recordStop bool, expected map[string]string) error {
	s.mu.Lock()
	c := s.client
	targets := s.conversationTargets()
	if all {
		for id, run := range s.childRuns {
			targets[id] = run
		}
	}
	s.mu.Unlock()
	if expected != nil {
		for id, run := range targets {
			original, owned := expected[id]
			if !owned {
				delete(targets, id)
			} else if original != "" && run != original {
				return errors.New("原工作状态已变化，停止请求尚未下发，请重新连接核对")
			}
		}
	}
	if len(targets) == 0 {
		return nil
	}
	if c == nil {
		return errors.New("连接已断开，停止请求尚未下发")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for id, run := range targets {
		if run == "" {
			var response struct {
				Thread nativeThread `json:"thread"`
			}
			if err := callDecode(ctx, c, "thread/read", map[string]any{"threadId": id, "includeTurns": true}, &response); err != nil || response.Thread.ID != id {
				return errors.New("后台工作尚未确认，无法确认停止")
			}
			for _, turn := range response.Thread.Turns {
				if turn.Status == "inProgress" {
					run = turn.ID
				}
			}
			if run == "" {
				if response.Thread.Status.Type == "active" {
					return errors.New("后台工作正在启动，请稍后重试停止")
				}
				s.mu.Lock()
				delete(s.childRuns, id)
				s.update()
				s.mu.Unlock()
				continue
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if recordStop {
			s.mu.Lock()
			previous := s.binding.StopState
			s.binding.StopState = "attempted"
			err := s.save()
			if err != nil {
				s.binding.StopState = previous
			}
			s.mu.Unlock()
			if err != nil {
				return err
			}
		}
		if err := callDecode(ctx, c, "turn/interrupt", map[string]string{"threadId": id, "turnId": run}, nil); err != nil {
			return errors.New("停止结果尚未确认，请重新连接核对")
		}
	}
	s.mu.Lock()
	if s.run != "" || (!all && s.hasBlockingChildren()) || (all && len(s.childRuns) > 0) {
		s.state.Phase = "interrupting"
		s.update()
	}
	s.mu.Unlock()
	return nil
}

// cleanTerminals only acts on explicit targets during a conversation Stop.
// Without targets it is used for whole-owner shutdown/replacement; old child
// threads may no longer be loaded and cannot block current-turn cleanup.
func (s *Session) cleanTerminals(ctx context.Context, c *Client, targets ...string) error {
	s.mu.Lock()
	if !s.bound {
		s.mu.Unlock()
		return nil
	}
	ids := targets
	if targets == nil {
		ids = []string{s.binding.ThreadID}
		for child := range s.children {
			ids = append(ids, child)
		}
	}
	current := s.binding.ThreadID
	s.mu.Unlock()
	seenThreads := map[string]bool{}
	for _, id := range ids {
		if id == "" || seenThreads[id] {
			continue
		}
		seenThreads[id] = true
		processes, err := listBackgroundTerminals(ctx, c, id)
		if err != nil {
			if targets == nil && id != current && nativeThreadError(err, "thread not loaded: ", id) {
				continue
			}
			return s.cleanupError("list", err)
		}
		for _, process := range processes {
			var result struct {
				Terminated bool `json:"terminated"`
			}
			// Natural exit is common; the final list, not this reply, decides.
			_ = callDecode(ctx, c, "thread/backgroundTerminals/terminate", map[string]string{"threadId": id, "processId": process}, &result)
		}
		// Clean may succeed after a terminate error or the process may exit
		// naturally. Re-list before deciding whether a warning is warranted.
		cleanErr := callDecode(ctx, c, "thread/backgroundTerminals/clean", map[string]string{"threadId": id}, nil)
		remaining, readErr := listBackgroundTerminals(ctx, c, id)
		if readErr != nil {
			return s.cleanupError("verify", readErr)
		}
		if len(remaining) > 0 {
			return s.cleanupError("remaining", errors.New("background terminals remain"))
		}
		if cleanErr != nil {
			// The post-clean list is authoritative for absence of residue.
			s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "codex", Code: "terminal_cleanup_reconciled", Reason: cleanupReason(cleanErr), Phase: "verify"})
		}
	}
	return nil
}

func listBackgroundTerminals(ctx context.Context, c *Client, thread string) ([]string, error) {
	var processes []string
	seenProcesses, seenCursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; page < 100; page++ {
		var response struct {
			Data []struct {
				ProcessID string `json:"processId"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if err := callDecode(ctx, c, "thread/backgroundTerminals/list", map[string]any{"threadId": thread, "limit": 100, "cursor": cursorOrNil(cursor)}, &response); err != nil {
			return nil, err
		}
		if response.Data == nil {
			return nil, ErrProtocol
		}
		for _, process := range response.Data {
			if process.ProcessID == "" {
				return nil, ErrProtocol
			}
			if !seenProcesses[process.ProcessID] {
				seenProcesses[process.ProcessID] = true
				processes = append(processes, process.ProcessID)
			}
		}
		if response.NextCursor == "" {
			return processes, nil
		}
		if seenCursors[response.NextCursor] {
			return nil, ErrProtocol
		}
		seenCursors[response.NextCursor] = true
		cursor = response.NextCursor
	}
	return nil, ErrProtocol
}

func cleanupReason(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if unsupportedHistory(err) {
		return "unsupported"
	}
	var native *NativeError
	if errors.As(err, &native) {
		return "native"
	}
	if errors.Is(err, ErrProtocol) {
		return "protocol"
	}
	return "transport"
}
func (s *Session) cleanupError(stage string, err error) error {
	s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "warn", Component: "codex", Code: "terminal_cleanup_unconfirmed", Reason: cleanupReason(err), Phase: stage})
	return errors.New("后台工具清理尚未确认")
}

func (s *Session) markCleanupUnknown(message string) {
	s.mu.Lock()
	s.state.Phase = "unknown"
	s.state.Message = message
	s.update()
	s.mu.Unlock()
}

// Recheck only the exact targets recorded before Stop. This read/clean path
// never resends a user message, review choice, or turn interruption.
func (s *Session) reconcileTerminalCleanup(ctx context.Context, c *Client) error {
	s.mu.Lock()
	targets := append([]string(nil), s.binding.CleanupTargets...)
	prepared := s.binding.StopState == "prepared"
	s.mu.Unlock()
	if len(targets) == 0 {
		return nil
	}
	if c == nil {
		if prepared {
			s.markCleanupUnknown("停止请求尚未下发，原工作状态尚未确认；请重新连接后重试停止")
		} else {
			s.markCleanupUnknown("工作已停止，但后台工具清理尚未确认")
		}
		return errors.New("后台工具清理尚未确认")
	}
	// A resumed thread may reveal a still running turn after local interruption
	// events were lost. Never clean terminals before this same-thread read.
	for _, id := range targets {
		var observed struct {
			Thread nativeThread `json:"thread"`
		}
		if err := callDecode(ctx, c, "thread/read", map[string]any{"threadId": id, "includeTurns": true}, &observed); err != nil || observed.Thread.ID != id {
			if err == nil {
				err = ErrProtocol
			}
			if prepared {
				s.markCleanupUnknown("停止请求尚未下发，原工作状态尚未确认；请重新连接后重试停止")
			} else {
				s.markCleanupUnknown("停止结果尚未确认，后台工具清理也尚未确认")
			}
			return s.cleanupError("read", err)
		}
		s.mu.Lock()
		for _, turn := range observed.Thread.Turns {
			if id == s.binding.ThreadID && turn.ID == s.run && terminal(turn.Status) {
				s.applyTurn(turn, true)
			} else if id != s.binding.ThreadID && terminal(turn.Status) {
				if current, known := s.childRuns[id]; known && (current == "" || current == turn.ID) {
					delete(s.childRuns, id)
				}
				s.childTerminals[opaque(id, turn.ID)] = true
			}
		}
		_, childActive := s.childRuns[id]
		active := observed.Thread.Status.Type == "active" || id == s.binding.ThreadID && s.run != "" || id != s.binding.ThreadID && childActive
		s.mu.Unlock()
		if active {
			if prepared {
				s.markCleanupUnknown("停止请求尚未下发，原工作仍在运行；可重试停止")
			} else {
				s.markCleanupUnknown("停止结果尚未确认，后台工具清理也尚未确认")
			}
			return errors.New("停止结果尚未确认，请重新连接核对")
		}
	}
	if err := s.cleanTerminals(ctx, c, targets...); err != nil {
		s.markCleanupUnknown("工作已停止，但后台工具清理尚未确认")
		return err
	}
	s.mu.Lock()
	previousStop, previousRuns := s.binding.StopState, s.binding.StopRuns
	s.binding.CleanupTargets = nil
	s.binding.StopState, s.binding.StopRuns = "", nil
	if err := s.save(); err != nil {
		s.binding.CleanupTargets = targets
		s.binding.StopState, s.binding.StopRuns = previousStop, previousRuns
		s.state.Phase = "unknown"
		s.state.Message = "后台工具已核对，但结果记录保存失败，请重试连接"
		s.update()
		s.mu.Unlock()
		return errors.New("后台工具清理记录未能保存")
	}
	if s.state.Connection == "ready" && s.run == "" && !s.hasBlockingChildren() {
		if terminal(s.runs[s.lastTurn]) {
			s.state.Phase = s.runs[s.lastTurn]
		} else if previousStop == "prepared" {
			s.state.Phase = "idle"
		} else {
			s.state.Phase = "interrupted"
		}
	}
	s.state.Message = ""
	s.update()
	s.mu.Unlock()
	return nil
}
func (s *Session) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	s.reconnectSeq++
	if s.reconnectCancel != nil {
		s.reconnectCancel()
	}
	s.cancelLife()
	s.update()
	s.mu.Unlock()
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	c := s.client
	if c == nil {
		c = s.retainedOwner
	}
	s.mu.Unlock()
	if c == nil {
		s.mu.Lock()
		s.closed = true
		s.update()
		s.mu.Unlock()
		return nil
	}
	c.captureTools()
	_ = s.interrupt(ctx, true)
	// Wait for the native terminal fact, not merely interrupt's acknowledgement.
	for {
		s.mu.Lock()
		active := s.run != "" || len(s.childRuns) > 0
		changed := s.changed
		s.mu.Unlock()
		if !active {
			break
		}
		select {
		case <-changed:
		case <-c.Done():
			goto cleanup
		case <-ctx.Done():
			goto cleanup
		}
	}
cleanup:
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cleanupErr := s.cleanTerminals(cleanupCtx, c)
	s.mu.Lock()
	var endpointErr error
	// Ordinary Close deliberately stops its private owner. An idle, fully
	// reconciled binding must not pin the next launch to that dead socket.
	unknownTaskReceipt := false
	for _, task := range s.binding.Tasks {
		if task != nil && taskHasUnknownReceipt(task) {
			unknownTaskReceipt = true
			break
		}
	}
	if cleanupErr == nil && s.run == "" && !s.hasBlockingChildren() && !s.hasUnresolvedTasks() && !unknownTaskReceipt && s.binding.Pending == nil && len(s.prompts) == 0 && (s.binding.LastReceipt == nil || s.binding.LastReceipt.Outcome != "unknown") {
		previous := s.binding.OwnerEndpoint
		s.binding.OwnerEndpoint = ""
		if err := s.save(); err != nil {
			s.binding.OwnerEndpoint = previous
			endpointErr = err
		}
	}
	s.closed = true
	s.state.Connection = "offline"
	s.update()
	s.mu.Unlock()
	c.Close()
	if endpointErr != nil {
		return fmt.Errorf("连接已关闭，但原生 owner 绑定未能清除: %w", endpointErr)
	}
	if cleanupErr != nil || c.toolCleanupError() != nil {
		return errors.New("连接已关闭，但后台工具的完整清理未能确认")
	}
	return nil
}

func (s *Session) operation(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, duration)
	stop := context.AfterFunc(s.life, cancel)
	return ctx, func() { stop(); cancel() }
}
