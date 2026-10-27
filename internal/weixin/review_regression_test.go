package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

func recoverDelivery(f *deliveryFixture, id string) {
	f.t.Helper()
	f.window(id)
	f.b.handleLocalCommand(f.t.Context(), f.p, inbound{ID: id, Text: "/r"})
}

func TestReviewPreviewKeepsOriginalBodyForRecovery(t *testing.T) {
	for _, response := range []string{`{"ret":0}`, `not-json`} {
		t.Run(response, func(t *testing.T) {
			f := newDeliveryFixture(t)
			body := strings.Repeat("a", 1500) + "\n\n" + strings.Repeat("b", 1500) + "\n\n" + strings.Repeat("c", 1500)
			if len(chunks(body)) != 3 {
				t.Fatal("fixture must have three original parts")
			}
			f.b.state.Window.Used = 9
			f.hook = func(string) string { return response }
			f.add("long", body, resultPriority)
			f.poll()
			if len(f.sent()) != 1 || !strings.Contains(f.sent()[0], "非全文") {
				t.Fatal("missing explicit preview")
			}
			if f.b.state.Outputs["item:long"].State == "accepted" || f.b.state.Outputs["item:long:0"].State != "" {
				t.Fatal("preview was recorded as original body delivery", f.b.state.Outputs)
			}
			preview := f.b.state.Outputs["item:long:preview"]
			if preview.Attempts != 1 {
				t.Fatal("missing separate preview intent", preview)
			}
			f.reopen()
			f.hook = nil
			recoverDelivery(f, "recover")
			if len(f.sent()) != 4 || f.used() != 3 {
				t.Fatal("unattempted original body lost", len(f.sent()), f.used())
			}
			for i, part := range chunks(body) {
				if f.sent()[i+1] != part {
					t.Fatal("body part identity changed", i)
				}
			}
			if !reflect.DeepEqual(preview, f.b.state.Outputs["item:long:preview"]) {
				t.Fatal("preview receipt overwritten")
			}
			f.reopen()
			recoverDelivery(f, "again")
			if strings.Count(strings.Join(f.sent(), ""), strings.Repeat("c", 1500)) != 1 {
				t.Fatal("body replayed")
			}
		})
	}
}

func TestReviewRetiredProgressCanBecomeResult(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("first", "already replied", resultPriority)
	f.poll()
	f.add("late", "unchanged completed assistant", progressPriority)
	f.poll()
	f.window("B")
	f.poll()
	if f.b.state.Outputs["item:late"].State != "superseded" {
		t.Fatal("fixture did not retire P3")
	}
	f.reopen()
	f.snapshot.ReportSources = append(f.snapshot.ReportSources, api.ReportSource{RuntimeOwner: "runtime", TurnKey: "turn-late", ReportID: "late-report", Phase: "completed"})
	f.poll()
	if len(f.sent()) != 2 || f.sent()[1] != "unchanged completed assistant" {
		t.Fatal("native classification upgrade lost result", f.sent())
	}
}

// Receive through getupdates: the latest window changes before dispatch, exactly
// as it does for two controls in one poll. No notice body is injected here.
var reviewBatchSerial atomic.Uint64

func receiveReviewControls(f *deliveryFixture, ids, bodies []string) {
	f.t.Helper()
	msgs := make([]map[string]any, 0, len(ids))
	for i, id := range ids {
		msgs = append(msgs, map[string]any{"message_id": id, "from_user_id": "owner", "to_user_id": "bot", "message_type": 1, "context_token": "ctx-" + id, "item_list": []any{map[string]any{"type": 1, "text_item": map[string]string{"text": bodies[i]}}}})
	}
	cursor := fmt.Sprintf("cursor-%s-%d", ids[len(ids)-1], reviewBatchSerial.Add(1))
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0, "get_updates_buf": cursor, "msgs": msgs})
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Millisecond):
			_, _ = w.Write([]byte(`{"ret":0}`))
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(f.t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.b.receive(ctx, &protocol{client: srv.Client(), base: srv.URL, token: "fixture"})
	}()
	until := time.After(3 * time.Second)
	for {
		f.b.mu.Lock()
		ready := f.b.state.Cursor == cursor
		f.b.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-until:
			f.t.Fatal("receive timed out")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
}

