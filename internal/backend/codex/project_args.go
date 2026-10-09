package codex

func appServerArgs(_ Options, listen string) []string {
	return []string{"app-server", "--listen", listen}
}
