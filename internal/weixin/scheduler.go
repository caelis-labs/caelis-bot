package weixin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const (
	approvalPriority = iota
	questionPriority
	resultPriority
	progressPriority
)
const ordinaryDelay = 30 * time.Second
const progressDivider = "\n\n——\n\n"

// This is a delivery spool, not conversation or execution state. Original IM
// IDs, native interaction handles and the existing Outputs receipts remain the
// authorities. Frozen parts make restart, batching and partial sends idempotent.
type queuedDelivery struct {
	Key        string        `json:"key"`
	Members    []string      `json:"members,omitempty"`
	Body       string        `json:"body,omitempty"`
	Parts      []string      `json:"parts,omitempty"`
	Priority   int           `json:"priority"`
	InputID    string        `json:"inputId"`
	At         int64         `json:"at"`
	Held       bool          `json:"held,omitempty"`
	ApprovalID string        `json:"approvalId,omitempty"`
	NoticeFor  string        `json:"noticeFor,omitempty"`
	QuestionID string        `json:"questionId,omitempty"`
	Artifact   *api.Artifact `json:"artifact,omitempty"`
	TurnKey    string        `json:"turnKey,omitempty"`
}
type deliveryObservation struct {
	Digest string `json:"digest"`
	Ready  bool   `json:"ready,omitempty"`
}
type deliverySchedule struct {
	CapacityDropped bool                           `json:"capacityDropped,omitempty"`
	Initialized     bool                           `json:"initialized,omitempty"`
	StartAt         int64                          `json:"startAt,omitempty"`
	WindowID        string                         `json:"windowId,omitempty"`
	PausedWindow    string                         `json:"pausedWindow,omitempty"`
	Seen            map[string]deliveryObservation `json:"seen,omitempty"`
	Queue           []queuedDelivery               `json:"queue,omitempty"`
	LastSent        int64                          `json:"lastSent,omitempty"`
	Phase           string                         `json:"phase,omitempty"`
	WorkerSignals   map[string]string              `json:"workerSignals,omitempty"`
}

func (b *Bridge) now() time.Time {
	if b.host.Now != nil {
		return b.host.Now()
	}
	return time.Now()
}

func (b *Bridge) output(ctx context.Context, p *protocol) {
	b.deliveryMu.Lock()
	defer b.deliveryMu.Unlock()
	if b.host.Snapshot == nil {
		return
	}
	snapshot := b.host.Snapshot()
	b.ObserveTurn(snapshot) // classification only; never source-channel routing
	workers, workersKnown := []api.WorkState(nil), b.host.WorkStates != nil
	if workersKnown {
		workers = b.host.WorkStates()
	}
	b.observeInteractions(snapshot)
	b.observeDelivery(snapshot)
	b.observeNotices()
	b.mu.Lock()
	urgent := b.deliverySignalsLocked(snapshot, workers)
	b.rotateDeliveriesLocked()
	blocked := b.closed || b.updating || b.issue == "auth_expired" || b.issue == "storage" || b.state.PauseUntil > b.now().UnixMilli()
	err := b.saveLocked()
	b.mu.Unlock()
	if blocked || err != nil || p == nil || (snapshot.Connection != "ready" && snapshot.Connection != "connected") {
		return
	}
	// Re-select after every part. A new P0/P1 can preempt any not-yet-reserved
	// lower-priority part. An HTTP request already reserved retains its receipt.
	for n := 0; n < windowSendLimit && ctx.Err() == nil; n++ {
		current := b.host.Snapshot()
		b.observeInteractions(current)
		b.mu.Lock()
		b.rotateDeliveriesLocked()
		index := b.nextDeliveryLocked()
		if index < 0 || b.remainingLocked(true) == 0 {
			b.holdMissedResultsLocked()
			_ = b.saveLocked()
			b.mu.Unlock()
			return
		}
		candidate := b.state.Delivery.Queue[index]
		if candidate.Priority >= resultPriority && !b.deliveryDueLocked(candidate, snapshot, workers, workersKnown, urgent) {
			b.mu.Unlock()
			return
		}
		// Low-value output cannot consume the final slot; normal progress also has
		// a per-window cap. P0/P1 may use the last slot when user action is needed.
		critical := candidate.Priority <= resultPriority
		if b.remainingLocked(critical) == 0 || candidate.Priority == progressPriority && b.remainingProgressLocked() == 0 {
			b.mu.Unlock()
			return
		}
		candidate = b.freezeDeliveryLocked(index)
		scope := replyScope{inputID: b.state.Window.InputID, ownerID: b.state.OwnerID, botID: b.state.BotID, deliveryKey: candidate.Key}
		err := b.saveLocked()
		b.mu.Unlock()
		if err != nil {
			return
		}
		sendCtx := context.WithValue(ctx, replyScopeKey{}, scope)
		if !b.deliveryActionCurrent(candidate, current) {
			b.finishDelivery(candidate.Key, "obsolete")
			continue
		}
		if candidate.Artifact != nil {
			b.mu.Lock()
			file, ready := b.preparedFiles[candidate.Key]
			fileState := b.state.Outputs[candidate.Key].State
			b.mu.Unlock()
			if fileState != "" && fileState != "pending" && fileState != "preparing" {
				b.finishDelivery(candidate.Key, fileState)
				continue
			}
			if !ready {
				b.scheduleArtifact(sendCtx, p, candidate.TurnKey, *candidate.Artifact)
				return
			}
			b.sendFileOne(sendCtx, p, candidate.Key, file)
			b.mu.Lock()
			state := b.state.Outputs[candidate.Key].State
			b.mu.Unlock()
			if state == "accepted" || state == "unknown" || state == "rejected" {
				b.finishDelivery(candidate.Key, state)
				continue
			}
			return
		}
		part, key, terminal := b.nextDeliveryPart(candidate)
		if terminal != "" {
			b.finishDelivery(candidate.Key, terminal)
			continue
		}
		if part == "" {
			return
		}
		// A native card must fit completely in the remaining local allowance.
		// Do not spend a slot on a title while stranding its original operations.
		if candidate.Priority <= questionPriority && !b.cardFits(candidate) {
			b.mu.Lock()
			for i := range b.state.Delivery.Queue {
				if b.state.Delivery.Queue[i].Key == candidate.Key {
					b.state.Delivery.Queue[i].Held = true
				}
			}
			if len(candidate.Parts) > windowSendLimit {
				b.enqueueDeliveryLocked(queuedDelivery{Key: "oversized:" + candidate.Key, NoticeFor: candidate.Key, ApprovalID: candidate.ApprovalID, QuestionID: candidate.QuestionID, Body: "有一项待处理审批或问题超出微信窗口容量。请在桌面同一 Bot 对话查看完整选项并处理。", Priority: resultPriority})
			}
			_ = b.saveLocked()
			b.mu.Unlock()
			continue
		}
		_, state := b.sendOnePolicy(sendCtx, p, key, part, critical, candidate.Priority == progressPriority)
		switch state {
		case "accepted":
			_, _, terminal = b.nextDeliveryPart(candidate)
			if terminal != "" {
				b.finishDelivery(candidate.Key, terminal)
			}
		case "unknown", "rejected":
			b.finishDelivery(candidate.Key, state)
		case "superseded":
			continue // fresh authority/window won admission before an HTTP intent
		default:
			return
		}
		// Only one ordinary batch per observation. Interactions and a continuation
		// of an already-started long reply remain eligible in the next selection.
		urgent = false
	}
}

