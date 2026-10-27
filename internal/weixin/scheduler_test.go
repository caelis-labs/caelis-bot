package weixin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

type deliveryFixture struct {
	t        *testing.T
	mu       sync.Mutex
	now      time.Time
	snapshot api.Snapshot
	workers  []api.WorkState
	texts    []string
	contexts []string
	hook     func(string) string
	b        *Bridge
	p        *protocol
	root     string
}

func newDeliveryFixture(t *testing.T) *deliveryFixture {
	t.Helper()
	f := &deliveryFixture{t: t, now: time.Now(), root: t.TempDir(), snapshot: api.Snapshot{RuntimeOwner: "runtime", Connection: "ready", Phase: "working", CurrentTurn: "work"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Msg struct {
				Context string        `json:"context_token"`
				Items   []messageItem `json:"item_list"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		body := ""
		if len(request.Msg.Items) > 0 && request.Msg.Items[0].Text != nil {
			body = request.Msg.Items[0].Text.Text
		}
		f.mu.Lock()
		f.texts = append(f.texts, body)
		f.contexts = append(f.contexts, request.Msg.Context)
		hook := f.hook
		f.mu.Unlock()
		response := `{"ret":0}`
		if hook != nil {
			response = hook(body)
		}
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	f.p = &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	control, err := textchannel.Open(f.root, func(context.Context, api.Decision) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	host := Host{Now: func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }, Snapshot: func() api.Snapshot {
		f.mu.Lock()
		defer f.mu.Unlock()
		s := f.snapshot
		s.Items = append([]api.Item(nil), s.Items...)
		s.Approvals = append([]api.Approval(nil), s.Approvals...)
		s.ReportSources = append([]api.ReportSource(nil), s.ReportSources...)
		return s
	}, WorkStates: func() []api.WorkState {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]api.WorkState(nil), f.workers...)
	}, TextControl: control, Recovery: func() api.RecoveryState { return api.RecoveryState{} }}
	f.b, err = Open(f.root, host)
	if err != nil {
		t.Fatal(err)
	}
	f.b.state.OwnerID, f.b.state.BotID, f.b.state.Enabled = "owner", "bot", true
	f.b.state.BaseURL = defaultBase
	f.window("A")
	return f
}
func (f *deliveryFixture) window(id string) {
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	if f.b.remainingLocked(true) == 0 {
		f.b.state.Delivery.PausedWindow = f.b.state.Window.InputID
	}
	f.b.state.Window = sendWindow{InputID: id, OwnerID: "owner", ContextToken: "context-" + id, InputAt: f.b.now().UnixMilli()}
	f.b.state.ContextToken = "context-" + id
	if err := f.b.saveLocked(); err != nil {
		f.t.Fatal(err)
	}
}
func (f *deliveryFixture) advance(d time.Duration) { f.mu.Lock(); f.now = f.now.Add(d); f.mu.Unlock() }
func (f *deliveryFixture) add(id, body string, priority int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	turn := "turn-" + id
	f.snapshot.Items = append(f.snapshot.Items, api.Item{ID: id, Kind: "assistant", Status: "completed", Text: body, TurnKey: turn, SeenAt: f.now.UnixMicro() + int64(len(f.snapshot.Items)+1)})
	if priority == resultPriority {
		f.snapshot.ReportSources = append(f.snapshot.ReportSources, api.ReportSource{RuntimeOwner: "runtime", TurnKey: turn, ReportID: "report-" + id, Phase: "completed"})
	}
}
func (f *deliveryFixture) poll() { f.b.output(f.t.Context(), f.p) }
func (f *deliveryFixture) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.texts...)
}
func (f *deliveryFixture) used() int {
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	return f.b.state.Window.Used
}
func (f *deliveryFixture) reopen() {
	f.t.Helper()
	host := f.b.host
	b, err := Open(f.root, host)
	if err != nil {
		f.t.Fatal(err)
	}
	f.b = b
}
func (f *deliveryFixture) approval(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot.Approvals = append(f.snapshot.Approvals, api.Approval{ID: id, Owner: "task", TaskID: "worker", TurnKey: "original-native-turn", Status: "pending", Title: "需要审批 " + id, Choices: []api.Choice{{ID: "allow-once", Label: "允许一次"}, {ID: "deny", Label: "拒绝"}}})
}
func (f *deliveryFixture) question(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot.Items = append(f.snapshot.Items, api.Item{ID: id, Kind: "assistant", Status: "completed", TurnKey: "question-turn", SeenAt: f.now.UnixMicro() + 1, AsyncCallID: "call-" + id, AsyncQuestions: []api.AsyncQuestion{{Title: "选择部署位置", Options: []string{"原选项一", "原选项二"}}}})
}

func TestDeliverySharedConversationKeepsWindowAcrossInputsAndWorkerReports(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("start-A", "Worker A 已启动", resultPriority)
	f.poll()
	f.window("B")
	f.add("late-A", "Worker A 的新报告", resultPriority)
	f.poll()
	f.mu.Lock()
	f.snapshot.Items = append(f.snapshot.Items, api.Item{ID: "desktop", Kind: "user", RequestID: "desktop-input"}, api.Item{ID: "telegram", Kind: "user", RequestID: "telegram-input"})
	f.mu.Unlock()
	for i := 0; i < 9; i++ {
		f.advance(ordinaryDelay)
		f.add(fmt.Sprint("shared-", i), "共享输出", resultPriority)
		f.poll()
	}
	if f.used() != 10 || len(f.sent()) != 11 || f.sent()[1] != "Worker A 的新报告" {
		t.Fatalf("window/source gating: used=%d sent=%v", f.used(), f.sent())
	}
	f.add("over-budget", "漏发重要结果", resultPriority)
	f.poll()
	if len(f.sent()) != 11 {
		t.Fatal("overspent local window")
	}
	f.window("C")
	f.poll()
	if len(f.sent()) != 11 {
		t.Fatal("ordinary input caught up exhausted results")
	}
	f.add("new-C", "最新回复", resultPriority)
	f.poll()
	if f.used() != 1 || f.sent()[11] != "最新回复" {
		t.Fatal(f.sent())
	}
	f.window("resume")
	f.b.handleLocalCommand(t.Context(), f.p, inbound{ID: "resume", Text: "/r"})
	if f.used() != 1 || f.sent()[12] != "漏发重要结果" {
		t.Fatal(f.sent())
	}
}

func TestDeliveryFirstResponseAndDynamicBatching(t *testing.T) {
	f := newDeliveryFixture(t)
	f.poll()
	if len(f.sent()) != 0 {
		t.Fatal("invented acknowledgement")
	}
	f.add("first", "第一条有意义回复", progressPriority)
	f.poll()
	if len(f.sent()) != 1 {
		t.Fatal("first reply waited for timer")
	}
	f.add("short1", "短进展一", progressPriority)
	f.poll()
	f.advance(29 * time.Second)
	f.poll()
	if len(f.sent()) != 1 {
		t.Fatal("working output sent too early")
	}
	f.add("short2", "短进展二", progressPriority)
	f.add("short3", "短进展三", progressPriority)
	f.poll()
	if len(f.sent()) != 2 || !strings.Contains(f.sent()[1], "短进展三") {
		t.Fatal(f.sent())
	}
	f.add("length", strings.Repeat("长", textChunkUnits*7/10), progressPriority)
	f.poll()
	if len(f.sent()) != 3 {
		t.Fatal("length trigger missing")
	}
	f.add("timer", "按时间合并", progressPriority)
	f.advance(ordinaryDelay)
	f.poll()
	if len(f.sent()) != 4 {
		t.Fatal("time trigger missing")
	}
}

func TestDeliveryMultipleFinalsCoalesceAndIdleUsesWorkerAuthority(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("first", "开始", resultPriority)
	f.poll()
	for i := 1; i <= 3; i++ {
		f.add(fmt.Sprint(i), fmt.Sprint("阶段结果", i), resultPriority)
		f.poll()
	}
	if len(f.sent()) != 2 || !strings.Contains(f.sent()[1], "阶段结果1") || !strings.Contains(f.sent()[1], "阶段结果3") {
		t.Fatal(f.sent())
	}
	f.mu.Lock()
	f.snapshot.Phase = "idle"
	f.workers = []api.WorkState{{Runtime: "worker-runtime", Task: api.Task{ID: "worker", Status: "completed"}, Activity: "idle"}}
	f.mu.Unlock()
	f.add("waiting", "Worker 阶段已结束，仍等待后续", resultPriority)
	f.advance(2 * time.Second)
	f.poll()
	if len(f.sent()) != 2 {
		t.Fatal("native final falsely ended Worker")
	}
	f.advance(28 * time.Second)
	f.poll()
	if len(f.sent()) != 3 {
		t.Fatal("idle with Worker failed to flush")
	}
	f.mu.Lock()
	f.workers[0].TaskEnded = true
	f.mu.Unlock()
	f.add("finished", "Worker 整体完成", resultPriority)
	f.poll()
	if len(f.sent()) != 4 {
		t.Fatal("explicit task completion was delayed")
	}
	f.add("idle", "空闲阶段结果", resultPriority)
	f.poll()
	f.advance(2 * time.Second)
	f.poll()
	if len(f.sent()) != 5 {
		t.Fatal("idle batch did not flush")
	}
}

func TestDeliveryPrioritiesAndHandledInteractions(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("progress", "普通进展", progressPriority)
	f.add("result", "有价值结果", resultPriority)
	f.question("question")
	f.approval("approval")
	f.poll()
	sent := f.sent()
	if len(sent) != 2 || !strings.Contains(sent[0], "/approve A1") || !strings.Contains(sent[1], "/answer Q1") || !strings.Contains(sent[1], "原选项二") {
		t.Fatal(sent)
	}
	f.advance(ordinaryDelay)
	f.poll()
	if len(f.sent()) != 3 || f.sent()[2] != "有价值结果" {
		t.Fatal(f.sent())
	}
	f.advance(ordinaryDelay)
	f.poll()
	if len(f.sent()) != 4 || f.sent()[3] != "普通进展" {
		t.Fatal(f.sent())
	}
	// A Bot/native decision recorded in the original controller must win even
	// while an earlier pending snapshot remains visible.
	g := newDeliveryFixture(t)
	g.approval("handled")
	s := g.b.host.Snapshot()
	_, short, err := g.b.host.TextControl.Card(s.Approvals[0])
	if err != nil {
		t.Fatal(err)
	}
	g.b.host.TextControl.Handle(t.Context(), textchannel.Inbound{Channel: "desktop", Conversation: "local", ID: "decision-original", Text: "/approve " + short + " 1"}, s)
	g.poll()
	if len(g.sent()) != 0 || g.used() != 0 {
		t.Fatal("handled action stole priority/budget", g.sent())
	}
}

func TestDeliveryLowBudgetAndChunkPreemption(t *testing.T) {
	f := newDeliveryFixture(t)
	f.b.mu.Lock()
	f.b.state.Window.Used = 9
	f.b.mu.Unlock()
	f.add("final", "重要结果", resultPriority)
	f.question("question")
	f.approval("approval")
	f.poll()
	if len(f.sent()) != 1 || !strings.Contains(f.sent()[0], "/approve") || f.used() != 10 {
		t.Fatal(f.sent())
	}
	g := newDeliveryFixture(t)
	body := strings.Repeat("内容", 2000)
	g.add("long", body, resultPriority)
	var once sync.Once
	g.hook = func(string) string { once.Do(func() { g.approval("preempt") }); return `{"ret":0}` }
	g.poll()
	sent := g.sent()
	if len(sent) != 3 || !strings.Contains(sent[1], "/approve") || strings.Contains(sent[0], "/approve") || sent[0]+sent[2] != body {
		t.Fatalf("chunk ordering/preemption: %v", sent)
	}
}

func TestDeliveryRestartRefreshUnknownAndInflightAccounting(t *testing.T) {
	f := newDeliveryFixture(t)
	f.hook = func(string) string { return "unreadable" }
	f.add("uncertain", "原投递未知", resultPriority)
	f.poll()
	f.b.mu.Lock()
	original := f.b.state.Outputs["item:uncertain:0"]
	f.b.mu.Unlock()
	if original.State != "unknown" || original.ClientID == "" || f.used() != 1 {
		t.Fatal(original)
	}
	f.reopen()
	f.window("r")
	f.b.handleLocalCommand(t.Context(), f.p, inbound{ID: "r", Text: "/r"})
	for _, text := range f.sent()[1:] {
		if text == "原投递未知" {
			t.Fatal("unknown replayed")
		}
	}
	f.b.mu.Lock()
	after := f.b.state.Outputs["item:uncertain:0"]
	f.b.mu.Unlock()
	if after.ClientID != original.ClientID || after.WindowUsed != 1 {
		t.Fatal("original receipt replaced")
	}
	g := newDeliveryFixture(t)
	g.hook = func(string) string { g.window("B"); return `{"ret":1,"errmsg":"rate limit"}` }
	g.add("old", "在途旧回复", resultPriority)
	g.poll()
	if g.used() != 0 || g.b.state.Window.Exhausted {
		t.Fatal("old response debited/closed fresh window")
	}
	g.mu.Lock()
	g.hook = nil
	g.mu.Unlock()
	g.add("new", "新回复", resultPriority)
	g.poll()
	if g.used() != 1 || g.sent()[1] != "新回复" {
		t.Fatal(g.sent())
	}
}

func TestDeliveryOrdinaryRefreshDiscardsProgressButKeepsUnattemptedResults(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("first", "首条", progressPriority)
	f.poll()
	f.add("old-progress", "旧过程", progressPriority)
	f.add("result", "尚待投递的结果", resultPriority)
	f.poll()
	f.reopen()
	f.window("B")
	f.poll()
	if len(f.sent()) != 2 || f.sent()[1] != "尚待投递的结果" {
		t.Fatal(f.sent())
	}
	f.advance(time.Minute)
	f.poll()
	if len(f.sent()) != 2 {
		t.Fatal("old progress caught up", f.sent())
	}
	f.reopen()
	f.poll()
	if f.used() != 1 || len(f.sent()) != 2 {
		t.Fatal("restart reset/duplicated window")
	}
}

func TestDeliveryHistoryAndExpiredWindow(t *testing.T) {
	f := newDeliveryFixture(t)
	f.mu.Lock()
	f.snapshot.Items = []api.Item{{ID: "old", Kind: "assistant", Status: "completed", Text: "历史", SeenAt: f.now.Add(-time.Hour).UnixMicro()}, {ID: "native-history", Kind: "assistant", Status: "completed", Text: "无游标历史"}}
	f.mu.Unlock()
	f.poll()
	if len(f.sent()) != 0 {
		t.Fatal(f.sent())
	}
	f.add("new", "新输出", resultPriority)
	f.poll()
	f.advance(windowMaxAge + time.Second)
	f.add("expired", "安全截止后结果", resultPriority)
	f.poll()
	if len(f.sent()) != 1 || f.used() != 1 {
		t.Fatal(f.sent())
	}
	f.window("B")
	f.poll()
	if len(f.sent()) != 1 {
		t.Fatal("ordinary input replayed expired output")
	}
	f.window("recover")
	f.b.handleLocalCommand(t.Context(), f.p, inbound{ID: "recover", Text: "/r"})
	if len(f.sent()) != 2 || f.sent()[1] != "安全截止后结果" {
		t.Fatal(f.sent())
	}
}

func TestDeliveryHandledAsyncQuestionAndNoRoomForWholeCard(t *testing.T) {
	f := newDeliveryFixture(t)
	f.question("q")
	f.b.observeInteractions(f.b.host.Snapshot())
	f.b.host.TextControl.SetAsyncAnswerer(func(context.Context, textchannel.Inbound, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "accepted"}, nil
	})
	f.b.host.TextControl.Handle(t.Context(), textchannel.Inbound{Channel: "desktop", Conversation: "resident", ID: "original-answer", Text: "/answer Q1 2"}, f.b.host.Snapshot())
	f.poll()
	if len(f.sent()) != 0 || f.used() != 0 {
		t.Fatal("handled question emitted action", f.sent())
	}
	g := newDeliveryFixture(t)
	g.approval("big")
	g.mu.Lock()
	g.snapshot.Approvals[0].Title = strings.Repeat("完整原请求。", 600)
	g.mu.Unlock()
	g.b.mu.Lock()
	g.b.state.Window.Used = 9
	g.b.mu.Unlock()
	g.add("result", "重要结果仍然可达", resultPriority)
	g.poll()
	if len(g.sent()) != 1 || !strings.Contains(g.sent()[0], "重要结果仍然可达") || strings.Contains(g.sent()[0], "/approve") {
		t.Fatal(g.sent())
	}
	g.window("fresh")
	g.poll()
	if len(g.sent()) < 3 || !strings.Contains(strings.Join(g.sent()[1:], ""), "/approve A1") || !strings.Contains(strings.Join(g.sent()[1:], ""), "拒绝") {
		t.Fatal("full native action not preserved")
	}
}

func TestDeliveryPartialRestartAndNewWindowKeepOriginalPartReceipts(t *testing.T) {
	f := newDeliveryFixture(t)
	body := strings.Repeat("字", 3000)
	f.add("long", body, resultPriority)
	f.b.ObserveTurn(f.b.host.Snapshot())
	f.b.observeDelivery(f.b.host.Snapshot())
	f.b.mu.Lock()
	q := f.b.freezeDeliveryLocked(0)
	f.b.state.Outputs["item:long:0"] = outbound{State: "accepted", ClientID: "original", ContextToken: "context-A", Attempts: 1, WindowUsed: 1, Digest: textDigest(q.Parts[0])}
	f.b.state.Window.Used = 1
	f.b.state.Window.Replied = true
	_ = f.b.saveLocked()
	f.b.mu.Unlock()
	f.reopen()
	f.poll()
	if len(f.sent()) != 1 || f.sent()[0] != q.Parts[1] || f.used() != 2 {
		t.Fatal("accepted part replay/reset", f.sent())
	}
	f.b.mu.Lock()
	old := f.b.state.Outputs["item:long:0"]
	f.b.mu.Unlock()
	if old.ClientID != "original" || old.WindowUsed != 1 {
		t.Fatal(old)
	}
	g := newDeliveryFixture(t)
	g.add("long", body, resultPriority)
	var once sync.Once
	g.hook = func(string) string { once.Do(func() { g.window("B") }); return `{"ret":0}` }
	g.poll()
	if len(g.sent()) != 2 || strings.Join(g.sent(), "") != body || g.used() != 1 {
		t.Fatal("window switch lost/duplicated parts", g.sent())
	}
	g.mu.Lock()
	contexts := append([]string(nil), g.contexts...)
	g.mu.Unlock()
	if contexts[0] != "context-A" || contexts[1] != "context-B" {
		t.Fatal(contexts)
	}
}

func TestDeliveryLegacyBatchUnknownIsNeverExpandedIntoIndividualReplay(t *testing.T) {
	for _, state := range []string{"accepted", "unknown"} {
		t.Run(state, func(t *testing.T) {
			f := newDeliveryFixture(t)
			f.add("old1", "旧进展一", progressPriority)
			f.add("old2", "旧进展二", progressPriority)
			ids := []string{"old1", "old2"}
			key := "progress:" + textDigest(strings.Join(ids, "\x00"))
			f.b.state.Progress = []pendingProgress{{ID: "old1", Text: "旧进展一"}, {ID: "old2", Text: "旧进展二"}}
			f.b.state.ProgressBatch = ids
			f.b.state.Outputs[key] = outbound{State: state, ClientID: "original-batch", Attempts: 1, WindowUsed: 4}
			f.poll()
			f.window("resume")
			f.b.handleLocalCommand(t.Context(), f.p, inbound{ID: "resume", Text: "/r"})
			if strings.Contains(strings.Join(f.sent(), ""), "旧进展") {
				t.Fatal("legacy batch replayed", f.sent())
			}
			if f.b.state.Outputs[key].ClientID != "original-batch" || f.b.state.Outputs["item:old1"].State != state {
				t.Fatal("original uncertain batch lost")
			}
		})
	}
}

func TestDeliveryCapacityPreviewIsExplicitAndKeepsWholeMarkdownBlocks(t *testing.T) {
	f := newDeliveryFixture(t)
	f.b.state.Window.Used = 9
	body := "关键结果：构建成功。\n\n```json\n" + strings.Repeat("{\"data\":1}\n", 600) + "```\n\n完整的后续详情。"
	f.add("large", body, resultPriority)
	f.poll()
	if len(f.sent()) != 1 || !strings.Contains(f.sent()[0], "关键结果：构建成功") || !strings.Contains(f.sent()[0], "非全文") || !strings.Contains(f.sent()[0], "同一 Bot 对话") || strings.Contains(f.sent()[0], "```") || !fitsText(f.sent()[0]) {
		t.Fatal(f.sent())
	}
}

func TestDeliveryFailureSignalCoalescesWithResultAndSnapshotDoesNotMutate(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("first", "进行中", progressPriority)
	f.poll()
	f.add("failure", "可用结果与失败详情", resultPriority)
	f.mu.Lock()
	f.snapshot.Phase = "failed"
	f.mu.Unlock()
	f.poll()
	if len(f.sent()) != 2 || !strings.Contains(f.sent()[1], "可用结果与失败详情") || !strings.Contains(f.sent()[1], "未能完成") {
		t.Fatal(f.sent())
	}
	s := f.b.host.Snapshot()
	if len(s.Items) != 2 || s.Items[1].Text != "可用结果与失败详情" || s.Phase != "failed" {
		t.Fatal("delivery policy mutated shared snapshot")
	}
}

func TestDeliveryLegacyQuestionReceiptDoesNotBecomeANewCard(t *testing.T) {
	for _, state := range []string{"accepted", "unknown", "rejected"} {
		t.Run(state, func(t *testing.T) {
			f := newDeliveryFixture(t)
			f.question("old-question")
			f.b.state.Outputs["item:old-question:0"] = outbound{State: state, Attempts: 1, ClientID: "original-question"}
			f.poll()
			f.reopen()
			f.window("resume")
			f.b.handleLocalCommand(t.Context(), f.p, inbound{ID: "resume", Text: "/r"})
			if strings.Contains(strings.Join(f.sent(), ""), "/answer") || f.b.state.Outputs["item:old-question:0"].ClientID != "original-question" {
				t.Fatal("legacy action replayed/lost original intent", f.sent())
			}
		})
	}
}

func TestDeliveryCapacityCannotBlockApprovalAndKeepsFrozenMembership(t *testing.T) {
	f := newDeliveryFixture(t)
	f.b.mu.Lock()
	for i := 0; i < 128; i++ {
		f.b.enqueueDeliveryLocked(queuedDelivery{Key: fmt.Sprint("old-", i), Body: "旧结果", Priority: resultPriority, Held: true})
	}
	f.b.mu.Unlock()
	f.approval("urgent")
	f.add("fresh", "当前结果", resultPriority)
	f.poll()
	if len(f.sent()) != 1 || !strings.Contains(f.sent()[0], "/approve") {
		t.Fatal("backlog blocked P0", f.sent())
	}
	f.advance(time.Minute)
	f.poll()
	if len(f.sent()) != 2 || !strings.Contains(f.sent()[1], "当前结果") || !strings.Contains(f.sent()[1], "完整记录") {
		t.Fatal(f.sent())
	}
	g := newDeliveryFixture(t)
	g.b.mu.Lock()
	g.b.enqueueDeliveryLocked(queuedDelivery{Key: "one", Body: "第一段", Priority: resultPriority})
	g.b.enqueueDeliveryLocked(queuedDelivery{Key: "two", Body: "第二段", Priority: resultPriority})
	batch := g.b.freezeDeliveryLocked(0)
	g.b.enqueueDeliveryLocked(queuedDelivery{Key: "two", Body: "第二段新状态", Priority: resultPriority})
	queue := append([]queuedDelivery(nil), g.b.state.Delivery.Queue...)
	g.b.mu.Unlock()
	if len(queue) != 1 || len(batch.Members) != 2 || queue[0].Body != batch.Body {
		t.Fatal("frozen batch member was enqueued twice", queue)
	}
}

func TestDeliveryPausedUnobservedResultNeedsExplicitRecovery(t *testing.T) {
	f := newDeliveryFixture(t)
	f.poll() // establish the existing shared IM cursor
	f.b.state.Window.Used = 10
	f.add("missed-between-polls", "耗尽后完成的结果", resultPriority)
	f.advance(time.Second)
	f.window("ordinary")
	f.add("fresh", "新输入后的结果", resultPriority)
	f.poll()
	if len(f.sent()) != 1 || f.sent()[0] != "新输入后的结果" {
		t.Fatal(f.sent())
	}
	f.window("recover")
	f.b.handleLocalCommand(t.Context(), f.p, inbound{ID: "recover", Text: "/r"})
	if len(f.sent()) != 2 || f.sent()[1] != "耗尽后完成的结果" {
		t.Fatal(f.sent())
	}
}

func TestDeliveryRefreshRecoversResultFirstObservedAfterRefresh(t *testing.T) {
	f := newDeliveryFixture(t)
	f.poll()
	f.b.state.Window.Used = 10
	f.add("between-polls", "刷新前尚未轮询到的完成结果", resultPriority)
	f.advance(time.Second)
	f.window("recover")
	f.b.handleLocalCommand(t.Context(), f.p, inbound{ID: "recover", Text: "/r"})
	if len(f.sent()) != 1 || f.sent()[0] != "刷新前尚未轮询到的完成结果" {
		t.Fatal(f.sent())
	}
	f.reopen()
	f.poll()
	if len(f.sent()) != 1 || f.used() != 1 {
		t.Fatal("refresh replayed after restart", f.sent())
	}
}

func TestDeliveryNewApprovalAtSendAdmissionPreemptsResult(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("result", "当前结果", resultPriority)
	original := f.b.host.Snapshot
	calls := 0
	f.b.host.Snapshot = func() api.Snapshot {
		calls++
		if calls == 3 {
			f.approval("appeared-before-reservation")
		}
		return original()
	}
	f.poll()
	if len(f.sent()) != 2 || !strings.Contains(f.sent()[0], "/approve") || f.sent()[1] != "当前结果" || f.used() != 2 {
		t.Fatal("result stole P0 admission", f.sent())
	}
	f.advance(ordinaryDelay)
	f.poll()
	if len(f.sent()) != 2 || f.sent()[1] != "当前结果" {
		t.Fatal(f.sent())
	}
}

func TestDeliveryCardLargerThanWholeWindowHasNonActionableDesktopEntry(t *testing.T) {
	f := newDeliveryFixture(t)
	f.approval("large")
	f.snapshot.Approvals[0].Title = strings.Repeat("完整原文。", 6000)
	f.poll()
	if len(f.sent()) != 1 || !strings.Contains(f.sent()[0], "桌面同一 Bot 对话") || strings.Contains(f.sent()[0], "/approve") || f.used() != 1 {
		t.Fatal(f.sent())
	}
	f.reopen()
	f.window("new")
	f.poll()
	if len(f.sent()) != 1 {
		t.Fatal("oversized card notice repeated", f.sent())
	}
}

func TestDeliveryHandledOversizedInteractionDropsItsDesktopNotice(t *testing.T) {
	f := newDeliveryFixture(t)
	f.approval("large")
	f.snapshot.Approvals[0].Title = strings.Repeat("完整原文。", 6000)
	original := f.b.host.Snapshot
	calls := 0
	f.b.host.Snapshot = func() api.Snapshot {
		calls++
		if calls == 4 {
			f.mu.Lock()
			f.snapshot.Approvals = nil
			f.mu.Unlock()
		}
		return original()
	}
	f.poll()
	if len(f.sent()) != 0 || f.used() != 0 {
		t.Fatal("handled action emitted stale desktop notice", f.sent())
	}
}

func TestDeliveryIdlePollSkipsRewriteButSendPersistsItsSlot(t *testing.T) {
	f := newDeliveryFixture(t)
	f.poll()
	before, err := os.Stat(f.b.path)
	if err != nil {
		t.Fatal(err)
	}
	f.poll()
	after, err := os.Stat(f.b.path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("idle poll rewrote the delivery ledger")
	}
	f.add("new", "新结果", resultPriority)
	f.poll()
	f.reopen()
	if f.used() != 1 || f.b.state.Outputs["item:new:0"].Attempts != 1 {
		t.Fatal("send skipped durable reservation")
	}
}

func TestDeliveryLegacyUnattemptedFinalRecoversWithoutReactivatingOldFiles(t *testing.T) {
	for _, mode := range []string{"text", "files-only"} {
		t.Run(mode, func(t *testing.T) {
			f := newDeliveryFixture(t)
			f.add("legacy-result", "原任务已经完成的结果", resultPriority)
			if mode == "files-only" {
				f.snapshot.Items[0].Text = ""
			}
			f.snapshot.Items[0].SeenAt = f.now.Add(-time.Minute).UnixMicro()
			f.snapshot.Items[0].Artifacts = []api.Artifact{{ID: "old-file", Name: "full.txt"}}
			f.b.state.Progress = []pendingProgress{{ID: "legacy-result", TurnKey: "turn-legacy-result", Text: "原任务已经完成的结果"}}
			f.poll()
			if len(f.sent()) != 0 || f.used() != 0 {
				t.Fatal("ordinary window resumed legacy backlog", f.sent())
			}
			f.reopen()
			f.window("recover")
			f.b.handleLocalCommand(t.Context(), f.p, inbound{ID: "recover", Text: "/r"})
			if len(f.sent()) != 1 || !strings.Contains(f.sent()[0], "full.txt") || !strings.Contains(f.sent()[0], "完整文件可在桌面") || f.used() != 1 {
				t.Fatal("legacy final lost", f.sent())
			}
			if mode != "files-only" && !strings.Contains(f.sent()[0], "原任务已经完成的结果") {
				t.Fatal("missing result", f.sent())
			}
			if out := f.b.state.Outputs[artifactKey("turn-legacy-result", "old-file")]; out.State != "deferred" || out.Attempts != 0 {
				t.Fatal("text recovery reactivated old artifact", out)
			}
			f.reopen()
			if mode != "files-only" {
				f.snapshot.Items[0].Text += "，原生展示补充详情"
			}
			f.poll()
			if len(f.sent()) != 1 {
				t.Fatal("accepted legacy recovery replayed", f.sent())
			}
		})
	}
}

func TestDeliveryLegacyPendingFinalWaitsForSharedIMBeforeRecovery(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("legacy-final", "已经完成的重要结果", resultPriority)
	f.snapshot.Items[0].SeenAt = f.now.Add(-time.Minute).UnixMicro()
	items := f.snapshot.Items
	f.snapshot.Items = nil
	f.b.state.PendingFinal = "legacy-final"
	f.poll()
	f.reopen()
	if f.b.state.PendingFinal != "legacy-final" || len(f.sent()) != 0 {
		t.Fatal("missing shared item discarded original final identity")
	}
	f.snapshot.Items = items
	f.poll()
	if len(f.sent()) != 0 {
		t.Fatal("ordinary observation replayed old final")
	}
	f.window("recover")
	f.b.handleLocalCommand(t.Context(), f.p, inbound{ID: "recover", Text: "/r"})
	if len(f.sent()) != 1 || !strings.Contains(f.sent()[0], "已经完成的重要结果") {
		t.Fatal("known final did not recover", f.sent())
	}
	f.reopen()
	f.poll()
	if len(f.sent()) != 1 || f.b.state.PendingFinal != "" {
		t.Fatal("recovered final repeated")
	}
}
