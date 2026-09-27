package codex

import (
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type commandAction struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

func itemActivity(item nativeItem) *api.Activity {
	a := &api.Activity{}
	switch item.Type {
	case "commandExecution":
		a.Kind = "execute"
		// Use App Server's parser, not heuristics over shell text. A mixed or
		// unknown command stays a command, even if it also reads a file.
		if len(item.CommandActions) > 0 {
			kind := item.CommandActions[0].Type
			for _, action := range item.CommandActions {
				if action.Type != kind {
					return a
				}
			}
			switch kind {
			case "read":
				a.Kind = "read"
			case "listFiles":
				a.Kind = "list"
			case "search":
				a.Kind = "search"
			}
			if a.Kind != "execute" && len(item.CommandActions) == 1 && item.CommandActions[0].Path != "" {
				a.Target = filepath.Base(item.CommandActions[0].Path)
			}
		}
	case "fileChange":
		a.Kind = "edit"
		if len(item.Changes) == 1 {
			a.Target = filepath.Base(item.Changes[0].Path)
		}
	case "webSearch":
		a.Kind = "web"
		if item.WebAction != nil && (item.WebAction.Type == "openPage" || item.WebAction.Type == "findInPage") {
			a.Kind = "fetch"
		}
	case "mcpToolCall", "dynamicToolCall":
		a.Kind, a.Target = "tool", item.Tool
	case "collabAgentToolCall":
		a.Kind = "delegate"
	case "imageView", "imageGeneration":
		a.Kind = "image"
	case "contextCompaction":
		a.Kind = "compact"
	case "plan":
		a.Kind = "plan"
	default:
		return nil // Child observations and unknown items do not own foreground work.
	}
	return a
}
