package chatlog

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// RepairAssistantLinks restores Markdown from an exact native replay. Earlier
// Bot builds stored only a local link's label. Keep the original IM row, time,
// and receipts; a recent display read cannot add a historical reply.
func (l *Log) RepairAssistantLinks(repairs []api.Item) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.chatLinkRepairs = make(map[string]api.Item, len(repairs))
	for _, repair := range repairs {
		l.chatLinkRepairs[repair.ID] = copyItem(repair)
	}
	l.repairAssistantLinksLocked()
}

func (l *Log) repairAssistantLinksLocked() {
	if len(l.chatLinkRepairs) == 0 {
		return
	}
	for n := range l.items {
		item := &l.items[n]
		repair, ok := l.chatLinkRepairs[item.ID]
		if !ok || item.Kind != "assistant" || item.TurnKey != repair.TurnKey ||
			item.Status != "completed" || item.ChatText != "" || (item.Text != repair.Text && item.Text != repair.ChatText) {
			continue
		}
		item.ChatText, item.Links = repair.ChatText, repair.Links
		l.dirty[key(*item)] = *item
		l.revision++
	}
}