func TestReviewEarlierControlFeedbackSurvivesLaterWindow(t *testing.T) {
	for _, command := range []string{"/approve A1 1", "/answer Q1 1"} {
		t.Run(command, func(t *testing.T) {
			f := newDeliveryFixture(t)
			observeReviewControlIM(f, f.b.host.TextControl)
			a := api.Approval{ID: "native", Owner: "conversation", TurnKey: "native-turn", Status: "pending", Choices: []api.Choice{{ID: "allow", Label: "Allow"}}}
			if strings.HasPrefix(command, "/answer") {
				a.Choices = []api.Choice{{ID: "answer", Label: "Answer"}}
				a.Questions = []api.Question{{ID: "q", Title: "Choose", Type: "select", Required: true, Options: []api.Choice{{ID: "one", Label: "One"}}}}
			}
			if _, _, err := f.b.host.TextControl.Card(a); err != nil {
				t.Fatal(err)
			}
			a.Status = "resolved"
			a.Resolution = &api.ApprovalResolution{Outcome: "allowed"}
			f.snapshot.Approvals = []api.Approval{a}
			receiveReviewControls(f, []string{"control", "status"}, []string{command, "/status"})
			f.b.dispatch(t.Context(), f.p)
			notices := f.b.host.TextControl.Notices()
			if len(notices) != 1 {
				t.Fatal("controller did not produce a visible outcome", notices)
			}
			f.b.dispatch(t.Context(), f.p)
			f.advance(ordinaryDelay)
			f.poll()
			if !strings.Contains(strings.Join(f.sent(), ""), notices[0].Text) {
				t.Fatal("earlier control outcome disappeared", f.sent(), notices[0].Text)
			}
			receipt := f.b.state.Outputs["notice:"+notices[0].ID+":0"]
			if receipt.Attempts != 1 {
				t.Fatal("missing independent notice intent", receipt)
			}
			f.reopen()
			f.window("next")
			f.poll()
			if strings.Count(strings.Join(f.sent(), ""), notices[0].Text) != 1 {
				t.Fatal("notice replayed")
			}
		})
	}
}

func TestReviewRefreshRecoversBoundedUnsentProgress(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("first", "already delivered", resultPriority)
	f.poll()
	for i := 0; i < 2; i++ {
		f.add(fmt.Sprint("progress-", i), fmt.Sprint("missed update ", i), progressPriority)
	}
	f.poll()
	f.window("B")
	f.poll()
	f.reopen()
	recoverDelivery(f, "recover")
	sent := strings.Join(f.sent(), "\n")
	if !strings.Contains(sent, "missed update 0") || !strings.Contains(sent, "missed update 1") {
		t.Fatal("/r lost retired progress", f.sent())
	}
	if len(f.sent()) != 2 {
		t.Fatal("short progress was not combined", f.sent())
	}
	f.reopen()
	recoverDelivery(f, "again")
	if strings.Count(strings.Join(f.sent(), ""), "missed update 0") != 1 {
		t.Fatal("recovered progress repeated")
	}
}

