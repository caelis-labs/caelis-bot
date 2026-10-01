package codex

// Darwin reparents descendants to launchd; the native root is still drained
// through its original Cmd before the watchdog confirms a stop.
func prepareWatchdogReaping() error             { return nil }
func (*ownedTools) reapWatchdogChildren() error { return nil }
