package caelis

import (
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"os"
)

func privateState(path string, _ os.FileInfo) bool { return localstate.IsPrivate(path) }
