package taskterminal

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestWindowsTerminalArgumentsPreserveNativeSocket(t *testing.T) {
	target := api.TerminalTarget{Runtime: "codex", Binary: `C:\工具\codex.exe`, Directory: `C:\工作`, Endpoint: `unix://C:/Users/Test/runtime.sock`, Thread: "owned-task"}
	args, err := TerminalArguments(target)
	if err != nil || len(args.Args) != 6 || args.Args[3] != target.Endpoint || args.Args[5] != target.Thread {
		t.Fatalf("native terminal target rejected or changed: %+v %v", args, err)
	}
	for _, endpoint := range []string{"unix://relative.sock", "unix://server/share/socket", "unix:////server/share/socket", "ws://127.0.0.1:99", "npipe://./pipe/codex"} {
		target.Endpoint = endpoint
		if _, err := TerminalArguments(target); err == nil {
			t.Fatalf("unowned endpoint accepted: %s", endpoint)
		}
	}
}
