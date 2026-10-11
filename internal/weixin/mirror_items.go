package weixin

import (
	"context"
	"fmt"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (b *Bridge) skipItem(key string, missed bool) {
	b.mu.Lock()
	if _, known := b.state.Outputs[key]; !known {
		b.state.Outputs[key] = outbound{State: "skip"}
		if missed {
			b.state.Missed++
		}
		_ = b.saveLocked()
	}
	b.mu.Unlock()
}

func (b *Bridge) deferFinal(id string) {
	b.mu.Lock()
	if b.state.PendingFinal != id {
		if old := b.state.PendingFinal; old != "" {
			b.state.Outputs["item:"+old] = outbound{State: "skip"}
			b.state.Missed++
		}
		b.state.PendingFinal = id
		_ = b.saveLocked()
	}
	b.mu.Unlock()
}

func (b *Bridge) clearFinal(id string) {
	b.mu.Lock()
	if b.state.PendingFinal == id {
		b.state.PendingFinal = ""
		_ = b.saveLocked()
	}
	b.mu.Unlock()
}

func (b *Bridge) mirrorItem(ctx context.Context, p *protocol, item api.Item, kind string) {
	critical := kind == "final" || kind == "question"
	parent := outputKey(item)
	body := item.Text
	if item.Kind == "user" {
		body = "User: " + body
	}
	parts := chunks(body)
	b.mu.Lock()
	if _, seen := b.state.Outputs[parent]; seen {
		b.mu.Unlock()
		return
	}
	_, compactSeen := b.state.Outputs[parent+":compact"]
	started := compactSeen
	for index := range parts {
		if _, seen := b.state.Outputs[fmt.Sprintf("%s:%d", parent, index)]; seen {
			started = true
			break
		}
	}
	remaining := b.remainingLocked(critical)
	b.mu.Unlock()
	if compactSeen {
		b.clearFinal(item.ID)
		return // accepted, rejected and unknown compact attempts are all terminal
	}
	if !started && len(parts) > remaining {
		if kind == "final" && remaining > 0 && len(parts) > 1 {
			_, state := b.sendOne(ctx, p, parent+":compact", compactFinal(body), true)
			if state == "deferred" || state == "unavailable" {
				b.deferFinal(item.ID)
			} else {
				b.clearFinal(item.ID)
			}
			return
		}
		if kind == "final" {
			b.deferFinal(item.ID)
		} else if kind == "user" {
			b.skipItem(parent, true)
		}
		return
	}
	for index, part := range parts {
		_, state := b.sendOne(ctx, p, fmt.Sprintf("%s:%d", parent, index), part, critical)
		switch state {
		case "accepted":
			continue
		case "deferred", "unavailable":
			if kind == "final" {
				b.deferFinal(item.ID)
			}
		default:
			b.clearFinal(item.ID) // unknown and rejected are never replayed
		}
		return
	}
	b.clearFinal(item.ID)
}

func (b *Bridge) mirrorMissed(ctx context.Context, p *protocol) {
	b.mu.Lock()
	count, input := b.state.Missed, b.state.Window.InputID
	room := b.remainingLocked(false)
	b.mu.Unlock()
	if count == 0 || input == "" || room < 2 {
		return
	}
	text := fmt.Sprintf("此前有 %d 条低优先级更新未能在微信推送。它们仍在同一 Bot 对话中；需要详情可直接询问 Bot。", count)
	_, state := b.sendOne(ctx, p, "missed:"+input, text, false)
	if state == "accepted" || state == "rejected" || state == "unknown" {
		b.mu.Lock()
		if b.state.Missed >= count {
			b.state.Missed -= count
			_ = b.saveLocked()
		}
		b.mu.Unlock()
	}
}
