package bot

import (
	"errors"
	"slices"
	"time"
)

// Windows are [start, end) in the selected zone. Overnight windows belong to
// their start date; weekday 1 is Monday and 7 is Sunday. No holiday calendar.
func clockMinute(raw string, end bool) (int, error) {
	if end && raw == "24:00" {
		return 1440, nil
	}
	t, err := time.Parse("15:04", raw)
	if err != nil || t.Format("15:04") != raw {
		return 0, errors.New("时间必须为 HH:MM")
	}
	return t.Hour()*60 + t.Minute(), nil
}
func validateSchedule(s *Schedule) error {
	modes := 0
	for _, yes := range []bool{s.At != "", s.EveryMinutes != 0, s.Daily != "", len(s.Times) > 0} {
		if yes {
			modes++
		}
	}
	if modes != 1 || s.EveryMinutes < 0 || s.EveryMinutes > 10080 {
		return errors.New("请选择单次时间、1–10080 分钟间隔、每天时间或多个每日时点")
	}
	calendar := s.Daily != "" || len(s.Times) > 0 || s.WindowStart != "" || s.WindowEnd != "" || len(s.Weekdays) > 0
	if s.At != "" && calendar {
		return errors.New("单次时间不能附加重复计划的时间窗口或星期条件")
	}
	if calendar || s.TimeZone != "" {
		if s.TimeZone == "" {
			return errors.New("日历计划需要明确的 IANA 时区")
		}
		if _, e := time.LoadLocation(s.TimeZone); e != nil {
			return errors.New("需要有效的 IANA 时区")
		}
	}
	if (s.WindowStart == "") != (s.WindowEnd == "") {
		return errors.New("请同时指定时间窗口的开始和结束")
	}
	if s.WindowStart != "" {
		a, e := clockMinute(s.WindowStart, false)
		if e != nil {
			return e
		}
		b, e := clockMinute(s.WindowEnd, true)
		if e != nil {
			return e
		}
		if a == b {
			return errors.New("时间窗口不能为空")
		}
	}
	if len(s.Weekdays) > 7 {
		return errors.New("星期必须为 1–7，周一为 1")
	}
	s.Weekdays = slices.Clone(s.Weekdays)
	slices.Sort(s.Weekdays)
	for i, d := range s.Weekdays {
		if d < 1 || d > 7 || (i > 0 && s.Weekdays[i-1] == d) {
			return errors.New("星期必须为不重复的 1–7，周一为 1")
		}
	}
	if s.Daily != "" {
		if _, e := clockMinute(s.Daily, false); e != nil {
			return e
		}
	}
	if len(s.Times) > 48 {
		return errors.New("每天最多指定 48 个时点")
	}
	s.Times = slices.Clone(s.Times)
	slices.Sort(s.Times)
	for i, t := range s.Times {
		if _, e := clockMinute(t, false); e != nil {
			return e
		}
		if i > 0 && s.Times[i-1] == t {
			return errors.New("每日时点不能重复")
		}
	}
	if s.At != "" {
		if _, e := time.Parse(time.RFC3339, s.At); e != nil {
			return errors.New("需要带时区的 RFC3339 时间")
		}
	}
	return nil
}
func weekdayAllowed(s Schedule, date time.Time) bool {
	d := int(date.Weekday())
	if d == 0 {
		d = 7
	}
	return len(s.Weekdays) == 0 || slices.Contains(s.Weekdays, d)
}
func window(s Schedule) (int, int) {
	if s.WindowStart == "" {
		return 0, 1440
	}
	a, _ := clockMinute(s.WindowStart, false)
	b, _ := clockMinute(s.WindowEnd, true)
	if b < a {
		b += 1440
	}
	return a, b
}
func scheduleAllowed(s Schedule, now time.Time) bool {
	if s.WindowStart == "" && len(s.Weekdays) == 0 {
		return true
	}
	loc, e := time.LoadLocation(s.TimeZone)
	if e != nil {
		return false
	}
	local := now.In(loc)
	minute := local.Hour()*60 + local.Minute()
	a, b := window(s)
	if b > 1440 && minute < b-1440 {
		local = local.AddDate(0, 0, -1)
		minute += 1440
	}
	return weekdayAllowed(s, local) && minute >= a && minute < b
}
func nextTime(s Schedule, after time.Time) (time.Time, error) {
	if s.EveryMinutes > 0 && s.WindowStart == "" && len(s.Weekdays) == 0 {
		interval := time.Duration(s.EveryMinutes) * time.Minute
		if s.Next.After(after) {
			return s.Next, nil
		}
		if s.Next.IsZero() {
			return after.Add(interval), nil
		}
		return s.Next.Add((after.Sub(s.Next)/interval + 1) * interval), nil
	}
	if s.At != "" {
		t, e := time.Parse(time.RFC3339, s.At)
		if e != nil {
			return time.Time{}, e
		}
		if !t.After(after) {
			return time.Time{}, nil
		}
		return t, nil
	}
	loc, e := time.LoadLocation(s.TimeZone)
	if e != nil {
		return time.Time{}, errors.New("需要有效的 IANA 时区")
	}
	local := after.In(loc)
	a, b := window(s)
	var minutes []int
	if s.EveryMinutes > 0 {
		for m := a; m < b; m += s.EveryMinutes {
			minutes = append(minutes, m)
		}
	} else {
		times := s.Times
		if s.Daily != "" {
			times = []string{s.Daily}
		}
		for _, raw := range times {
			m, e := clockMinute(raw, false)
			if e != nil {
				return time.Time{}, e
			}
			if b > 1440 && m < a {
				m += 1440
			}
			if m >= a && m < b {
				minutes = append(minutes, m)
			}
		}
		slices.Sort(minutes)
	}
	if len(minutes) == 0 {
		return time.Time{}, errors.New("时间窗口内没有可触发的时点")
	}
	for day := -1; day <= 8; day++ {
		date := time.Date(local.Year(), local.Month(), local.Day()+day, 12, 0, 0, 0, loc)
		if !weekdayAllowed(s, date) {
			continue
		}
		for _, m := range minutes {
			target := date.AddDate(0, 0, m/1440)
			minute := m % 1440
			t := time.Date(target.Year(), target.Month(), target.Day(), minute/60, minute%60, 0, 0, loc)
			// A nonexistent DST wall time is skipped, never shifted to another slot.
			if t.Day() != target.Day() || t.Hour() != minute/60 || t.Minute() != minute%60 {
				continue
			}
			if t.After(after) {
				return t, nil
			}
		}
	}
	return time.Time{}, errors.New("无法确定下次提醒时间")
}

