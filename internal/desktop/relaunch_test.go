package desktop

import (
	"strings"
	"testing"
)

func TestResearchRelaunchKeepsIsolatedRuntimeRoots(t *testing.T) {
	command := runtimeRelaunchCommand("/tmp/research/Caelis Bot Research.app/Contents/MacOS/caelis-bot", "/tmp/profile/data", 41, true, "/tmp/profile/home", "/tmp/profile/home/.codex", "/tmp/profile/home/.local/bin:/usr/bin")
	got := command.Args
	if got[0] != "/bin/sh" || got[3] != "caelis-relaunch" || got[4] != "41" || got[5] != "/usr/bin/open" || got[6] != "/tmp/research/Caelis Bot Research.app" || got[7] != "/tmp/profile/data" {
		t.Fatalf("unexpected relaunch target: %q", got[:8])
	}
	if len(got) != 11 || got[8] != "/tmp/profile/home" || got[9] != "/tmp/profile/home/.codex" || got[10] != "/tmp/profile/home/.local/bin:/usr/bin" {
		t.Fatalf("isolated roots not retained: %q", got[8:])
	}
	for _, key := range []string{"CAELIS_BOT_DATA_DIR=$4", "HOME=$5", "ZDOTDIR=$5", "CODEX_HOME=$6", "PATH=$7"} {
		if !strings.Contains(got[2], key) {
			t.Errorf("relaunch omitted %s", key)
		}
	}
}

func TestProductionRelaunchDoesNotOverrideRuntimeHome(t *testing.T) {
	command := runtimeRelaunchCommand("/Applications/Caelis Bot.app/Contents/MacOS/caelis-bot", "/Users/user/Library/Application Support/Caelis Bot", 42, false, "/tmp/ignored", "/tmp/ignored/.codex", "/tmp/ignored/bin")
	if len(command.Args) != 8 || strings.Contains(command.Args[2], "HOME=") || strings.Contains(command.Args[2], "PATH=") {
		t.Fatalf("production relaunch changed account environment: %q", command.Args)
	}
}
