package api

// RecentItems selects the latest input boundary without copying history.
// Callers must clone the result before exposing it or changing nested fields.
// Steering retains earlier activity in the same turn for the pet's work state.
func RecentItems(items []Item, currentTurn string) []Item {
	start := 0
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Kind == "user" || items[i].Kind == "activation" {
			start = i
			break
		}
	}
	if currentTurn != "" {
		for i := 0; i < start; i++ {
			if items[i].TurnKey == currentTurn {
				start = i
				break
			}
		}
	}
	return items[start:]
}
