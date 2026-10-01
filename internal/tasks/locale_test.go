package tasks

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

func TestLocalizedOwnershipErrorsReleaseTaskAdmission(t *testing.T) {
	for _, locale := range []i18n.Locale{i18n.English, i18n.Chinese} {
		t.Run(string(locale), func(t *testing.T) {
			f := newRuntime()
			m := openFixture(t, t.TempDir(), "fixture", f)
			m.SetLocale(func() i18n.Locale { return locale })
			done := make(chan error, 1)
			go func() { _, err := m.ReadTask(t.Context(), "foreign"); done <- err }()
			select {
			case err := <-done:
				if err == nil || err.Error() != i18n.Text(locale, "host.onlyOperateBotCreatedTasks", nil) {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("localized rejection blocked task admission")
			}
			v := start(t, m, "after-rejection")
			if _, err := m.ReadTask(t.Context(), v.ID); err != nil {
				t.Fatal(err)
			}
			if len(m.ListTasks()) != 1 {
				t.Fatal("task manager remained blocked")
			}
			if _, err := m.capture(v.ID, api.Task{ID: "wrong-native-id"}, nil); err == nil || !strings.Contains(err.Error(), i18n.Text(locale, "host.runtimeReturnedDifferentTask", nil)) {
				t.Fatal(err)
			}
			m.state.Records[v.ID].Provider = "another-runtime"
			if err := m.refresh(); err == nil || err.Error() != i18n.Text(locale, "host.taskConflictOtherRuntime", nil) {
				t.Fatal(err)
			}
		})
	}
}

func TestTerminalFeedbackDistinguishesRemoteUnsupportedAndOfflineInCurrentLocale(t *testing.T) {
	for _, locale := range []i18n.Locale{i18n.English, i18n.Chinese} {
		t.Run(string(locale), func(t *testing.T) {
			m, _, _, _, router, target := routedFixture(t)
			m.SetLocale(func() i18n.Locale { return locale })
			in := input("localized-terminal-route")
			in.Target = &target
			v, err := m.StartTask(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			if err = router.Set(nodes.Node{ID: target.NodeID, Label: "Original node"}, nodes.Capability{Target: target, State: nodes.Ready}, &workerwire.Client{}); err != nil {
				t.Fatal(err)
			}
			_, err = m.WorkTerminal(t.Context(), v.ID)
			if !errors.Is(err, api.ErrRemoteWorkTerminal) || err.Error() != i18n.Text(locale, "host.remoteTaskTerminalUnavailable", nil) {
				t.Fatal("remote TTY feedback lost locale or typed cause", err)
			}
			if err = router.Set(nodes.Node{ID: target.NodeID, Label: "Original node"}, nodes.Capability{Target: target, State: nodes.Unavailable}, nil); err != nil {
				t.Fatal(err)
			}
			_, err = m.WorkTerminal(t.Context(), v.ID)
			if !errors.Is(err, api.ErrWorkTerminalOffline) || errors.Is(err, api.ErrRemoteWorkTerminal) || err.Error() != i18n.Text(locale, "host.taskTerminalNodeUnavailable", nil) {
				t.Fatal("offline original node feedback hidden", err)
			}
		})
	}
}

func TestLocaleCallbackRunsOutsideLocaleLock(t *testing.T) {
	m := &Manager{}
	m.SetLocale(func() i18n.Locale { m.SetLocale(nil); return i18n.English })
	done := make(chan i18n.Locale, 1)
	go func() { done <- m.currentLocale() }()
	select {
	case got := <-done:
		if got != i18n.English {
			t.Fatal(got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("locale callback invoked under lock")
	}
}