func (b *Bridge) rotateDeliveriesLocked() {
	d := &b.state.Delivery
	if d.WindowID == b.state.Window.InputID {
		return
	}
	keep := make([]queuedDelivery, 0, len(d.Queue))
	for _, q := range d.Queue {
		if q.Priority == progressPriority && q.InputID != b.state.Window.InputID {
			delete(b.preparedFiles, q.Key)
			if q.Artifact != nil {
				if old := b.state.Outputs[q.Key]; old.Attempts == 0 {
					old.State, old.Reason = "deferred", "new_input"
					old.ArtifactID, old.FileName, old.FileTurnKey, old.FileInputID = q.Artifact.ID, q.Artifact.Name, q.TurnKey, q.InputID
					b.state.FileIssue = "file_deferred"
					b.state.Outputs[q.Key] = old
				}
			} else if b.state.Outputs[q.Key].Attempts == 0 {
				b.state.Outputs[q.Key] = outbound{State: "superseded", Reason: "new_input"}
			}
			continue
		}
		if q.Priority == resultPriority && q.InputID == d.PausedWindow {
			q.Held = true
		}
		if q.Priority <= questionPriority {
			q.Held = false
		}
		q.InputID = b.state.Window.InputID
		keep = append(keep, q)
	}
	d.Queue, d.WindowID = keep, b.state.Window.InputID
}

func (b *Bridge) holdMissedResultsLocked() {
	if b.remainingLocked(true) != 0 {
		return
	}
	for i := range b.state.Delivery.Queue {
		if b.state.Delivery.Queue[i].Priority == resultPriority {
			b.state.Delivery.Queue[i].Held = true
		}
	}
	b.state.Delivery.PausedWindow = b.state.Window.InputID
}

func (b *Bridge) nextDeliveryLocked() int {
	index := -1
	for i, q := range b.state.Delivery.Queue {
		if q.Held {
			continue
		}
		if index < 0 || q.Priority < b.state.Delivery.Queue[index].Priority {
			index = i
		}
	}
	return index
}