func TestReviewMergedPreviewDoesNotAcceptMembers(t *testing.T) {
	f := newDeliveryFixture(t)
	f.b.state.Window.Used = 9
	f.b.state.Delivery.CapacityDropped = true
	f.add("one", strings.Repeat("a", 1180), resultPriority)
	f.add("two", strings.Repeat("b", 1180), resultPriority)
	f.poll()
	if len(f.sent()) != 1 || len(f.b.state.Delivery.Queue) != 1 {
		t.Fatal("merged preview fixture", len(f.sent()), f.b.state.Delivery.Queue)
	}
	q := f.b.state.Delivery.Queue[0]
	if len(q.Members) != 2 || len(q.Parts) != 2 {
		t.Fatal("fixture did not combine into two original parts", q.Members, len(q.Parts))
	}
	for _, key := range q.Members {
		if f.b.state.Outputs[key].State == "accepted" {
			t.Fatal("excerpt accepted a whole member", key)
		}
	}
	preview := f.b.state.Outputs[q.Key+":preview"]
	f.reopen()
	recoverDelivery(f, "recover")
	if len(f.sent()) != 3 {
		t.Fatal("combined body not resumed", len(f.sent()))
	}
	for i, part := range q.Parts {
		if f.sent()[i+1] != part {
			t.Fatal("combined part changed", i)
		}
	}
	for _, key := range q.Members {
		if f.b.state.Outputs[key].State != "accepted" {
			t.Fatal("full member not completed", key)
		}
	}
	if !reflect.DeepEqual(preview, f.b.state.Outputs[q.Key+":preview"]) {
		t.Fatal("original preview overwritten")
	}
}

func TestReviewRecoveryPreservesOriginalBodyAttemptReceipts(t *testing.T) {
	for _, outcome := range []string{"accepted", "unknown", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			f := newDeliveryFixture(t)
			body := strings.Repeat("a", 1500) + "\n\n" + strings.Repeat("b", 1500) + "\n\n" + strings.Repeat("c", 1500)
			f.b.state.Window.Used = 9
			f.add("long", body, resultPriority)
			f.poll()
			// Model a persisted first body intent whose HTTP result arrived before a crash.
			original := outbound{State: outcome, ClientID: "original-body-client", ContextToken: "original-context", Attempts: 1, Digest: textDigest(chunks(body)[0])}
			f.b.state.Outputs["item:long:0"] = original
			if err := f.b.saveLocked(); err != nil {
				t.Fatal(err)
			}
			f.reopen()
			recoverDelivery(f, "recover")
			if !reflect.DeepEqual(original, f.b.state.Outputs["item:long:0"]) {
				t.Fatal("body intent overwritten")
			}
			count := strings.Count(strings.Join(f.sent()[1:], ""), strings.Repeat("a", 1500))
			if count != 0 {
				t.Fatal("attempted original body replayed")
			}
			if outcome == "accepted" && len(f.sent()) != 3 {
				t.Fatal("unattempted continuation lost", len(f.sent()))
			}
			if outcome != "accepted" && strings.Contains(strings.Join(f.sent()[1:], ""), strings.Repeat("b", 1500)) {
				t.Fatal("unknown/rejected original body treated as unsent")
			}
		})
	}
}

func TestReviewClassificationNeverReplaysAttemptedOutput(t *testing.T) {
	for _, response := range []string{`{"ret":0}`, `not-json`, `{"ret":1,"errmsg":"rejected"}`} {
		t.Run(response, func(t *testing.T) {
			f := newDeliveryFixture(t)
			f.hook = func(string) string { return response }
			f.add("late", "already attempted", progressPriority)
			f.poll()
			original := f.b.state.Outputs["item:late:0"]
			f.window("B")
			f.reopen()
			f.snapshot.ReportSources = append(f.snapshot.ReportSources, api.ReportSource{RuntimeOwner: "runtime", TurnKey: "turn-late", ReportID: "late", Phase: "completed"})
			f.hook = nil
			f.poll()
			recoverDelivery(f, "recover")
			if strings.Count(strings.Join(f.sent(), ""), "already attempted") != 1 || !reflect.DeepEqual(original, f.b.state.Outputs["item:late:0"]) {
				t.Fatal("classification replayed original intent", f.sent())
			}
		})
	}
}

