package weixin

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (b *Bridge) observeDelivery(s api.Snapshot) {
	kinds := b.outputKinds(s)
	b.mu.Lock()
	defer b.mu.Unlock()
	d := &b.state.Delivery
	legacyResults := b.migrateDeliveryLocked(kinds)
	if d.StartAt == 0 {
		d.StartAt = b.state.Window.InputAt
	}
	if d.Seen == nil {
		d.Seen = map[string]deliveryObservation{}
	}
	b.rotateDeliveriesLocked()
	for _, item := range s.Items {
		if item.ID == "" || item.Kind != "assistant" && item.Kind != "tool" {
			continue
		}
		key := outputKey(item)
		encoded, _ := json.Marshal(struct {
			Text, Status string
			Artifacts    []api.Artifact
		}{item.Text, item.Status, item.Artifacts})
		ready := item.Status == "completed" || item.Status == "accepted" || item.Status == "failed"
		seen, observed := d.Seen[key]
		next := deliveryObservation{Digest: textDigest(string(encoded)), Ready: ready}
		d.Seen[key] = next
		priority := progressPriority
		if kinds[item.ID] == "final" || kinds[item.ID] == "task" || item.Status == "failed" {
			priority = resultPriority
		}
		// A completed commentary may subsequently become the native terminal item.
		// Promote its unattempted delivery, never repeat an accepted commentary.
		for i := range d.Queue {
			if (d.Queue[i].Key == key || slices.Contains(d.Queue[i].Members, key)) && priority < d.Queue[i].Priority {
				d.Queue[i].Priority = priority
			}
		}
		if !ready || observed && seen == next {
			continue
		}
		// SeenAt is the shared IM's presentation cursor, used only for delivery
		// freshness. Restored/paged history with no cursor cannot become new output.
		historical := !observed && (item.SeenAt == 0 || item.SeenAt < d.StartAt*1000)
		if historical && !legacyResults[item.ID] {
			if _, exists := b.state.Outputs[key]; !exists {
				b.state.Outputs[key] = outbound{State: "skip"}
			}
			b.deferOldArtifactsLocked(item, "skip")
			continue
		}
		if !observed && b.state.Window.InputID == "" {
			b.state.Outputs[key] = outbound{State: "skip"}
			b.deferOldArtifactsLocked(item, "skip")
			continue
		}
		if priority == progressPriority && !observed && item.SeenAt < b.state.Window.InputAt*1000 && !legacyResults[item.ID] {
			b.state.Outputs[key] = outbound{State: "superseded", Reason: "new_input"}
			b.deferOldArtifactsLocked(item, "deferred")
			continue
		}
		if kinds[item.ID] == "question" || len(item.QuestionCards) > 0 || len(item.AsyncQuestions) > 0 {
			continue
		}
		oldFiles := !observed && item.SeenAt < b.state.Window.InputAt*1000
		held := legacyResults[item.ID] || oldFiles && d.PausedWindow != ""
		if item.Kind == "assistant" && strings.TrimSpace(item.Text) != "" && item.Text != api.SilentReminder {
			body := withArtifactNames(item.Text, item.Artifacts)
			if oldFiles && len(item.Artifacts) > 0 {
				body += "\n\n完整文件可在桌面同一 Bot 对话获取。"
			}
			if item.Task != nil && item.Task.Title != "" {
				if item.Text == item.Task.Title {
					body = "**" + item.Task.Title + "**" + strings.TrimPrefix(body, item.Text)
				} else {
					body = "**" + item.Task.Title + "**\n\n" + body
				}
			}
			b.enqueueDeliveryLocked(queuedDelivery{Key: key, Body: body, Priority: priority, Held: held})
		}
		if strings.TrimSpace(item.Text) == "" && len(item.Artifacts) > 0 && (oldFiles || len(item.Artifacts) > b.remainingProgressLocked()) {
			b.enqueueDeliveryLocked(queuedDelivery{Key: key + ":files", Body: withArtifactNames("", item.Artifacts) + "\n\n完整文件可在桌面同一 Bot 对话获取。", Priority: resultPriority, Held: held})
		}
		if oldFiles {
			b.deferOldArtifactsLocked(item, "deferred")
			continue // recovering text cannot reactivate old nonurgent files
		}
		for _, artifact := range item.Artifacts {
			if artifact.ID == "" || !fileName(artifact.Name) {
				continue
			}
			file := artifact
			b.enqueueDeliveryLocked(queuedDelivery{Key: artifactKey(item.TurnKey, artifact.ID), Priority: progressPriority, Artifact: &file, TurnKey: item.TurnKey})
		}
	}
	// The old source-bound progress spool is intentionally not resumed. A known
	// unattempted terminal result above is retained for explicit /r recovery.
	if d.Initialized && d.Phase != "" && d.Phase != s.Phase {
		body := map[string]string{"failed": "Bot 未能完成当前工作，可在同一对话查看完整结果并询问原因。", "interrupted": "当前工作已停止。"}[s.Phase]
		if body != "" {
			b.enqueueDeliveryLocked(queuedDelivery{Key: "status:" + textDigest(s.RuntimeOwner+"\x00"+s.CurrentTurn+"\x00"+s.LastReceipt.ID+"\x00"+s.Phase), Body: body, Priority: resultPriority})
		}
	}
	b.state.Progress, b.state.ProgressBatch = nil, nil
	b.state.FinalProgressFor, b.state.FinalProgressIDs = "", nil
	// Keep an original known final ID until the shared IM presents it; startup
	// may initially have no visible history. Never synthesize its missing body.
	if d.Seen["item:"+b.state.PendingFinal].Ready {
		b.state.PendingFinal = ""
	}
	d.Initialized, b.state.LocalIMSeeded, b.state.MirrorV2 = true, true, true
	// Bound observation metadata to the currently visible IM plus queued items.
	// Older paged items retain their original SeenAt and fail the freshness gate.
	if len(d.Seen) > 1024 {
		keep := map[string]deliveryObservation{}
		for _, item := range s.Items {
			if v, ok := d.Seen[outputKey(item)]; ok {
				keep[outputKey(item)] = v
			}
		}
		for _, q := range d.Queue {
			if v, ok := d.Seen[q.Key]; ok {
				keep[q.Key] = v
			}
		}
		d.Seen = keep
	}
	_ = b.saveLocked()
}

