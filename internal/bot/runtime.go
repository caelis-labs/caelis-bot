// Package bot owns durable identity and finite wakeups, independent of a backend
// conversation, workspace, character asset or renderer.
package bot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type Schedule struct {
	Runtime      string    `json:"runtime,omitempty"`
	ID           string    `json:"id"`
	Label        string    `json:"label"`
	Prompt       string    `json:"prompt"`
	At           string    `json:"at,omitempty"`
	EveryMinutes int       `json:"everyMinutes,omitempty"`
	Daily        string    `json:"daily,omitempty"`
	TimeZone     string    `json:"timeZone,omitempty"`
	Next         time.Time `json:"next"`
	Enabled      bool      `json:"enabled"`
}
type Wake struct {
	Runtime     string   `json:"runtime,omitempty"`
	ID          string   `json:"id"`
	Prompt      string   `json:"prompt"`
	Messages    []string `json:"messages"`
	ScheduleIDs []string `json:"scheduleIds"`
	Status      string   `json:"status"` // pending, dispatching, accepted, unknown
}
type State struct {
	Version         int        `json:"version"`
	PersonalVersion int        `json:"personalVersion,omitempty"`
	ID              string     `json:"id"`
	Schedules       []Schedule `json:"schedules"`
	Wake            *Wake      `json:"wake,omitempty"`
}
type Engine interface {
	Snapshot() api.Snapshot
	Submit(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)
}
type Runtime struct {
	stopped        bool
	mu             sync.Mutex
	step           sync.Mutex
	path           string
	state          State
	now            func() time.Time
	engine         Engine
	provider       string
	personal       api.PersonalTools
	initialization *Initializer
	tasks          api.TaskProvider
	reports        api.TaskReporter
	action         func(string) error
	done           chan struct{}
	cancel         context.CancelFunc
	notify         func(id, title string)
}

func (r *Runtime) SetReminderNotifier(f func(id, title string)) {
	r.mu.Lock()
	r.notify = f
	r.mu.Unlock()
}

func New(path string, action func(string) error) (*Runtime, error) {
	return NewForRuntime(path, "codex", action)
}

