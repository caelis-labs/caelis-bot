package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
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
