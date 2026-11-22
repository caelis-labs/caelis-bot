package app

import (
	"path/filepath"
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
