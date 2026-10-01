package runtimemanagement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Version inspection uses a fresh empty home and allowlisted environment. It
// neither reads nor copies the user's model/store credentials or configuration.
func verifyRelease(ctx context.Context, binary string, release Release) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	home, err := os.MkdirTemp(filepath.Dir(binary), ".verify-home-")
	if err != nil {
		return errors.New("verification home unavailable")
	}
	defer os.RemoveAll(home)
	args := []string{"--version"}
	if release.Runtime == "caelis" {
		args = []string{"version", "--format", "json"}
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "CODEX_HOME=" + filepath.Join(home, ".codex"), "LANG=C", "LC_ALL=C"}
	cmd.Dir = home
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	var output boundedOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return errors.New("version command failed")
	}
	if release.Runtime == "codex" {
		if strings.TrimSpace(output.String()) != "codex-cli "+release.Version {
			return errors.New("Codex version mismatch")
		}
	} else {
		var version struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(output.Bytes(), &version) != nil || strings.TrimPrefix(version.Version, "v") != release.Version {
			return errors.New("Caelis version mismatch")
		}
	}
	return nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 64<<10 {
		return 0, errors.New("version output exceeds limit")
	}
	return b.Buffer.Write(data)
}
