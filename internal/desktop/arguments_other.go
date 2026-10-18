//go:build !windows

package desktop

import "os"

func customInstanceProfile(string) bool { return os.Getenv("CAELIS_BOT_DATA_DIR") != "" }

func HandleNativeArguments([]string) (bool, error) { return false, nil }
