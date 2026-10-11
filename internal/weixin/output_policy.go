package weixin

import (
	"fmt"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// A completed assistant item can be commentary inside an unfinished Turn.
// Only the last ordinary assistant item of a terminal Turn is its reply. Native
// async question cards and Worker-start chips are separately visible events.
func (b *Bridge) outputKinds(snapshot api.Snapshot) map[string]string {
	kinds := map[string]string{}
	last := map[string]string{}
	b.mu.Lock()
	terminal := make(map[string]string, len(b.state.TurnPhases))
	for key, phase := range b.state.TurnPhases {
		terminal[key] = phase
	}
	b.mu.Unlock()
	for _, item := range snapshot.Items {
		if item.Kind != "assistant" || item.ID == "" || item.Text == "" || item.Text == api.SilentReminder || item.Status != "completed" && item.Status != "accepted" {
			continue
		}
		if b.host.TextControl != nil && len(b.host.TextControl.AsyncButtonQuestions(item.ID)) > 0 {
			kinds[item.ID] = "question"
			continue
		}
		if item.Task != nil {
			kinds[item.ID] = "task"
			continue
		}
		if item.TurnKey != "" {
			if terminal[item.TurnKey] != "completed" {
				continue
			}
		} else if snapshot.CurrentTurn != "" {
			continue // unscoped text cannot prove a Turn completed
		}
		last[item.TurnKey] = item.ID
	}
	for _, id := range last {
		kinds[id] = "final"
	}
	return kinds
}

// This is a deterministic excerpt of the original text, never a model summary.
// It is used only when the remaining estimated window cannot fit every normal
// Markdown-aware chunk. The full original remains in the shared local chat.
func compactFinal(body string) string {
	runes := []rune(body)
	for keep := len(runes); keep > 0; keep -= 32 {
		if keep > 1200 {
			keep = 1200
		}
		if keep >= len(runes) && fitsText(body) {
			return body
		}
		head := keep * 7 / 10
		tail := keep - head
		if head+tail >= len(runes) {
			return body
		}
		text := fmt.Sprintf("%s\n\n[... 中间省略 %d 字符 ...]\n\n%s\n\n本条为原文节选；如需其余内容，请回复 Bot 继续发送。", string(runes[:head]), len(runes)-keep, string(runes[len(runes)-tail:]))
		if fitsText(text) {
			return text
		}
	}
	return fmt.Sprintf("%s\n\n[... 中间省略 %d 字符 ...]\n\n如需其余内容，请回复 Bot 继续发送。", string(runes[:1]), len(runes)-1)
}