// A queued occurrence may expire while busy or disconnected. Re-check its
// window before dispatch; unknown/accepted operations are never rewritten.
func (r *Runtime) prunePendingLocked(now time.Time) {
	w := r.state.Wake
	if w == nil || w.Status != "pending" {
		return
	}
	next := *w
	next.ScheduleIDs = nil
	next.Messages = nil
	for i, id := range w.ScheduleIDs {
		for _, s := range r.state.Schedules {
			if s.ID == id && s.Runtime == r.provider && scheduleAllowed(s, now) {
				next.ScheduleIDs = append(next.ScheduleIDs, id)
				if i < len(w.Messages) {
					next.Messages = append(next.Messages, w.Messages[i])
				}
				break
			}
		}
	}
	if len(next.ScheduleIDs) == 0 {
		r.state.Wake = nil
		return
	}
	if len(next.ScheduleIDs) != len(w.ScheduleIDs) {
		next.Prompt = wakePrompt(next.Messages)
		r.state.Wake = &next
	}
}

// reminderCalendarSchema is shared by the local MCP contract. Calendar fields
// describe weekdays literally; legal workdays remain a prompt condition.
func reminderCalendarSchema() map[string]any {
	str := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	return map[string]any{
		"times":       map[string]any{"type": "array", "items": str("Daily local HH:MM"), "maxItems": 48, "uniqueItems": true, "description": "Multiple daily times; choose instead of at, daily or everyMinutes"},
		"weekdays":    map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1, "maximum": 7}, "maxItems": 7, "uniqueItems": true, "description": "Optional literal weekdays: 1 Monday through 7 Sunday; not a holiday calendar"},
		"windowStart": str("Optional inclusive local HH:MM; requires windowEnd and timeZone. Interval slots start here each selected day"),
		"windowEnd":   str("Exclusive local HH:MM, or 24:00; earlier than start means overnight. For 09:00 through 18:00 inclusive use end 19:00 with a 60-minute interval"),
	}
}

func reminderFields(fields map[string]any) map[string]any {
	for key, v := range reminderCalendarSchema() {
		fields[key] = v
	}
	return fields
}
