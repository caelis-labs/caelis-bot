package desktop

import (
	"github.com/caelis-labs/caelis-bot/internal/windowsnotifications"
	"os"
	"path/filepath"
	"strings"
)

func customInstanceProfile(root string) bool {
	base, err := nativeDataDirectoryBase()
	name, _ := applicationIdentity()
	return err != nil || !strings.EqualFold(filepath.Clean(root), filepath.Join(base, name))
}

// Fixed protocol activation never interprets notification content as commands.
func HandleNativeArguments(args []string) (bool, error) {
	if root, handled, err := windowsnotifications.ParseActivation(args); handled {
		if err != nil {
			return true, err
		}
		if err = os.Setenv("CAELIS_BOT_DATA_DIR", root); err != nil {
			return true, err
		}
		os.Args = []string{os.Args[0], "--chat"}
		return false, nil
	}
	if len(args) == 1 && args[0] == "--unregister-notifications" {
		root, err := applicationDataDirectory()
		if err != nil {
			return true, err
		}
		exe, err := os.Executable()
		if err != nil {
			return true, err
		}
		return true, windowsnotifications.Unregister(root, exe)
	}
	return false, nil
}
