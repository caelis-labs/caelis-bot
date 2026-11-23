package app

import "path/filepath"

func (a *Application) chatHistoryPath() string {
	return filepath.Join(a.root, "ChatHistory", "conversation.jsonl")
}

func (a *Application) introductionRequest() string {
	if a.initialization == nil {
		return ""
	}
	return a.initialization.Initialization().RequestID
}