func (b *Bridge) deliveryDueLocked(q queuedDelivery, snapshot api.Snapshot, work []api.WorkState, known bool, urgent bool) bool {
	if !b.state.Window.Replied || urgent || len(q.Parts) > 0 {
		return true
	}
	count, units := 0, 0
	for _, item := range b.state.Delivery.Queue {
		if !item.Held && item.Priority == q.Priority {
			count++
			units += textUnits(item.Body)
		}
	}
	if count >= 3 || units >= textChunkUnits*7/10 {
		return true
	}
	delay := ordinaryDelay
	active := !known
	for _, w := range work {
		if !w.TaskEnded {
			active = true
		}
	}
	if !active && snapshot.Phase != "working" && snapshot.Phase != "sending" {
		delay = 2 * time.Second
	}
	// A burst arriving long after the last delivery is immediately eligible.
	base := b.state.Delivery.LastSent
	if delay < ordinaryDelay {
		base = max(base, q.At)
	}
	return b.now().Sub(time.UnixMilli(base)) >= delay
}

func (b *Bridge) freezeDeliveryLocked(index int) queuedDelivery {
	q := b.state.Delivery.Queue[index]
	if q.Artifact != nil || len(q.Parts) > 0 {
		return q
	}
	q.Members = []string{q.Key}
	if q.Priority >= resultPriority && q.NoticeFor == "" {
		keep := b.state.Delivery.Queue[:index+1]
		barrier := false
		for _, other := range b.state.Delivery.Queue[index+1:] {
			// Never merge action cards, files, partial replies, different priorities,
			// or held recovery results. Preserve FIFO within each priority.
			if !barrier && !other.Held && other.Priority == q.Priority && other.Artifact == nil && other.NoticeFor == "" && len(other.Parts) == 0 && fitsText(q.Body+progressDivider+other.Body) {
				q.Body += progressDivider + other.Body
				q.Members = append(q.Members, other.Key)
			} else {
				if other.Priority == q.Priority && !other.Held {
					barrier = true
				}
				keep = append(keep, other)
			}
		}
		b.state.Delivery.Queue = keep
	}
	if len(q.Members) > 1 {
		q.Key = "batch:" + textDigest(strings.Join(q.Members, "\x00"))
	}
	if q.Priority >= resultPriority && b.state.Delivery.CapacityDropped {
		q.Body = "部分待发更新超出微信队列容量，完整记录保留在桌面同一 Bot 对话。\n\n" + q.Body
		b.state.Delivery.CapacityDropped = false
	}
	q.Parts = chunks(q.Body)
	if q.Priority >= resultPriority && len(q.Parts) > b.remainingLocked(q.Priority == resultPriority) {
		q.Parts = []string{capacityPreview(q.Body)}
	}
	b.state.Delivery.Queue[index] = q
	return q
}

func capacityPreview(body string) string {
	// Complete Markdown blocks only, explicitly labelled as an excerpt. Never
	// fabricate a summary or silently cut a code block / the middle of a result.
	prefix := "回复节选（非全文）：\n\n"
	result := ""
	for _, block := range markdownBlocks(body) {
		if !fitsText(prefix + result + block.text + "\n\n" + longReplyNotice) {
			break
		}
		result += block.text
	}
	if strings.TrimSpace(result) == "" {
		return longReplyNotice
	}
	return prefix + strings.TrimSpace(result) + "\n\n" + longReplyNotice
}

func (b *Bridge) nextDeliveryPart(q queuedDelivery) (string, string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, part := range q.Parts {
		key := b.deliveryPartKeyLocked(q.Key, i)
		old := b.state.Outputs[key]
		switch old.State {
		case "accepted":
			continue
		case "unknown", "rejected":
			return "", "", old.State
		case "":
			return part, key, ""
		default:
			return "", "", "obsolete"
		}
	}
	return "", "", "accepted"
}
func (b *Bridge) cardFits(q queuedDelivery) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	needed := 0
	for i := range q.Parts {
		if b.state.Outputs[b.deliveryPartKeyLocked(q.Key, i)].State != "accepted" {
			needed++
		}
	}
	return needed <= b.remainingLocked(true)
}
func (b *Bridge) finishDelivery(key, state string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, q := range b.state.Delivery.Queue {
		if q.Key != key {
			continue
		}
		if q.Artifact == nil {
			for _, member := range q.Members {
				b.state.Outputs[member] = outbound{State: state, Reason: "delivery:" + q.Key}
			}
			if len(q.Members) == 0 {
				b.state.Outputs[q.Key] = outbound{State: state}
			}
		}
		b.state.Delivery.Queue = append(b.state.Delivery.Queue[:i], b.state.Delivery.Queue[i+1:]...)
		delete(b.preparedFiles, key)
		_ = b.saveLocked()
		return
	}
}

func (b *Bridge) deliverySignalsLocked(s api.Snapshot, work []api.WorkState) bool {
	d := &b.state.Delivery
	urgent := d.Phase != s.Phase && (s.Phase == "failed" || s.Phase == "interrupted")
	d.Phase = s.Phase
	signals := map[string]string{}
	for _, w := range work {
		key := w.Runtime + "\x00" + w.Task.ID + "\x00" + w.ExecutionKey
		signal := fmt.Sprintf("%t:%s", w.TaskEnded, w.Task.Status)
		if prior := d.WorkerSignals[key]; prior != signal && (w.TaskEnded || w.Task.Status == "failed") {
			urgent = true
		}
		signals[key] = signal
	}
	d.WorkerSignals = signals
	return urgent
}
