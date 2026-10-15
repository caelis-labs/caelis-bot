package desktop

import (
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomCommandValidationIsActionable(t *testing.T) {
	s := &Service{}
	if err := s.configureLanguage(filepath.Join(t.TempDir(), "language.json"), []string{"zh"}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ command, want string }{{"{script}", "终端程序路径"}, {"terminal \"{script}", "引号"}, {"terminal file", "{script}"}} {
		_, err := s.SaveTaskPreferences(tasks.Preferences{MaxRunning: 6, Terminal: "custom", CustomCommand: tt.command})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("%q: %v", tt.command, err)
		}
	}
}
