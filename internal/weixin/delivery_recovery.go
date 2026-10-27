package weixin

import (
	"strconv"
	"strings"
)

// Reuse only the current shared IM tail: no paging or adapter-owned transcript.
const recoveryItemLimit = 128

func (b *Bridge) deliveryAttemptedLocked(key string) bool {
	for id, out := range b.state.Outputs {
		if id != key {
			if !strings.HasPrefix(id, key+":") {
				continue
			}
			suffix := strings.TrimPrefix(id, key+":")
			if _, err := strconv.Atoi(strings.TrimPrefix(suffix, "part:")); err != nil && suffix != "preview" && suffix != "compact" {
				continue
			}
		}
		if out.Attempts > 0 || out.ClientID != "" || out.State == "accepted" || out.State == "unknown" || out.State == "rejected" {
			return true
		}
	}
	return false
}

func (b *Bridge) deliveryDispositionLocked(key, state, reason string) {
	old := b.state.Outputs[key]
	if old.Attempts > 0 || old.ClientID != "" || old.State == "accepted" || old.State == "unknown" || old.State == "rejected" {
		return
	}
	b.state.Outputs[key] = outbound{State: state, Reason: reason}
}

func (b *Bridge) retireTextLocked(q queuedDelivery, reason string) {
	// Retired batch membership must remain connected to its original part ledger.
	for _, member := range q.Members {
		if b.deliveryAttemptedLocked(q.Key) {
			b.deliveryDispositionLocked(member, "unknown", "delivery:"+q.Key)
		} else {
			b.deliveryDispositionLocked(member, "superseded", reason)
		}
	}
	if out := b.state.Outputs[q.Key]; out.State != "pending" || out.Reason != "control_feedback" {
		b.deliveryDispositionLocked(q.Key, "superseded", reason)
	}
}

func (b *Bridge) recoverableDispositionLocked(key string) bool {
	out := b.state.Outputs[key]
	return out.State == "superseded" && (out.Reason == "new_input" || out.Reason == "delivery_capacity") && !b.deliveryAttemptedLocked(key)
}

func (b *Bridge) deliveryPreview(q queuedDelivery) (string, bool) {
	if q.Priority < resultPriority {
		return "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !q.BodyExcerpt {
		// Once original body delivery has begun, preserve its continuation across
		// renewal. Its own accepted/unknown/rejected parts govern progress.
		for i := range q.Parts {
			if b.state.Outputs[b.deliveryPartKeyLocked(q.Key, i)].State != "" {
				return "", false
			}
		}
		if b.state.Window.Recover {
			return "", false
		}
	}
	if b.state.Outputs[q.Key+":preview"].State != "" {
		return "", true
	}
	if !q.BodyExcerpt {
		remaining := b.remainingLocked(q.Priority == resultPriority)
		if q.Priority == progressPriority {
			remaining = b.remainingProgressLocked()
		}
		if len(q.Parts) <= remaining {
			return "", false
		}
		return capacityPreview(q.Body), true
	}
	if fitsText(q.Body) {
		return q.Body, true
	}
	return capacityPreview(q.Body), true
}

func (b *Bridge) holdDelivery(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.state.Delivery.Queue {
		if b.state.Delivery.Queue[i].Key == key {
			b.state.Delivery.Queue[i].Held = true
		}
	}
	_ = b.saveLocked()
}

func (b *Bridge) recoverProgress() {
	if b.host.Snapshot == nil {
		return
	}
	s := b.host.Snapshot()
	b.ObserveTurn(s)
	kinds := b.outputKinds(s)
	b.observeDelivery(s)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.state.Window.Recover {
		return
	}
	cutoff := max(b.state.Delivery.StartAt*1000, b.now().Add(-windowMaxAge).UnixMicro())
	covered := map[string]bool{}
	for _, item := range s.Items {
		if item.TurnKey != "" && item.Kind == "assistant" && (kinds[item.ID] == "final" || item.Status == "failed") {
			covered[item.TurnKey] = true
		}
	}
	eligible := map[string]bool{}
	for _, item := range s.Items[max(0, len(s.Items)-recoveryItemLimit):] {
		key := outputKey(item)
		if item.Kind != "assistant" || item.SeenAt < cutoff || kinds[item.ID] != "progress" || covered[item.TurnKey] || strings.TrimSpace(item.Text) == "" {
			continue
		}
		if _, seen := b.state.Delivery.Seen[key]; !seen {
			continue
		}
		eligible[key] = true
		if !b.recoverableDispositionLocked(key) {
			continue
		}
		body := withArtifactNames(item.Text, item.Artifacts)
		if len(item.Artifacts) > 0 {
			body += "\n\n完整文件可在桌面同一 Bot 对话获取。"
		}
		b.restoreRetiredDeliveryLocked(queuedDelivery{Key: key, Body: body, Priority: progressPriority, TurnKey: item.TurnKey})
	}
	for i, q := range b.state.Delivery.Queue {
		if q.Priority != progressPriority || q.Artifact != nil || q.BodyExcerpt {
			continue
		}
		canResume := eligible[q.Key]
		if len(q.Members) > 0 {
			canResume = true
			for _, member := range q.Members {
				canResume = canResume && eligible[member]
			}
		}
		if canResume {
			b.state.Delivery.Queue[i].Held = false
		}
	}
	_ = b.saveLocked()
}

// Queue pressure must not erase the proof that this was an unattempted candidate.
func (b *Bridge) restoreRetiredDeliveryLocked(q queuedDelivery) {
	old := b.state.Outputs[q.Key]
	delete(b.state.Outputs, q.Key)
	b.enqueueDeliveryLocked(q)
	for _, pending := range b.state.Delivery.Queue {
		if pending.Key == q.Key {
			return
		}
	}
	if _, exists := b.state.Outputs[q.Key]; !exists {
		b.state.Outputs[q.Key] = old
	}
}
