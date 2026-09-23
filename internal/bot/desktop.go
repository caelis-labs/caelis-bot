package bot

import (
	"encoding/json"
	"errors"
)

// ExecuteDesktop reuses the native action and reminder persistence owner. The
// remote provider owns grants and fires; this does not start Runtime.Tick.
func (r *Runtime) ExecuteDesktop(action string, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Operation, ID, Label, Prompt, At, Daily, Gesture string
		EveryMinutes                                     int    `json:"every_minutes"`
		TimeZone                                         string `json:"time_zone"`
	}
	if json.Unmarshal(args, &a) != nil {
		return nil, errors.New("无效桌面参数")
	}
	var out any
	var err error
	switch action {
	case "clock":
		out = r.Clock()
	case "gesture":
		err = r.Perform(a.Gesture)
		out = map[string]bool{"ok": err == nil}
	case "reminders":
		switch a.Operation {
		case "list":
			out = r.State().Schedules
		case "save":
			out, err = r.Upsert(Schedule{ID: a.ID, Label: a.Label, Prompt: a.Prompt, At: a.At, EveryMinutes: a.EveryMinutes, Daily: a.Daily, TimeZone: a.TimeZone})
		case "remove":
			err = r.Remove(a.ID)
			out = map[string]bool{"removed": err == nil}
		default:
			err = errors.New("不支持的提醒动作")
		}
	default:
		err = errors.New("不支持的桌面动作")
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