func TestReviewAsyncControlOutcomeUsesNewWindowAndOriginalIdentity(t *testing.T) {
	f := newDeliveryFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var decisions atomic.Int32
	decide := func(context.Context, api.Decision) error {
		decisions.Add(1)
		close(entered)
		<-release
		return errors.New("fixture uncertain result")
	}
	control, err := textchannel.Open(f.root, decide)
	if err != nil {
		t.Fatal(err)
	}
	f.b.host.TextControl = control
	observeReviewControlIM(f, control)
	a := api.Approval{ID: "native", Owner: "conversation", TurnKey: "turn", Status: "pending", Choices: []api.Choice{{ID: "allow", Label: "Allow"}}}
	if _, _, err := control.Card(a); err != nil {
		t.Fatal(err)
	}
	f.snapshot.Approvals = []api.Approval{a}
	receiveReviewControls(f, []string{"approve"}, []string{"/approve A1 1"})
	done := make(chan struct{})
	go func() { defer close(done); f.b.dispatch(t.Context()) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("control did not enter original decision")
	}
	receiveReviewControls(f, []string{"status"}, []string{"/status"})
	close(release)
	<-done
	notices := control.Notices()
	if len(notices) != 1 {
		t.Fatal("expected real uncertain control outcome", notices)
	}
	// Reopen both owners after publication, before the bridge observes the notice.
	f.reopen()
	control, err = textchannel.Open(f.root, decide)
	if err != nil {
		t.Fatal(err)
	}
	f.b.host.TextControl = control
	f.poll()
	if len(f.sent()) != 1 || f.sent()[0] != notices[0].Text || f.contexts[0] != "ctx-status" {
		t.Fatal("async outcome lost/new window not used", f.sent(), f.contexts)
	}
	f.b.dispatch(t.Context(), f.p)
	f.advance(ordinaryDelay)
	f.poll()
	receiveReviewControls(f, []string{"approve", "status"}, []string{"/approve A1 1", "/status"})
	f.b.dispatch(t.Context(), f.p)
	f.poll()
	f.reopen()
	f.poll()
	if decisions.Load() != 1 || strings.Count(strings.Join(f.sent(), ""), notices[0].Text) != 1 {
		t.Fatal("control or outcome duplicated", decisions.Load(), f.sent())
	}
}

func TestReviewHistoricalControlNoticeIsNotAdmitted(t *testing.T) {
	f := newDeliveryFixture(t)
	in := textchannel.Inbound{Channel: "weixin", Conversation: "ctx-old", ID: "weixin:bot:old", Text: "/approve A1 1"}
	// The real controller published this before this delivery admission existed.
	old := f.b.host.TextControl.Handle(t.Context(), in, f.snapshot)
	if len(f.b.host.TextControl.Notices()) != 1 {
		t.Fatal("missing historical notice")
	}
	receiveReviewControls(f, []string{"old", "status"}, []string{in.Text, "/status"})
	f.b.dispatch(t.Context(), f.p)
	f.b.dispatch(t.Context(), f.p)
	f.advance(ordinaryDelay)
	f.poll()
	if strings.Contains(strings.Join(f.sent(), ""), old) {
		t.Fatal("historical feedback replayed", f.sent())
	}
}