func (b *Bridge) enqueueDeliveryLocked(q queuedDelivery) {
	if old := b.state.Outputs[q.Key+":compact"]; old.State != "" {
		b.state.Outputs[q.Key] = outbound{State: old.State, Reason: "delivery:" + q.Key + ":compact"}
		return
	}
	if old, exists := b.state.Outputs[q.Key]; exists && old.State != "pending" && old.State != "preparing" {
		return
	}
	for i, old := range b.state.Delivery.Queue {
		if slices.Contains(old.Members, q.Key) {
			return // frozen content belongs to its original batch/part receipts
		}
		if old.Key == q.Key {
			if len(old.Parts) == 0 {
				q.At, q.InputID, q.Held = old.At, old.InputID, old.Held
				b.state.Delivery.Queue[i] = q
			}
			return
		}
	}
	q.At, q.InputID = b.now().UnixMilli(), b.state.Window.InputID
	if q.Priority == resultPriority {
		if b.remainingLocked(true) == 0 {
			q.Held = true
		} else if b.state.Window.Recover {
			q.Held = false
		}
	}
	// Delivery content is bounded. Oversized results retain complete initial
	// Markdown blocks and an explicit route to the authoritative shared transcript.
	if q.Priority >= resultPriority && len(q.Body) > 64<<10 {
		q.Body = capacityPreview(q.Body)
	}
	bytes := len(q.Body)
	for _, pending := range b.state.Delivery.Queue {
		bytes += len(pending.Body)
	}
	if q.Priority >= resultPriority && bytes > 512<<10 {
		q.Body = capacityPreview(q.Body)
	}
	if len(b.state.Delivery.Queue) >= 128 {
		victim := -1
		for i, old := range b.state.Delivery.Queue {
			// Never displace frozen parts or higher/equal-priority current work.
			// A new result may replace a held older result; active cards always
			// displace lower-priority backlog rather than losing their priority.
			if len(old.Parts) == 0 && (old.Priority > q.Priority || old.Priority == q.Priority && q.Priority >= resultPriority && old.Held) && (victim < 0 || old.Priority > b.state.Delivery.Queue[victim].Priority) {
				victim = i
			}
		}
		b.state.Delivery.CapacityDropped = true
		if victim < 0 {
			return
		}
		old := b.state.Delivery.Queue[victim]
		b.state.Outputs[old.Key] = outbound{State: "superseded", Reason: "delivery_capacity"}
		delete(b.preparedFiles, old.Key)
		b.state.Delivery.Queue = append(b.state.Delivery.Queue[:victim], b.state.Delivery.Queue[victim+1:]...)
	}
	b.state.Delivery.Queue = append(b.state.Delivery.Queue, q)
}

