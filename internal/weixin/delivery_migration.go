package weixin

import (
	"fmt"
	"strconv"
	"strings"
)

// Preserve old batch membership before clearing the superseded progress spool.
// A crash between HTTP and parent bookkeeping must never turn an accepted or
// unknown original intent into apparently unsent individual messages.
func (b *Bridge) migrateDeliveryLocked() {
	if b.state.Delivery.Initialized {
		return
	}
	bind := func(ids []string, key string) {
		out := b.state.Outputs[key]
		if out.State == "" {
			return
		}
		for _, id := range ids {
			if _, exists := b.state.Outputs["item:"+id]; !exists {
				b.state.Outputs["item:"+id] = outbound{State: out.State, Reason: "delivery:" + key}
			}
		}
	}
	if len(b.state.ProgressBatch) > 0 {
		bind(b.state.ProgressBatch, "progress:"+textDigest(strings.Join(b.state.ProgressBatch, "\x00")))
	}
	if b.state.FinalProgressFor != "" {
		ids := append(append([]string(nil), b.state.FinalProgressIDs...), b.state.FinalProgressFor)
		bind(ids, "item:"+b.state.FinalProgressFor+":0")
	}
	for _, old := range b.state.Progress {
		if _, exists := b.state.Outputs["item:"+old.ID]; !exists && old.ID != b.state.PendingFinal {
			b.state.Outputs["item:"+old.ID] = outbound{State: "superseded", Reason: "legacy_progress"}
		}
	}
}

func (b *Bridge) deliveryPartKeyLocked(key string, index int) string {
	legacy := fmt.Sprintf("%s:part:%d", key, index)
	if _, exists := b.state.Outputs[legacy]; exists {
		return legacy
	}
	return fmt.Sprintf("%s:%d", key, index)
}

// Old async cards used the IM item key. Their replacement must not turn an
// already-attempted original card (even a partially accepted one) into a resend.
func (b *Bridge) bindLegacyQuestionLocked(q queuedDelivery) {
	if _, exists := b.state.Outputs[q.Key]; exists {
		return
	}
	parent := "item:" + q.QuestionID
	old := b.state.Outputs[parent]
	if old.State == "accepted" || old.State == "unknown" || old.State == "rejected" {
		b.state.Outputs[q.Key] = outbound{State: old.State, Reason: "delivery:" + parent}
		return
	}
	state := ""
	for key, part := range b.state.Outputs {
		if !strings.HasPrefix(key, parent+":") {
			continue
		}
		suffix := strings.TrimPrefix(strings.TrimPrefix(key, parent+":"), "part:")
		if n, err := strconv.Atoi(suffix); suffix != "compact" && (err != nil || n < 0) {
			continue
		}
		switch part.State {
		case "unknown":
			state = "unknown"
		case "rejected":
			if state != "unknown" {
				state = "rejected"
			}
		case "accepted":
			if state == "" {
				state = "partial"
			}
		}
	}
	if state != "" {
		b.state.Outputs[q.Key] = outbound{State: state, Reason: "delivery:" + parent}
	}
}
