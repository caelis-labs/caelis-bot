package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

func TestRemoteCommandsAreHiddenFromChatAcrossReload(t *testing.T) {
	a, _ := fixtureApp(t, newTestEngine(), Host{})
	classified, plain := true, false
	items := []api.Item{
		{ID: "channel-input:refresh", RequestID: "refresh", Kind: "user", Text: " /R ", Status: "received"},
		{ID: "channel-input:status", RequestID: "status", Kind: "user", Text: "/status", Status: "received"},
		{ID: "channel-input:private", RequestID: "private", Kind: "user", Text: "回答内容已隐藏", RemoteCommand: &plain},
		{ID: "channel-input:path", RequestID: "path", Kind: "user", Text: "/usr/local/bin"},
		{ID: "channel-input:caption", RequestID: "caption", Kind: "user", Text: "/status", RemoteCommand: &plain},
		{ID: "control-input:local", RequestID: "local", Kind: "user", Text: "/status"},
		{ID: "control-input:reply", RequestID: "reply", Kind: "user", Text: "yes", RemoteCommand: &classified, QuestionReply: &api.QuestionReply{ShortID: "Q1", Binding: "original", Outcome: "accepted"}},
		{ID: "control:status-original", Kind: "controlNotice", Text: "original status receipt"},
	}
	for _, origin := range []struct{ id, channel string }{
		{"refresh", "weixin"}, {"status", "telegram"}, {"path", "weixin"}, {"local", "desktop"},
	} {
		if err := a.textControl.RecordOrigin(origin.id, textchannel.Origin{Channel: origin.channel, Conversation: "fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(s *backend.Service) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if len(s.Snapshot().Items) != len(items) {
				continue
			}
			got := s.ChatSnapshot(0, "", "").Snapshot.Items
			if len(got) != 6 {
				t.Fatalf("command leaked or caption/receipt disappeared: %+v", got)
			}
			for _, item := range got {
				if item.RequestID == "refresh" || item.RequestID == "status" {
					t.Fatalf("control input appeared in chat: %+v", item)
				}
			}
			return
		}
		t.Fatal("original IM rows did not load")
	}
	a.Backend.ObserveChat(api.Snapshot{Items: items})
	check(a.Backend)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := backend.NewService(newTestEngine(), nil, nil, nil, nil)
	reopened.ConfigureChat(filepath.Join(a.root, "chat.sqlite"))
	reopened.ConfigureLegacyRemoteCommand(a.legacyRemoteCommand)
	t.Cleanup(func() { _ = reopened.Shutdown() })
	check(reopened)
}

type idleActivityEngine struct {
	*testEngine
	api.ConversationRuntime
	activityMu sync.Mutex
	session    string
	renewals   int
	submitted  map[string]string
}

func (*idleActivityEngine) Snapshot() api.Snapshot { return api.Snapshot{CanSend: true} }

func (e *idleActivityEngine) ConversationState() api.ConversationState {
	e.activityMu.Lock()
	defer e.activityMu.Unlock()
	return api.ConversationState{Session: e.session, Observed: true, Idle: true}
}

func (e *idleActivityEngine) RenewIdleConversation(_ context.Context, _ string, _ api.ConversationState) error {
	e.activityMu.Lock()
	defer e.activityMu.Unlock()
	e.renewals++
	e.session = "replacement"
	return nil
}

func (e *idleActivityEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.activityMu.Lock()
	defer e.activityMu.Unlock()
	if _, exists := e.submitted[in.ID]; !exists {
		e.submitted[in.ID] = e.session
	}
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}

func TestDesktopStatusPreservesIdleRenewalAtBothMessageEntries(t *testing.T) {
	for _, channel := range []string{"desktop", "telegram", "weixin"} {
		t.Run(channel, func(t *testing.T) {
			engine := &idleActivityEngine{testEngine: newTestEngine(), session: "original", submitted: map[string]string{}}
			a, _ := fixtureApp(t, engine, Host{ResolveFiles: func([]string) ([]api.InputFile, error) { return nil, nil }})
			startNoticeResident(t, a)
			a.Backend.ConfigureInitialization(nil)
			a.Backend.SetUserSubmitter(a.companion.SubmitUser)
			ctx := t.Context()
			last := time.Now().Truncate(time.Microsecond).Add(-49 * time.Hour)
			a.Backend.ObserveChat(api.Snapshot{Items: []api.Item{
				{ID: "outgoing:old", RequestID: "old", Kind: "user", Status: "accepted", Text: "previous user message", SeenAt: last.UnixMicro()},
			}})
			control := api.Submission{ID: "desktop-status", Text: "/status"}
			for range 2 {
				if receipt, err := a.Backend.Submit(ctx, control); err != nil || receipt.Outcome != "accepted" {
					t.Fatalf("desktop command was not handled: %v %v", receipt, err)
				}
			}
			if channel != "desktop" {
				a.textControl.Handle(ctx, textchannel.Inbound{Channel: channel, Conversation: "peer", ID: "remote-status", Text: "/status"}, a.Backend.Snapshot())
			}
			waitChatInputs(t, func() []api.Item { return a.Backend.ChatSnapshot(0, "", "").Snapshot.Items }, control.ID)
			if activity, known, err := a.Backend.BotConversationActivity(ctx, control.ID, a.introductionRequest); err != nil || known || !activity.Equal(last) {
				t.Fatalf("control receipt affected conversation activity or retry: %v %t %v", activity, known, err)
			}

			in := api.Submission{ID: channel + ":ordinary", Text: "continue with my question"}
			submit := a.Backend.Submit
			if channel != "desktop" {
				ingress := api.Item{ID: "channel-input:" + in.ID, RequestID: in.ID, Kind: "user", Status: "received", Text: in.Text}
				a.Backend.ObserveChat(api.Snapshot{Items: []api.Item{ingress}})
				submit = func(ctx context.Context, input api.Submission) (api.Receipt, error) {
					return backend.SubmitRemote(ctx, a.Backend, input, nil)
				}
			}
			if receipt, err := submit(ctx, in); err != nil || receipt.Outcome != "accepted" {
				t.Fatalf("ordinary input was not accepted: %v %v", receipt, err)
			}
			if _, known, err := a.Backend.BotConversationActivity(ctx, in.ID, a.introductionRequest); err != nil || !known {
				t.Fatalf("the original native submission was not remembered: %t %v", known, err)
			}
			if receipt, err := submit(ctx, in); err != nil || receipt.Outcome != "accepted" {
				t.Fatalf("ordinary retry did not return its receipt: %v %v", receipt, err)
			}
			engine.activityMu.Lock()
			defer engine.activityMu.Unlock()
			if engine.renewals != 1 || engine.submitted[in.ID] != "replacement" || len(engine.submitted) != 1 {
				t.Fatalf("expected only the ordinary message in one replacement: %d %v", engine.renewals, engine.submitted)
			}
		})
	}
}
