package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const stagedRuntimeFile = "onboarding-runtime.json"

type stagedRuntimeDocument struct {
	Version int    `json:"version"`
	Runtime string `json:"runtime"`
}

func readStagedRuntime(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, stagedRuntimeFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", errors.New("无法读取首次连接选择")
	}
	var doc stagedRuntimeDocument
	if json.Unmarshal(b, &doc) != nil || doc.Version != 1 || (doc.Runtime != "codex" && doc.Runtime != "caelis") {
		return "", errors.New("首次连接选择无法识别，请保留文件并重试")
	}
	return doc.Runtime, nil
}

func writeStagedRuntime(root, id string) error {
	return localstate.Write(filepath.Join(root, stagedRuntimeFile), stagedRuntimeDocument{Version: 1, Runtime: id})
}