// NewForRuntime shares Bot identity while retaining each schedule execution target.
func NewForRuntime(path, provider string, action func(string) error) (*Runtime, error) {
	if provider == "" {
		return nil, errors.New("提醒需要明确的运行时")
	}
	r := &Runtime{path: path, provider: provider, now: time.Now, action: action, state: State{Version: 1, PersonalVersion: 1, ID: rand.Text(), Schedules: []Schedule{}}}
	b, e := os.ReadFile(path)
	if e == nil {
		r.state = State{} // Existing files must supply identity; never fill missing fields with new defaults.
		if json.Unmarshal(b, &r.state) != nil || r.state.Version != 1 || r.state.ID == "" || r.state.PersonalVersion > 1 || r.state.PersonalVersion < 0 {
			return nil, errors.New("Bot 记录无法读取，请保留文件并重试")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	// A process exit pauses missed activation. Repeating schedules resume at the
	// next future slot; due one-offs missed while quit are disabled, never replayed.
	now := r.now()
	for i, s := range r.state.Schedules {
		if s.Runtime == "" {
			s.Runtime = "codex"
			r.state.Schedules[i] = s
		}
		if s.Runtime != provider {
			continue
		}
		if !s.Next.After(now) {
			next, err := nextTime(s, now)
			if err != nil {
				return nil, err
			}
			s.Next = next
			s.Enabled = s.Enabled && !next.IsZero()
			r.state.Schedules[i] = s
		}
	}
	if r.state.Wake != nil && r.state.Wake.Runtime == "" {
		r.state.Wake.Runtime = "codex"
	}
	if r.state.Wake != nil && r.state.Wake.Status == "dispatching" {
		r.state.Wake.Status = "unknown"
	}
	if e := r.saveLocked(); e != nil {
		return nil, e
	}
	return r, nil
}
func (r *Runtime) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, _ := json.Marshal(r.state)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}
func (r *Runtime) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(r.path), ".bot-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	c := f.Close()
	if err == nil {
		err = c
	}
	if err == nil {
		err = os.Rename(f.Name(), r.path)
	}
	return err
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func nextTime(s Schedule, after time.Time) (time.Time, error) {
	if s.EveryMinutes > 0 {
		interval := time.Duration(s.EveryMinutes) * time.Minute
		if s.Next.After(after) {
			return s.Next, nil
		}
		if s.Next.IsZero() {
			return after.Add(interval), nil
		}
		return s.Next.Add((after.Sub(s.Next)/interval + 1) * interval), nil
	}
	if s.Daily != "" {
		loc, e := time.LoadLocation(s.TimeZone)
		if e != nil {
			return time.Time{}, errors.New("需要有效的 IANA 时区")
		}
		v, e := time.Parse("15:04", s.Daily)
		if e != nil {
			return time.Time{}, errors.New("每天时间必须为 HH:MM")
		}
		local := after.In(loc)
		for day := 0; day < 4; day++ {
			d := local.AddDate(0, 0, day)
			candidate := time.Date(d.Year(), d.Month(), d.Day(), v.Hour(), v.Minute(), 0, 0, loc)
			if candidate.Hour() != v.Hour() || candidate.Minute() != v.Minute() {
				continue
			}
			if candidate.After(after) {
				return candidate, nil
			}
		}
		return time.Time{}, errors.New("无法确定下次提醒时间")
	}
	t, e := time.Parse(time.RFC3339, s.At)
	if e != nil {
		return time.Time{}, errors.New("需要带时区的 RFC3339 时间")
	}
	if !t.After(after) {
		return time.Time{}, nil
	}
	return t, nil
}
func (r *Runtime) Upsert(s Schedule) (Schedule, error) {
	s.Runtime = r.provider
	if !identifier.MatchString(s.ID) || strings.TrimSpace(s.Label) == "" || len(s.Label) > 200 || strings.TrimSpace(s.Prompt) == "" || len(s.Prompt) > 16000 {
		return s, errors.New("提醒需要有效标识、标题和内容")
	}
	modes := 0
	if s.At != "" {
		modes++
	}
	if s.EveryMinutes != 0 {
		modes++
	}
	if s.Daily != "" {
		modes++
	}
	if modes != 1 || s.EveryMinutes < 0 || s.EveryMinutes > 10080 {
		return s, errors.New("请选择单次时间、1–10080 分钟间隔或每天时间")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index := -1
	for i, old := range r.state.Schedules {
		if old.ID == s.ID {
			if old.Runtime != r.provider {
				return s, errors.New("该提醒属于其他运行时，请切换后管理")
			}
			index = i
			old.Next = time.Time{}
			old.Enabled = false
			copy := s
			copy.Next = time.Time{}
			copy.Enabled = false
			if old == copy {
				return r.state.Schedules[i], nil
			}
			break
		}
	}
	if index < 0 && len(r.state.Schedules) >= 32 {
		return s, errors.New("最多保存 32 项提醒")
	}
	s.Next = time.Time{}
	next, e := nextTime(s, r.now())
	if e != nil {
		return s, e
	}
	if next.IsZero() {
		return s, errors.New("提醒时间必须在未来")
	}
	s.Next = next
	s.Enabled = true
	previous := append([]Schedule(nil), r.state.Schedules...)
	if index < 0 {
		r.state.Schedules = append(r.state.Schedules, s)
	} else {
		r.state.Schedules[index] = s
	}
	if e := r.saveLocked(); e != nil {
		r.state.Schedules = previous
		return s, e
	}
	return s, nil
}
func (r *Runtime) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.state.Schedules {
		if s.ID == id && s.Runtime != r.provider {
			return errors.New("该提醒属于其他运行时，请切换后管理")
		}
	}
	old := r.state.Schedules
	r.state.Schedules = nil
	for _, s := range old {
		if s.ID != id {
			r.state.Schedules = append(r.state.Schedules, s)
		}
	}
	// A queued batch is rebuilt without a removed reminder. An already dispatched
	// action cannot be silently recalled; user can use explicit Stop in the UI.
	previousWake := r.state.Wake
	if w := r.state.Wake; w != nil && (w.Status == "pending" || w.Status == "unknown") {
		next := *w
		next.ScheduleIDs = nil
		next.Messages = nil
		for i, v := range w.ScheduleIDs {
			if v != id {
				next.ScheduleIDs = append(next.ScheduleIDs, v)
				if i < len(w.Messages) {
					next.Messages = append(next.Messages, w.Messages[i])
				}
			}
		}
		if len(next.ScheduleIDs) == 0 {
			r.state.Wake = nil
		} else {
			next.Prompt = wakePrompt(next.Messages)
			r.state.Wake = &next
		}
	}
	if e := r.saveLocked(); e != nil {
		r.state.Schedules = old
		r.state.Wake = previousWake
		return e
	}
	return nil
}

// ConfigureTasks binds the application coordinator, never a provider-specific product.
func (r *Runtime) ConfigureTasks(provider api.TaskProvider, reports api.TaskReporter) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil || r.stopped {
		return errors.New("任务宿主须在启动前绑定")
	}
	r.tasks = provider
	r.reports = reports
	return nil
}
func (r *Runtime) Start(engine Engine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil || r.stopped {
		return
	}
	r.engine = engine
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_ = r.Tick(ctx)
			}
		}
	}()
}
func (r *Runtime) Stop() {
	r.mu.Lock()
	r.stopped = true
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (r *Runtime) Close() {
	r.Stop()
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Tick uses wall time after wake. It never invokes a model while idle, never
// steers an unrelated active request, and persists an occurrence before dispatch.
func (r *Runtime) Tick(ctx context.Context) error {
	r.step.Lock()
	defer r.step.Unlock()
	var noticeID, noticeTitle string
	var notify func(string, string)
	defer func() {
		if noticeID != "" && notify != nil {
			notify(noticeID, noticeTitle)
		}
	}()
	if r.engine == nil {
		return nil
	}
	if r.initialization != nil {
		if err := r.initialization.Deliver(ctx, r.engine, r.provider); err != nil {
			return err
		}
		if r.initialization.Initialization().Status != "accepted" {
			return nil
		}
	}
	if r.reports != nil {
		if err := r.reports.DeliverTaskReport(ctx); err != nil {
			return err
		}
	}
	r.mu.Lock()
	notify = r.notify
	now := r.now()
	if r.state.Wake != nil && r.state.Wake.Runtime != r.provider && r.state.Wake.Status != "accepted" {
		r.mu.Unlock()
		return nil
	}
	previous := r.state
	previous.Schedules = append([]Schedule(nil), r.state.Schedules...)
	if w := r.state.Wake; w != nil && (w.Status == "unknown" || w.Status == "dispatching") {
		receipt := r.engine.Snapshot().LastReceipt
		if receipt.ID == w.ID && receipt.Outcome == "accepted" {
			w.Status = "accepted"
			e := r.saveLocked()
			r.mu.Unlock()
			return e
		}
		r.mu.Unlock()
		return nil
	}
	if r.state.Wake == nil || r.state.Wake.Status == "accepted" {
		var ids, texts, labels []string
		for i, s := range r.state.Schedules {
			if s.Runtime != r.provider || !s.Enabled || s.Next.After(now) {
				continue
			}
			if len(strings.Join(texts, "\n"))+len(s.Prompt) > 96000 {
				break
			}
			ids = append(ids, s.ID)
			texts = append(texts, s.Label+"："+s.Prompt)
			labels = append(labels, s.Label)
			next, e := nextTime(s, now)
			if e != nil {
				r.mu.Unlock()
				return e
			}
			s.Next = next
			s.Enabled = !next.IsZero()
			r.state.Schedules[i] = s
		}
		if len(ids) > 0 {
			r.state.Wake = &Wake{Runtime: r.provider, ID: "wake-" + rand.Text(), Prompt: wakePrompt(texts), Messages: texts, ScheduleIDs: ids, Status: "pending"}
			if e := r.saveLocked(); e != nil {
				r.state = previous
				r.mu.Unlock()
				return e
			}
			noticeID, noticeTitle = r.state.Wake.ID, strings.Join(labels, "、")
		}
	}
	if r.state.Wake == nil || r.state.Wake.Status != "pending" {
		r.mu.Unlock()
		return nil
	}
	if !r.engine.Snapshot().CanSend {
		r.mu.Unlock()
		return nil
	}
	wake := *r.state.Wake
	r.state.Wake.Status = "dispatching"
	if e := r.saveLocked(); e != nil {
		r.state.Wake.Status = "pending"
		r.mu.Unlock()
		return e
	}
	r.mu.Unlock()
	receipt, e := r.engine.Submit(ctx, api.Submission{ID: wake.ID, Text: wake.Prompt}, nil)
	r.mu.Lock()
	defer r.mu.Unlock()
	if e != nil || receipt.Outcome == "unknown" {
		r.state.Wake.Status = "unknown"
	} else if receipt.Outcome == "accepted" {
		r.state.Wake.Status = "accepted"
	} else {
		r.state.Wake.Status = "pending"
	}
	if save := r.saveLocked(); save != nil {
		return save
	}
	return e
}
func (r *Runtime) Perform(action string) error {
	switch action {
	case "attention", "nod", "celebrate":
	default:
		return errors.New("不支持的角色动作")
	}
	if r.action == nil {
		return errors.New("角色暂不可用")
	}
	return r.action(action)
}
func (r *Runtime) Clock() map[string]string {
	now := r.now()
	return map[string]string{"now": now.Format(time.RFC3339), "timeZone": now.Location().String(), "scheduling": "应用常驻时触发；睡眠期间合并；退出后暂停"}
}

func wakePrompt(messages []string) string {
	return "定时提醒（错过的同类提醒已合并）：\n" + strings.Join(messages, "\n")
}
func (r *Runtime) Status() string {
	if r.initialization != nil && r.initialization.Initialization().Status != "accepted" {
		return r.initialization.Initialization().Message
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Wake != nil && r.state.Wake.Runtime != r.provider && r.state.Wake.Status != "accepted" {
		return "提醒正在等待其所属运行时连接，不会改用当前运行时。"
	}
	if r.state.Wake != nil && r.state.Wake.Status == "unknown" {
		return "有一次定时任务的发送结果待确认。请重新连接核对；不会自动重复执行。"
	}
	if r.state.Wake != nil && r.state.Wake.Status == "pending" {
		return "提醒已到时间，正在等待连接或当前工作结束。"
	}
	return ""
}
