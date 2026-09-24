package bot

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func stamp(t *testing.T, s string) time.Time {
	t.Helper()
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestCalendarSlots(t *testing.T) {
	cases := []struct {
		name        string
		s           Schedule
		after, want string
	}{
		{"before opening", Schedule{EveryMinutes: 60, WindowStart: "09:00", WindowEnd: "18:00", TimeZone: "Asia/Shanghai"}, "2026-09-24T08:59:00+08:00", "2026-09-24T09:00:00+08:00"},
		{"aligned not creation time", Schedule{EveryMinutes: 60, WindowStart: "09:00", WindowEnd: "18:00", TimeZone: "Asia/Shanghai"}, "2026-09-24T09:23:00+08:00", "2026-09-24T10:00:00+08:00"},
		{"exclusive end", Schedule{EveryMinutes: 60, WindowStart: "09:00", WindowEnd: "18:00", TimeZone: "Asia/Shanghai"}, "2026-09-24T17:00:00+08:00", "2026-09-25T09:00:00+08:00"},
		{"literal weekdays", Schedule{EveryMinutes: 60, WindowStart: "09:00", WindowEnd: "18:00", Weekdays: []int{1, 2, 3, 4, 5}, TimeZone: "Asia/Shanghai"}, "2026-09-25T17:30:00+08:00", "2026-09-28T09:00:00+08:00"},
		{"overnight belongs to Friday", Schedule{EveryMinutes: 60, WindowStart: "22:00", WindowEnd: "02:00", Weekdays: []int{5}, TimeZone: "Asia/Shanghai"}, "2026-09-25T23:00:00+08:00", "2026-09-26T00:00:00+08:00"},
		{"overnight expired", Schedule{EveryMinutes: 60, WindowStart: "22:00", WindowEnd: "02:00", Weekdays: []int{5}, TimeZone: "Asia/Shanghai"}, "2026-09-26T01:00:00+08:00", "2026-10-02T22:00:00+08:00"},
		{"multiple times", Schedule{Times: []string{"09:00", "12:30", "18:00"}, TimeZone: "Asia/Shanghai"}, "2026-09-24T12:30:00+08:00", "2026-09-24T18:00:00+08:00"},
		{"DST missing slot", Schedule{EveryMinutes: 60, WindowStart: "01:00", WindowEnd: "04:00", TimeZone: "America/New_York"}, "2026-03-08T01:00:00-05:00", "2026-03-08T03:00:00-04:00"},
		{"DST daily skips", Schedule{Daily: "02:30", TimeZone: "America/New_York"}, "2026-03-08T00:00:00-05:00", "2026-03-09T02:30:00-04:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if e := validateSchedule(&c.s); e != nil {
				t.Fatal(e)
			}
			v, e := nextTime(c.s, stamp(t, c.after))
			if e != nil || !v.Equal(stamp(t, c.want)) {
				t.Fatalf("got %v %v, want %s", v, e, c.want)
			}
		})
	}
}
func TestWindowsFilterSleepAndBusyQueue(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "sleep", true: "busy"}[queued], func(t *testing.T) {
			r, f, now := fixture(t)
			*now = stamp(t, "2026-09-24T16:59:00+08:00")
			_, e := r.Upsert(Schedule{ID: "water", Label: "water", Prompt: "synthetic", EveryMinutes: 60, WindowStart: "09:00", WindowEnd: "18:00", TimeZone: "Asia/Shanghai"})
			if e != nil {
				t.Fatal(e)
			}
			if queued {
				*now = stamp(t, "2026-09-24T17:00:00+08:00")
				f.view.CanSend = false
				if e = r.Tick(context.Background()); e != nil {
					t.Fatal(e)
				}
				if r.State().Wake == nil {
					t.Fatal("not queued")
				}
			}
			*now = stamp(t, "2026-09-24T19:00:00+08:00")
			f.view.CanSend = true
			if e = r.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if len(f.submissions) != 0 || r.State().Wake != nil {
				t.Fatal("expired reminder dispatched")
			}
			if e = r.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if !r.State().Schedules[0].Next.Equal(stamp(t, "2026-09-25T09:00:00+08:00")) {
				t.Fatal(r.State().Schedules[0].Next)
			}
			*now = stamp(t, "2026-09-25T09:00:00+08:00")
			if e = r.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if len(f.submissions) != 1 || !f.submissions[0].Scheduled {
				t.Fatal("next valid occurrence lost its provenance")
			}
		})
	}
}
func TestScheduleValidationAndCanonicalRetry(t *testing.T) {
	r, _, _ := fixture(t)
	s := Schedule{ID: "slots", Label: "Slots", Prompt: "test", Times: []string{"18:00", "09:00"}, Weekdays: []int{5, 1}, TimeZone: "Asia/Shanghai"}
	first, e := r.Upsert(s)
	if e != nil {
		t.Fatal(e)
	}
	s.Times = []string{"09:00", "18:00"}
	s.Weekdays = []int{1, 5}
	retry, e := r.Upsert(s)
	if e != nil || !reflect.DeepEqual(first, retry) {
		t.Fatal("retry moved schedule", e)
	}
	for _, bad := range []Schedule{
		{EveryMinutes: 60, WindowStart: "09:00", WindowEnd: "18:00"},
		{EveryMinutes: 60, WindowStart: "09:00", TimeZone: "Asia/Shanghai"},
		{Daily: "09:00", Times: []string{"10:00"}, TimeZone: "UTC"},
		{EveryMinutes: 60, Weekdays: []int{0}, TimeZone: "UTC"},
		{Daily: "09:00", Weekdays: []int{1, 1}, TimeZone: "UTC"},
		{Times: []string{"09:00", "09:00"}, TimeZone: "UTC"},
		{EveryMinutes: 60, WindowStart: "09:00", WindowEnd: "09:00", TimeZone: "UTC"},
		{At: "2030-01-01T00:00:00Z", Weekdays: []int{1}, TimeZone: "UTC"},
	} {
		if validateSchedule(&bad) == nil {
			t.Fatalf("accepted %#v", bad)
		}
	}
}
