//go:build webfixture

// This deterministic adapter is acceptance evidence, not a real model/Runtime.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktop"
	"github.com/caelis-labs/caelis-bot/internal/websurface"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type engine struct {
	mu    sync.Mutex
	state api.Snapshot
	root  string
}

func (e *engine) Snapshot() api.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, _ := json.Marshal(e.state)
	var v api.Snapshot
	_ = json.Unmarshal(b, &v)
	return v
}
func (e *engine) save() {
	e.state.Revision++
	b, _ := json.Marshal(e.state)
	_ = os.WriteFile(filepath.Join(e.root, "fixture-state.json"), b, 0600)
}
func (*engine) Connect(context.Context) error { return nil }
func (*engine) Close(context.Context) error   { return nil }
func (e *engine) Interrupt(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state.Phase = "idle"
	e.state.CanSend = true
	e.state.CanInterrupt = false
	e.save()
	return nil
}
func (e *engine) Submit(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, item := range e.state.Items {
		if item.RequestID == in.ID && item.Kind == "user" {
			return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
		}
	}
	e.state.Items = append(e.state.Items, api.Item{ID: in.ID, RequestID: in.ID, Kind: "user", Text: in.Text, Status: "completed"})
	e.state.LastReceipt = api.Receipt{ID: in.ID, Outcome: "accepted"}
	e.state.Phase = "thinking"
	e.state.CanSend = false
	e.state.CanInterrupt = true
	e.save()
	go func() {
		text := "这是测试后端的流式回复，不会调用真实模型。"
		if len(files) > 0 {
			text = fmt.Sprintf("已通过真实附件路径收到 %d 个文件。", len(files))
		}
		if strings.Contains(in.Text, "审批") {
			e.mu.Lock()
			e.state.Approvals = append(e.state.Approvals, api.Approval{ID: in.ID + "-approval", Owner: "bot", Title: "允许测试操作？", Description: "这是无外部效果的验收请求。", Status: "pending", Choices: []api.Choice{{ID: "allow", Label: "允许一次", Scope: "once"}, {ID: "deny", Label: "拒绝", Scope: "once"}}})
			e.state.Phase = "approval"
			e.save()
			e.mu.Unlock()
			return
		}
		if strings.Contains(in.Text, "问题") {
			e.mu.Lock()
			e.state.Items = append(e.state.Items, api.Item{ID: in.ID + "-question", Kind: "assistant", Text: "请选择测试方案。", Status: "completed", QuestionCards: []api.QuestionCard{{ShortID: "q-fixture", Binding: in.ID, Title: "希望使用哪种方案？", Options: []string{"简洁", "详细"}, State: ""}}})
			e.state.CanSend = true
			e.state.CanInterrupt = false
			e.state.Phase = "idle"
			e.save()
			e.mu.Unlock()
			return
		}
		for i := 1; i <= len([]rune(text)); i++ {
			time.Sleep(70 * time.Millisecond)
			e.mu.Lock()
			id := in.ID + "-reply"
			if i == 1 {
				e.state.Items = append(e.state.Items, api.Item{ID: id, Kind: "assistant", Status: "inProgress"})
			}
			item := &e.state.Items[len(e.state.Items)-1]
			item.Text = string([]rune(text)[:i])
			if i == len([]rune(text)) {
				item.Status = "completed"
				item.Artifacts = []api.Artifact{{ID: "fixture-result", Name: "result.txt"}}
				e.state.Phase = "idle"
				e.state.CanSend = true
				e.state.CanInterrupt = false
				e.state.CanInterrupt = false
			}
			e.save()
			e.mu.Unlock()
		}
	}()
	return e.state.LastReceipt, nil
}
func (e *engine) Decide(_ context.Context, d api.Decision) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.state.Approvals {
		a := &e.state.Approvals[i]
		if a.ID == d.ID && a.Status == "pending" && (d.Choice == "allow" || d.Choice == "deny") {
			a.Status = "resolved"
			a.Resolution = &api.ApprovalResolution{ChoiceID: d.Choice, Outcome: "handled", Scope: "once"}
			e.state.Phase = "idle"
			e.state.CanSend = true
			e.state.CanInterrupt = false
			e.save()
			return nil
		}
	}
	return errors.New("stale decision")
}
func (e *engine) Artifact(id string) (string, error) {
	if id != "fixture-result" {
		return "", errors.New("not owned")
	}
	return filepath.Join(e.root, "result.txt"), nil
}
func (*engine) Initialization() api.BotInitialization {
	return api.BotInitialization{Status: "accepted"}
}
func (e *engine) Initialize(context.Context, api.BotIntroduction) (api.BotInitialization, error) {
	return e.Initialization(), nil
}
func (e *engine) RetryInitialization(context.Context) (api.BotInitialization, error) {
	return e.Initialization(), nil
}

