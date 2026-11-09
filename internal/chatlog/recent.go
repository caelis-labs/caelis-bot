package chatlog

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// RecentSnapshot selects the same latest user/assistant preview as the full IM
// snapshot before copying nested presentation fields. It never reads the DB.
func (l *Log) RecentSnapshot(turn string) ([]api.Item, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var user, assistant *api.Item
	for i := range l.items {
		item := &l.items[i]
		if _, waiting := l.pendingInputs[key(*item)]; waiting {
			continue
		}
		if turn != "" && item.TurnKey != turn {
			continue
		}
		switch item.Kind {
		case "user":
			user, assistant = item, nil
		case "assistant":
			assistant = item
		}
	}
	items := make([]api.Item, 0, 2)
	if user != nil {
		items = append(items, copyItem(*user))
	}
	if assistant != nil {
		items = append(items, copyItem(*assistant))
	}
	return items, l.earlier
}
