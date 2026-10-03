package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"strings"
)

func remoteArtifact(arch string) ([]byte, error) {
	if arch != "amd64" && arch != "arm64" {
		return nil, errors.New("unsupported remote architecture")
	}
	root := os.Getenv("CAELIS_BOT_REMOTE_HELPER_DIR")
	if root == "" {
		exe, e := os.Executable()
		if e != nil {
			return nil, e
		}
		root = filepath.Join(filepath.Dir(exe), "../Resources/remote")
	}
	p := filepath.Join(root, "linux-"+arch)
	b, e := os.ReadFile(p)
	if e != nil {
		return nil, errors.New("remote helper missing from app")
	}
	expected, e := os.ReadFile(p + ".sha256")
	sum := sha256.Sum256(b)
	if e != nil || hex.EncodeToString(sum[:]) != strings.TrimSpace(string(expected)) {
		return nil, errors.New("remote helper integrity check failed")
	}
	return b, nil
}
func (a *Application) MachineTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	return a.machines.MachineTerminal(ctx, id)
}
func (a *Application) ReadMachineAdvanced(ctx context.Context, id string) (api.Machine, error) {
	return a.machines.ReadMachineAdvanced(ctx, id)
}