// Read-only synthetic projections: these never create or execute a Worker.
func (e *engine) SurfaceTasks() []api.Task {
	return []api.Task{{ID: "fixture-task", Title: "整理验收记录", Status: "completed", Result: "已整理测试记录。此记录为 fixture，没有执行外部任务。"}, {ID: "fixture-running", Title: "等待测试确认", Status: "running"}}
}
func (e *engine) SurfaceTaskConversation(_ context.Context, id string) (api.TaskConversation, error) {
	if id == "fixture-task" {
		return api.TaskConversation{Status: "completed", Messages: []api.TaskConversationMessage{{ID: "fixture-task-message", Role: "assistant", Text: "已整理测试记录。此记录为 fixture，没有执行外部任务。"}}}, nil
	}
	if id == "fixture-running" {
		return api.TaskConversation{Status: "running", Messages: []api.TaskConversationMessage{{ID: "fixture-working-message", Role: "assistant", Text: "测试任务正在等待确认。"}}}, nil
	}
	return api.TaskConversation{}, errors.New("task not owned")
}

func main() {
	root := flag.String("data-dir", "", "dedicated fixture profile")
	assets := flag.String("assets", "frontend/dist", "compiled shared assets")
	listen := flag.String("listen", "127.0.0.1:9318", "listener")
	origin := flag.String("origin", "http://127.0.0.1:9318", "browser origin")
	flag.Parse()
	if !filepath.IsAbs(*root) {
		log.Fatal("absolute dedicated fixture data directory required")
	}
	if err := os.MkdirAll(*root, 0700); err != nil {
		log.Fatal(err)
	}
	e := &engine{root: *root, state: api.Snapshot{Revision: 1, Connection: "ready", CanSend: true, Phase: "idle", Items: []api.Item{}, Approvals: []api.Approval{}, Reviews: []api.Review{}, References: []api.Reference{}}}
	if b, err := os.ReadFile(filepath.Join(*root, "fixture-state.json")); err == nil {
		if json.Unmarshal(b, &e.state) != nil {
			log.Fatal("invalid fixture state")
		}
	}
	_ = os.WriteFile(filepath.Join(*root, "result.txt"), []byte("CaelisBot Linux Web fixture result\n"), 0600)
	b, s, err := desktop.NewWebFixture(*root, e)
	if err != nil {
		log.Fatal(err)
	}
	b.ConfigureInitialization(e)
	go func() {
		for range time.Tick(100 * time.Millisecond) {
			b.ObserveChat(e.Snapshot())
		}
	}()
	b.SetQuestionLifecycle(func(items []api.Item) ([]api.Item, uint64) {
		v := e.Snapshot()
		for i := range items {
			for _, original := range v.Items {
				if items[i].ID == original.ID {
					items[i].QuestionCards = original.QuestionCards
				}
			}
		}
		return items, 0
	}, nil, nil)
	b.SetQuestionAnswerer(func(_ context.Context, id, binding, value, request string) string {
		e.mu.Lock()
		defer e.mu.Unlock()
		for i := range e.state.Items {
			for j := range e.state.Items[i].QuestionCards {
				q := &e.state.Items[i].QuestionCards[j]
				if q.ShortID == id && q.Binding == binding && q.State == "" {
					q.State = "answered"
					e.state.Items = append(e.state.Items, api.Item{ID: request, Kind: "user", Text: value, Status: "completed"})
					e.save()
					return "回答已提交，等待后续回复。"
				}
			}
		}
		return "问题已结束。"
	})
	server, err := websurface.New(websurface.Config{Directory: filepath.Join(*root, "Web"), Origins: []string{*origin}, Version: "FIXTURE · no real model"}, websurface.Services{App: e, Backend: b, Surface: s}, os.DirFS(*assets))
	if err != nil {
		log.Fatal(err)
	}
	defer server.Close()
	fmt.Println("FIXTURE:", *origin)
	fmt.Println("Pairing code file:", filepath.Join(*root, "Web", "pairing-code"))
	log.Fatal(http.ListenAndServe(*listen, server))
}
