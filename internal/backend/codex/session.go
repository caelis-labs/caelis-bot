package codex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type binding struct {
	Tasks          map[string]*taskRecord `json:"tasks,omitempty"`
	DelegationText string                 `json:"delegationText,omitempty"`
	Children       []string               `json:"children,omitempty"`
	Version        int                    `json:"version"`
	ThreadID       string                 `json:"threadId"`
	Pending        *pendingSubmission     `json:"pending,omitempty"`
}
type pendingSubmission struct {
	ID     string `json:"id"`
	TurnID string `json:"turnId"`
}
type SessionOptions struct {
	Execution                            api.ExecutionSettings
	Binary, Socket, Directory, StateFile string
	WorkRoot                             string
	// RequireApproval tightens policy for isolated acceptance runs. The desktop
	// defaults to on-request; this flag can never weaken its sandbox.
	RequireApproval bool
	// Host-only MCP config; never supplied by the renderer.
	BotTools *api.ToolConnection
}

// Session projects one internally bound conversation. Native facts remain
// authoritative; a UI fetch, hidden window or character asset cannot execute it.
type Session struct {
	historyMu      sync.Mutex
	op             sync.Mutex
	mu             sync.Mutex
	opts           SessionOptions
	client         *Client
	bound          bool
	epoch          uint64
	state          api.Snapshot
	binding        binding
	loadErr        error
	loading        bool
	buffer         []Notification
	run            string
	runs           map[string]string
	items          map[string]int
	nativeItems    map[string]nativeItem
	prompts        map[string]*prompt
	promptHandles  map[string]string
	instance       string
	refs           map[string]nativeReference
	artifacts      map[string]string
	historyCursor  string
	historyPaged   bool
	children       map[string]bool
	childRuns      map[string]string
	childTerminals map[string]bool
	childWatching  map[string]bool
	childRevision  map[string]uint64
	loginID        string
	loginStarting  bool
	earlyLogin     map[string]bool
	changed        chan struct{}
	closed         bool
	closing        bool
	life           context.Context
	cancelLife     context.CancelFunc
	start          func(context.Context, Options) (*Client, error)
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
func (s *Session) resetProjection() {
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
	s.childWatching = map[string]bool{}
	s.childRevision = map[string]uint64{}
	s.state.Items = []api.Item{}
	s.state.Approvals = []api.Approval{}
	s.state.Reviews = []api.Review{}
	s.state.References = []api.Reference{}
	s.historyCursor = ""
	s.historyPaged = false
	s.state.HasEarlier = false
	s.run = ""
}
func opaque(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:16])
}
func (s *Session) update() {
	if len(s.prompts) > 0 && s.state.Connection == "ready" {
		s.state.Phase = "attention"
	} else if len(s.childRuns) > 0 && s.state.Phase != "unknown" && s.state.Phase != "interrupting" {
		s.state.Phase = "working"
	}
	s.state.CurrentTurn = ""
	if s.run != "" {
		s.state.CurrentTurn = opaque(s.run)
	}
	s.state.Revision++
	s.state.CanSend = s.state.Connection == "ready" && s.binding.Pending == nil && s.run == "" && !s.hasBlockingChildren() && len(s.prompts) == 0 && s.state.Phase != "unknown" && !s.closed && !s.closing
	s.state.CanSteer = s.state.Connection == "ready" && s.binding.Pending == nil && s.run != "" && s.state.Phase == "working" && len(s.prompts) == 0 && !s.closed && !s.closing
	s.state.CanInterrupt = s.state.Connection == "ready" && (s.run != "" || len(s.childRuns) > 0) && !s.closed && !s.closing
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
	return out
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
	return err
}
func (s *Session) Connect(ctx context.Context) error {
	s.op.Lock()
	defer s.op.Unlock()
	return s.connect(ctx)
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
		for child := range s.childRuns {
			if !s.childWatching[child] {
				s.childWatching[child] = true
				go s.watchChild(c, s.epoch, child)
			}
		}
		s.mu.Unlock()
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
				if len(s.prompts) > 0 {
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
	// A live owner in another state may still own background tools.
	if old != nil && old.Err() == nil && s.bound {
		s.mu.Unlock()
		if err := s.cleanTerminals(ctx, old); err != nil {
			return errors.New("原连接的后台工具尚未确认清理，请重试")
		}
		s.mu.Lock()
	}
	s.loginID = ""
	s.state.LoginPending = false
	s.client = nil
	s.epoch++
	epoch := s.epoch
	s.state.Connection = "connecting"
	s.state.ConnectionIssue = ""
	s.state.Message = ""
	s.loading = true
	s.buffer = nil
	s.update()
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if err := os.MkdirAll(s.opts.Directory, 0700); err != nil {
		return s.connectionError("无法准备工作文件夹", err)
	}
	c, err := s.start(ctx, Options{Binary: s.opts.Binary, Socket: s.opts.Socket, Directory: s.opts.Directory, Experimental: true, HandleRequests: true})
	if err != nil {
		return s.connectionError("无法连接本机 Codex，请检查连接设置后重试", err)
	}
	s.mu.Lock()
	s.client = c
	s.bound = false
	s.mu.Unlock()
	go s.listen(c, epoch)
	auth, err := c.ReadAuthStatus(ctx)
	if err != nil {
		c.Close()
		return s.connectionError("无法读取连接状态，请重新连接", err)
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
	s.mu.Lock()
	threadID := s.binding.ThreadID
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
		} else if !unsupportedHistory(pageErr) {
			c.Close()
			return s.connectionError("暂时无法读取最近消息，请重新连接", pageErr)
		}
	}
	var response struct {
		Thread nativeThread `json:"thread"`
	}
	if err = callDecode(ctx, c, method, params, &response); err != nil {
		c.Close()
		return s.connectionError("无法恢复对话；草稿已保留，请重新连接", err)
	}
	if response.Thread.ID == "" || (threadID != "" && response.Thread.ID != threadID) {
		c.Close()
		return s.connectionError("后端返回了不匹配的对话", ErrProtocol)
	}
	var firstPage turnPage
	if paged {
		// Read after subscribing so concurrent live events are buffered and replayed.
		firstPage, err = readTurnPage(ctx, c, threadID, "")
		if err != nil {
			c.Close()
			return s.connectionError("暂时无法读取最近消息，请重新连接", err)
		}
		response.Thread.Turns = chronological(firstPage.Data)
	}
	s.mu.Lock()
	s.resetProjection()
	s.historyPaged, s.historyCursor = paged, firstPage.NextCursor
	s.state.HasEarlier = firstPage.NextCursor != ""
	s.binding.ThreadID = response.Thread.ID
	s.bound = true
	if err = s.save(); err != nil {
		s.mu.Unlock()
		c.Close()
		return s.connectionError("无法保存对话绑定，暂不发送消息", err)
	}
	for _, turn := range response.Thread.Turns {
		s.applyTurn(turn, true)
	}
	s.state.Connection = "ready"
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
	s.buffer = nil
	if s.binding.Pending != nil {
		s.state.Phase = "unknown"
		s.state.Message = "上次发送结果尚未确认。请重新连接核对，草稿已保留，不会自动重发。"
	}
	s.update()
	for _, task := range s.binding.Tasks {
		if task.Thread != "" && !terminal(task.View.Status) && !s.childWatching[task.Thread] {
			s.childRuns[task.Thread] = task.Run
			s.childWatching[task.Thread] = true
			go s.watchChild(c, epoch, task.Thread)
		}
	}
	s.update()
	s.mu.Unlock()
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
	s.state.ConnectionIssue = "connection"
	if errors.Is(cause, errRuntimeMissing) {
		s.state.ConnectionIssue = "runtime_missing"
		message = "未找到本机 Codex。安装后点击重新检测；已有登录和对话会继续保留。"
	} else if incompatibleProtocol(cause) {
		s.state.ConnectionIssue = "runtime_protocol"
		message = "本机 Codex 未能提供当前需要的连接接口。请更新 Codex 或选择兼容的安装后重新检测；原有对话和待确认发送会继续保留。"
	} else if errors.Is(cause, errExistingServer) {
		s.state.ConnectionIssue = "existing_server"
		message = "发现了本机 Codex 连接入口，但暂时无法握手。请确认提供入口的应用仍在运行，再重新连接。"
	}
	s.state.Message = message
	if s.run != "" || s.binding.Pending != nil {
		s.state.Phase = "unknown"
	}
	s.update()
	return errors.New(message) // No native error payloads in Wails logs.
}
func callDecode(ctx context.Context, c *Client, method string, params, result any) error {
	b, err := c.rpc.call(ctx, method, params)
	if err != nil {
		return err
	}
	if result != nil && json.Unmarshal(b, result) != nil {
		return ErrProtocol
	}
	return nil
}
func (s *Session) listen(c *Client, epoch uint64) {
	for event := range c.Notifications() {
		s.mu.Lock()
		if epoch != s.epoch {
			s.mu.Unlock()
			continue
		}
		if s.loading {
			s.buffer = append(s.buffer, event)
		} else {
			s.applyEvent(event)
			s.update()
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.epoch || s.closed {
		return
	}
	s.state.Connection = "offline"
	s.state.Message = "连接已断开；请重新连接核对结果。"
	s.state.ConnectionIssue = "connection"
	if s.run != "" || s.binding.Pending != nil {
		s.state.Phase = "unknown"
	}
	for id, p := range s.prompts {
		p.view.Status = "unavailable"
		s.replacePrompt(id, p.view)
	}
	s.update()
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
	if len(in.ID) < 8 || len(in.ID) > 128 || len(in.Text) > 128*1024 || (strings.TrimSpace(in.Text) == "" && len(files) == 0) {
		r.Message = "请输入消息，或添加文件"
		return r, nil
	}
	s.mu.Lock()
	if s.state.LastReceipt.ID == in.ID {
		r = s.state.LastReceipt
		s.mu.Unlock()
		return r, nil
	}
	if (!s.state.CanSend && !s.state.CanSteer) || (onlyIfIdle && !s.state.CanSend) {
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
	input, err := s.prepareInput(in, files, refs)
	if err != nil {
		r.Message = err.Error()
		return r, nil
	}
	s.mu.Lock()
	s.binding.Pending = &pendingSubmission{ID: in.ID, TurnID: run}
	if !report {
		s.binding.DelegationText = in.Text
	}
	if err = s.save(); err != nil {
		s.binding.Pending = nil
		s.mu.Unlock()
		r.Message = "无法保存发送记录，消息未发送"
		return r, nil
	}
	s.state.Phase = "sending"
	s.state.Reviews = []api.Review{}
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
	if err := s.save(); err != nil {
		s.state.Message = "对话记录未能保存，请勿重复发送；下次启动需核对历史"
	} else {
		s.state.Message = r.Message
	}
	s.state.LastReceipt = r
	if r.Outcome != "unknown" {
		if s.run != "" {
			if len(s.prompts) > 0 {
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
	for _, task := range s.binding.Tasks {
		task.SuppressReport = true
		task.ReportState = "observed"
	}
	if err := s.save(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	ctx, cancel := s.operation(ctx, 20*time.Second)
	defer cancel()
	s.mu.Lock()
	current := s.client
	s.mu.Unlock()
	if current != nil {
		current.captureTools()
		_ = s.cleanTerminals(ctx, current)
	}
	if err := s.interrupt(ctx); err != nil {
		return err
	}
	for {
		s.mu.Lock()
		active, c, changed := s.run != "" || len(s.childRuns) > 0, s.client, s.changed
		s.mu.Unlock()
		if !active {
			if c == nil {
				return nil
			}
			if c.UsesSharedServer() {
				// Shared server lifecycle belongs to its host. Native cleanup is
				// scoped to our bound threads; never recycle that process.
				cleanupErr := s.cleanTerminals(ctx, c)
				s.mu.Lock()
				s.state.Phase = "interrupted"
				if cleanupErr != nil {
					s.state.Message = "工作已停止，但后台工具清理尚未确认"
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
			_ = s.cleanTerminals(ctx, c)
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
			s.mu.Lock()
			if s.run == "" && s.state.Connection == "ready" {
				s.state.Phase = "interrupted"
			}
			s.update()
			s.mu.Unlock()
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return errors.New("停止结果尚未确认，请重新连接核对")
		case <-c.Done():
			return errors.New("连接已断开，停止结果尚未确认")
		}
	}
}
func (s *Session) interrupt(ctx context.Context) error {
	s.mu.Lock()
	c := s.client
	targets := map[string]string{}
	if s.run != "" {
		targets[s.binding.ThreadID] = s.run
	}
	for id, run := range s.childRuns {
		targets[id] = run
	}
	s.mu.Unlock()
	if c == nil || len(targets) == 0 {
		return nil
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
		if err := callDecode(ctx, c, "turn/interrupt", map[string]string{"threadId": id, "turnId": run}, nil); err != nil {
			return errors.New("停止结果尚未确认，请重新连接核对")
		}
	}
	s.mu.Lock()
	if s.run != "" || len(s.childRuns) > 0 {
		s.state.Phase = "interrupting"
		s.update()
	}
	s.mu.Unlock()
	return nil
}
func (s *Session) cleanTerminals(ctx context.Context, c *Client) error {
	s.mu.Lock()
	if !s.bound {
		s.mu.Unlock()
		return nil
	}
	ids := []string{s.binding.ThreadID}
	for child := range s.children {
		ids = append(ids, child)
	}
	s.mu.Unlock()
	var last error
	for _, id := range ids {
		if id == "" {
			continue
		}
		var response struct {
			Data []struct {
				ProcessID string `json:"processId"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		var cursor any
		for {
			if err := callDecode(ctx, c, "thread/backgroundTerminals/list", map[string]any{"threadId": id, "limit": 100, "cursor": cursor}, &response); err != nil {
				last = err
				break
			}
			for _, process := range response.Data {
				var result struct {
					Terminated bool `json:"terminated"`
				}
				if err := callDecode(ctx, c, "thread/backgroundTerminals/terminate", map[string]string{"threadId": id, "processId": process.ProcessID}, &result); err != nil {
					last = err
				}
			}
			if response.NextCursor == "" {
				break
			}
			cursor = response.NextCursor
		}
		if err := callDecode(ctx, c, "thread/backgroundTerminals/clean", map[string]string{"threadId": id}, nil); err != nil {
			last = err
		}
	}

	return last
}
func (s *Session) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
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
	s.mu.Unlock()
	if c == nil {
		s.mu.Lock()
		s.closed = true
		s.update()
		s.mu.Unlock()
		return nil
	}
	c.captureTools()
	_ = s.interrupt(ctx)
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
	s.closed = true
	s.state.Connection = "offline"
	s.update()
	s.mu.Unlock()
	c.Close()
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
