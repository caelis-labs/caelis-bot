package scheduledata

// Source describes a host-owned native observation, never a model-published event.
type Source struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Fields      map[string]string `json:"fields"`
}

func NativeSources() []Source {
	return []Source{
		{Name: "clock.minute", Description: "One current wall-clock minute while the app runs; no missed-minute replay. Use local fields for the rule timezone.", Fields: map[string]string{}},
		{Name: "desktop.usage", Description: "Sampled about every 30 seconds with known unlocked presence. Active time resets after a five-minute break, lock, sleep or app restart.", Fields: map[string]string{"activeSeconds": "integer", "idleSeconds": "integer", "application": "bundle identifier string"}},
		{Name: "desktop.appChanged", Description: "A change between sampled foreground application bundle identifiers, with known unlocked presence.", Fields: map[string]string{"application": "bundle identifier string", "previousApplication": "bundle identifier string", "activeSeconds": "integer", "idleSeconds": "integer"}},
	}
}
func KnownSource(name string) bool {
	for _, s := range NativeSources() {
		if s.Name == name {
			return true
		}
	}
	return false
}