func TestReviewProgressRecoveryRangeCapacityAndPriority(t *testing.T) {
	f := newDeliveryFixture(t)
	f.b.state.Delivery.StartAt = f.now.Add(-24 * time.Hour).UnixMilli()
	f.b.state.Window.Used = 10
	f.add("expired", "expired progress", progressPriority)
	f.snapshot.Items[0].SeenAt = f.now.Add(-13 * time.Hour).UnixMicro()
	for i := 0; i < 140; i++ {
		f.add(fmt.Sprintf("p%03d", i), fmt.Sprintf("progress-%03d", i), progressPriority)
	}
	f.add("result", "important result", resultPriority)
	f.poll()
	f.window("B")
	f.poll()
	f.reopen()
	recoverDelivery(f, "recover")
	if len(f.sent()) == 0 || !strings.Contains(f.sent()[0], "important result") {
		t.Fatal("results lost recovery priority", f.sent())
	}
	all := strings.Join(f.sent(), "\n")
	// Last 128 shared items includes the result, hence p013 through p139 only.
	if strings.Contains(all, "expired progress") || strings.Contains(all, "progress-012") || !strings.Contains(all, "progress-013") || !strings.Contains(all, "progress-139") {
		t.Fatal("recovery range/capacity wrong", all)
	}
	if f.used() > windowSendLimit || f.b.state.Window.ProgressUsed > progressSendLimit {
		t.Fatal("recovery exceeded local budget")
	}
	if f.snapshot.Items[1].Text != "progress-000" {
		t.Fatal("shared history mutated")
	}
	f.reopen()
	recoverDelivery(f, "again")
	if strings.Count(strings.Join(f.sent(), ""), "progress-139") != 1 {
		t.Fatal("recovered tail replayed")
	}
}

func TestReviewProgressCoveredByResultIsNotRecovered(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("first", "first", resultPriority)
	f.poll()
	f.add("progress", "obsolete stage", progressPriority)
	f.poll()
	f.window("B")
	f.poll()
	f.add("final", "complete result", resultPriority)
	f.snapshot.Items[1].TurnKey = "turn-final"
	f.poll()
	f.reopen()
	recoverDelivery(f, "recover")
	if strings.Contains(strings.Join(f.sent(), ""), "obsolete stage") || strings.Count(strings.Join(f.sent(), ""), "complete result") != 1 {
		t.Fatal("covered progress recovered", f.sent())
	}
}

func observeReviewControlIM(f *deliveryFixture, control *textchannel.Store) {
	// Production app publishes the notice into shared IM before the sender sees it.
	control.SetNoticeObserver(func(n textchannel.Notice) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.snapshot.Items = append(f.snapshot.Items, api.Item{ID: "control:" + n.ID, Kind: "controlNotice", Text: n.Text, Status: "completed", SeenAt: n.SeenAt})
	})
}

func TestReviewBodyRecoveryAcrossInflightWindowChange(t *testing.T) {
	f := newDeliveryFixture(t)
	body := strings.Repeat("a", 1500) + "\n\n" + strings.Repeat("b", 1500) + "\n\n" + strings.Repeat("c", 1500)
	f.b.state.Window.Used = 9
	f.add("long", body, resultPriority)
	f.poll()
	preview := f.b.state.Outputs["item:long:preview"]
	f.hook = func(string) string {
		f.window("new-window")
		f.mu.Lock()
		f.hook = nil
		f.mu.Unlock()
		return `{"ret":0}`
	}
	recoverDelivery(f, "recover")
	if len(f.sent()) != 4 || f.used() != 2 {
		t.Fatal("new window lost original continuation or debited old in-flight send", len(f.sent()), f.used())
	}
	for i, part := range chunks(body) {
		if f.sent()[i+1] != part {
			t.Fatal("part order changed", i)
		}
	}
	if f.b.state.Outputs["item:long:0"].ContextToken != "context-recover" || !reflect.DeepEqual(preview, f.b.state.Outputs["item:long:preview"]) {
		t.Fatal("original context/preview receipt overwritten")
	}
	f.reopen()
	f.poll()
	if len(f.sent()) != 4 {
		t.Fatal("inflight recovery replayed")
	}
}

