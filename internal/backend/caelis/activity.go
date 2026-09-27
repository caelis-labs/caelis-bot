package caelis

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func applyToolActivity(v *view, e wire.Envelope) {
	var call wire.ACPToolCallUpdate
	if json.Unmarshal(value(e.Update), &call) != nil || call.ToolCallId == "" || (call.SessionUpdate != "tool_call" && call.SessionUpdate != "tool_call_update") {
		return
	}
	id := value(e.TurnId) + "/activity/" + call.ToolCallId
	final := value(e.Final) || value(call.Status) == "completed" || value(call.Status) == "failed"
	if e.ParentTool != nil {
		if !final || value(e.ParentTool.ToolCallId) != call.ToolCallId {
			return
		}
		// Terminal task frames can use a different turn identity. Only close
		// an existing invocation; never create work from child observations.
		for i := len(v.Items) - 1; i >= 0; i-- {
			item := &v.Items[i]
			if item.Kind == "activity" && strings.HasSuffix(item.ID, "/activity/"+call.ToolCallId) && item.Status == "inProgress" {
				item.Status = "completed"
				return
			}
		}
		return
	}
	index := -1
	for i := range v.Items {
		if v.Items[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		if call.SessionUpdate != "tool_call" && !final {
			return
		}
		v.Items = append(v.Items, api.Item{ID: id, TurnKey: value(e.TurnId), Kind: "activity", Status: "inProgress"})
		index = len(v.Items) - 1
	}
	item := &v.Items[index]
	if item.Status == "completed" || item.Status == "failed" {
		return
	}
	if call.Kind != nil || call.Name != nil || call.Locations != nil || item.Activity == nil {
		a := &api.Activity{Kind: "tool", Target: value(call.Name)}
		if item.Activity != nil {
			*a = *item.Activity
		}
		if a.Kind == "tool" && call.Name != nil {
			a.Target = value(call.Name)
		}
		switch value(call.Kind) {
		case "read", "edit", "search", "fetch", "execute":
			if a.Kind != value(call.Kind) {
				a.Target = ""
			}
			a.Kind = value(call.Kind)
		case "delete", "move":
			a.Kind = "edit"
			a.Target = ""
		case "think":
			a.Kind = "plan"
			a.Target = ""
		}
		switch value(call.Name) {
		case "WebSearch":
			a.Kind, a.Target = "web", ""
		case "WebFetch":
			a.Kind, a.Target = "fetch", ""
		case "Spawn", "WaitThread":
			a.Kind, a.Target = "delegate", ""
		}
		if (a.Kind == "read" || a.Kind == "edit") && call.Locations != nil {
			a.Target = ""
			if len(call.Locations) == 1 {
				a.Target = filepath.Base(call.Locations[0].Path)
			}
		}
		item.Activity = a
	}
	if final {
		item.Status = "completed"
	}
	if value(call.Status) == "failed" {
		item.Status = "failed"
	}
}
