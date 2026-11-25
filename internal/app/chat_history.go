package app

import "path/filepath"

func (a *Application) chatHistoryDirectory() string {
	return filepath.Join(a.root, "ChatHistory")
}

func (a *Application) introductionRequest() string {
	if a.initialization == nil {
		return ""
	}
	return a.initialization.Initialization().RequestID
}