func TestReviewAsyncQuestionOutcomeSurvivesLaterControl(t *testing.T) {
	f := newDeliveryFixture(t)
	f.question("question")
	entered, release := make(chan struct{}), make(chan struct{})
	var answers atomic.Int32
	f.b.host.TextControl.SetAsyncAnswerer(func(_ context.Context, in textchannel.Inbound, _ string) (api.Receipt, error) {
		answers.Add(1)
		close(entered)
		<-release
		return api.Receipt{ID: in.ID, Outcome: "unknown"}, errors.New("fixture uncertain answer")
	})
	if _, err := f.b.host.TextControl.AsyncCard(f.snapshot.Items[0], "runtime"); err != nil {
		t.Fatal(err)
	}
	observeReviewControlIM(f, f.b.host.TextControl)
	receiveReviewControls(f, []string{"answer"}, []string{"/answer Q1 1"})
	done := make(chan struct{})
	go func() { defer close(done); f.b.dispatch(t.Context()) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("answer did not enter original operation")
	}
	receiveReviewControls(f, []string{"status"}, []string{"/status"})
	close(release)
	<-done
	notices := f.b.host.TextControl.Notices()
	if len(notices) != 1 {
		t.Fatal("missing real question outcome", notices)
	}
	f.reopen()
	f.poll()
	if !strings.Contains(strings.Join(f.sent(), ""), notices[0].Text) || answers.Load() != 1 {
		t.Fatal("async question result lost", f.sent())
	}
	f.b.dispatch(t.Context(), f.p)
	f.advance(ordinaryDelay)
	f.poll()
	f.reopen()
	f.poll()
	if strings.Count(strings.Join(f.sent(), ""), notices[0].Text) != 1 {
		t.Fatal("shared IM notice duplicated", f.sent())
	}
}

func TestReviewRoutineControlAcknowledgementStaysSilent(t *testing.T) {
	f := newDeliveryFixture(t)
	a := api.Approval{ID: "native", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "allow", Label: "Allow"}}}
	if _, _, err := f.b.host.TextControl.Card(a); err != nil {
		t.Fatal(err)
	}
	f.snapshot.Approvals = []api.Approval{a}
	receiveReviewControls(f, []string{"approve", "status"}, []string{"/approve A1 1", "/status"})
	f.b.dispatch(t.Context(), f.p)
	f.b.dispatch(t.Context(), f.p)
	f.advance(ordinaryDelay)
	f.poll()
	if len(f.b.host.TextControl.Notices()) != 0 || len(f.sent()) != 1 || !strings.Contains(f.sent()[0], "Bot：") {
		t.Fatal("manufactured routine acknowledgement", f.sent())
	}
}

func TestReviewRetiredProgressUnknownAndAgeCutoff(t *testing.T) {
	f := newDeliveryFixture(t)
	f.b.state.Delivery.StartAt = f.now.Add(-24 * time.Hour).UnixMilli()
	f.add("first", "first", resultPriority)
	f.poll()
	for _, id := range []string{"expired", "uncertain", "recent"} {
		f.add(id, id+" progress", progressPriority)
	}
	f.snapshot.Items[1].SeenAt = f.now.Add(-13 * time.Hour).UnixMicro()
	// Do not allow the three-item batching threshold to send them before rotation.
	f.b.state.Window.Used = 10
	f.poll()
	f.window("B")
	f.poll()
	original := outbound{State: "unknown", ClientID: "original-progress", Attempts: 1}
	f.b.state.Outputs["item:uncertain:0"] = original
	if err := f.b.saveLocked(); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	recoverDelivery(f, "recover")
	sent := strings.Join(f.sent(), "\n")
	if strings.Contains(sent, "expired progress") || strings.Contains(sent, "uncertain progress") || !strings.Contains(sent, "recent progress") {
		t.Fatal("recovery ignored original receipt/age bound", sent)
	}
	if !reflect.DeepEqual(original, f.b.state.Outputs["item:uncertain:0"]) {
		t.Fatal("uncertain progress receipt changed")
	}
}