func (b *Bridge) observeInteractions(s api.Snapshot) {
	control := b.host.TextControl
	if control == nil || s.Connection != "ready" && s.Connection != "connected" {
		return
	}
	// Use the existing lifecycle controller; no adapter-owned resolved/expiry
	// authority. Native renewal and explicit Worker finish close their own cards.
	var work []api.WorkState
	if b.host.WorkStates != nil {
		work = b.host.WorkStates()
	}
	if control.ReconcileAsync(s, work) != nil {
		return
	}
	var current []queuedDelivery
	for _, a := range s.Approvals {
		if a.Status != "pending" {
			continue
		}
		if _, claimed := control.Claimed(a.ID); claimed {
			continue
		}
		body, short, err := control.Card(a)
		if err != nil || body == "" {
			continue
		}
		priority := approvalPriority
		if len(a.Questions) > 0 {
			priority = questionPriority
		}
		current = append(current, queuedDelivery{Key: "approval:" + a.ID + ":" + short, Body: markdownApprovalCard(body), Priority: priority, ApprovalID: a.ID})
	}
	for _, item := range s.Items {
		if len(item.AsyncQuestions) > 0 && len(control.AsyncButtonQuestions(item.ID)) == 0 {
			_, _ = control.AsyncCard(item, s.RuntimeOwner)
		}
		body, ok := b.questionBody(item.ID)
		if !ok {
			continue
		}
		current = append(current, queuedDelivery{Key: "question:" + item.ID + ":" + textDigest(body), Body: body, Priority: questionPriority, QuestionID: item.ID})
	}
	valid := map[string]queuedDelivery{}
	for _, q := range current {
		valid[q.Key] = q
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	keep := b.state.Delivery.Queue[:0]
	for _, q := range b.state.Delivery.Queue {
		if q.Priority <= questionPriority || q.NoticeFor != "" {
			key := q.Key
			if q.NoticeFor != "" {
				key = q.NoticeFor
			}
			now, ok := valid[key]
			if !ok || q.NoticeFor == "" && now.Body != q.Body {
				b.state.Outputs[q.Key] = outbound{State: "obsolete", Reason: "interaction_changed"}
				continue
			}
		}
		keep = append(keep, q)
	}
	b.state.Delivery.Queue = keep
	for _, q := range current {
		if q.QuestionID != "" {
			b.bindLegacyQuestionLocked(q)
		}
		b.enqueueDeliveryLocked(q)
	}
}

func (b *Bridge) questionBody(itemID string) (string, bool) {
	if b.host.TextControl == nil {
		return "", false
	}
	// Only questions already visible in the shared snapshot are considered by
	// the caller. Private, unpresented Worker questions never enter this spool.
	var lines []string
	for _, q := range b.host.TextControl.AsyncButtonQuestions(itemID) {
		if q.State != "" || q.ExpiresAt > 0 && b.now().Unix() >= q.ExpiresAt {
			continue
		}
		lines = append(lines, "**["+q.ShortID+"] "+q.Title+"**")
		for i, option := range q.Options {
			lines = append(lines, fmt.Sprintf("[%d] %s", i+1, option))
		}
		suffix := " + 完整回答"
		if len(q.Options) > 0 {
			suffix = " + 选项标号"
		}
		lines = append(lines, "**提示：发送 /answer "+q.ShortID+suffix+"**", "")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), len(lines) > 0
}

func (b *Bridge) deliveryActionCurrent(q queuedDelivery, s api.Snapshot) bool {
	if q.Priority > questionPriority && q.NoticeFor == "" {
		return true
	}
	if b.host.TextControl == nil || s.Connection != "ready" && s.Connection != "connected" {
		return false
	}
	if q.ApprovalID != "" {
		if _, claimed := b.host.TextControl.Claimed(q.ApprovalID); claimed {
			return false
		}
		for _, a := range s.Approvals {
			if a.ID == q.ApprovalID && a.Status == "pending" {
				body, _, err := b.host.TextControl.Card(a)
				return err == nil && (q.NoticeFor != "" || markdownApprovalCard(body) == q.Body)
			}
		}
		return false
	}
	for _, item := range s.Items {
		if item.ID == q.QuestionID {
			body, ok := b.questionBody(item.ID)
			return ok && (q.NoticeFor != "" || body == q.Body)
		}
	}
	return false
}

func (b *Bridge) observeNotices() {
	if b.host.TextControl == nil {
		return
	}
	notices := b.host.TextControl.Notices()
	b.mu.Lock()
	defer b.mu.Unlock()
	w := b.state.Window
	for _, notice := range notices {
		if notice.Origin.Channel != "weixin" {
			continue
		}
		if notice.ID != textDigest("weixin\x00"+w.ContextToken+"\x00"+w.InputID) && notice.ID != textDigest("weixin\x00"+w.OwnerID+"\x00"+w.ContextToken+"\x00"+w.InputID) {
			continue
		}
		b.enqueueDeliveryLocked(queuedDelivery{Key: "notice:" + notice.ID, Body: notice.Text, Priority: resultPriority})
	}
}

// Remember unattempted historical artifacts in the same receipt ledger so a
// later text/metadata refresh cannot turn them into newly prepared uploads.
func (b *Bridge) deferOldArtifactsLocked(item api.Item, state string) {
	for _, artifact := range item.Artifacts {
		if artifact.ID == "" || !fileName(artifact.Name) {
			continue
		}
		key := artifactKey(item.TurnKey, artifact.ID)
		if _, exists := b.state.Outputs[key]; !exists {
			b.state.Outputs[key] = outbound{State: state, Reason: "historical_file", ArtifactID: artifact.ID, FileName: artifact.Name, FileTurnKey: item.TurnKey}
		}
	}
}