func TestReviewOversizedCacheExcerptCannotBecomeAcceptedBody(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("oversized", "Useful result.\n\n"+strings.Repeat("x", 65<<10), resultPriority)
	f.poll()
	if f.b.state.Outputs["item:oversized"].State == "accepted" || f.b.state.Outputs["item:oversized:0"].State != "" {
		t.Fatal("bounded cache excerpt claimed full delivery")
	}
	receipt := f.b.state.Outputs["item:oversized:preview"]
	f.reopen()
	recoverDelivery(f, "recover")
	if !reflect.DeepEqual(receipt, f.b.state.Outputs["item:oversized:preview"]) || strings.Count(strings.Join(f.sent(), ""), "Useful result.") != 1 {
		t.Fatal("oversized excerpt replayed")
	}
}

func TestReviewLegacyUnknownClassificationDoesNotReconstructLostBatchMembership(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("first", "first", resultPriority)
	f.poll()
	f.add("legacy", "legacy same body", progressPriority)
	f.poll()
	observation := f.b.state.Delivery.Seen["item:legacy"]
	// 3b3398ae persisted no classification field.
	f.b.state.Delivery.Seen["item:legacy"] = deliveryObservation{Digest: observation.Digest, Ready: observation.Ready}
	// Older rotation could discard a frozen batch while its HTTP was in flight.
	// Its missing membership is not evidence that the visible item was unsent.
	f.b.state.Delivery.Queue = nil
	original := outbound{State: "accepted", ClientID: "legacy-batch", Attempts: 1}
	f.b.state.Outputs["batch:legacy:0"] = original
	if err := f.b.saveLocked(); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	f.window("B")
	f.snapshot.ReportSources = append(f.snapshot.ReportSources, api.ReportSource{RuntimeOwner: "runtime", TurnKey: "turn-legacy", ReportID: "late-final", Phase: "completed"})
	f.poll()
	recoverDelivery(f, "recover")
	if strings.Contains(strings.Join(f.sent(), ""), "legacy same body") || !reflect.DeepEqual(original, f.b.state.Outputs["batch:legacy:0"]) {
		t.Fatal("legacy observation invented delivery eligibility", f.sent())
	}
}

func TestReviewRecoveredProgressDoesNotReactivateFiles(t *testing.T) {
	f := newDeliveryFixture(t)
	f.add("first", "first", resultPriority)
	f.poll()
	f.add("progress", "file ready", progressPriority)
	f.snapshot.Items[1].Artifacts = []api.Artifact{{ID: "old-file", Name: "result.txt"}}
	f.poll()
	f.window("B")
	f.poll()
	f.reopen()
	recoverDelivery(f, "recover")
	all := strings.Join(f.sent(), "\n")
	if !strings.Contains(all, "file ready") || !strings.Contains(all, "result.txt") || !strings.Contains(all, "完整文件可在桌面") {
		t.Fatal("recovered progress lost file entry", all)
	}
	if out := f.b.state.Outputs[artifactKey("turn-progress", "old-file")]; out.Attempts != 0 || out.State != "deferred" {
		t.Fatal("old file resumed", out)
	}
}

func TestReviewProgressCoverageRequiresKnownTurnResult(t *testing.T) {
	for _, kind := range []string{"missing-turn", "tool-failure"} {
		t.Run(kind, func(t *testing.T) {
			f := newDeliveryFixture(t)
			f.add("first", "first", resultPriority)
			f.poll()
			f.add("progress", "still useful progress", progressPriority)
			turn := "same-turn"
			if kind == "missing-turn" {
				turn = ""
			}
			f.snapshot.Items[1].TurnKey = turn
			f.poll()
			f.window("B")
			f.poll()
			f.add("failure", "failure detail", progressPriority)
			failure := &f.snapshot.Items[2]
			failure.Status = "failed"
			failure.TurnKey = turn
			if kind == "tool-failure" {
				failure.Kind = "tool"
			}
			f.poll()
			f.reopen()
			recoverDelivery(f, "recover")
			if !strings.Contains(strings.Join(f.sent(), ""), "still useful progress") {
				t.Fatal("missing turn or a tool failure incorrectly covered progress", f.sent())
			}
		})
	}
}
