// Package runtimeinstall owns explicit, user-triggered official installation.
package runtimeinstall

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const CodexURL = "https://chatgpt.com/codex/install.sh"

func Codex(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return errors.New("此平台暂未支持自动安装")
	}
	b, e := DownloadOfficialScript(ctx, CodexURL, "chatgpt.com")
	if e != nil {
		return errors.New("无法取得 Codex 官方安装程序；请检查网络、代理或证书设置后重试")
	}
	f, e := os.CreateTemp("", "codex-installer-*.sh")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", f.Name())
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "PATH=") && !strings.HasPrefix(v, "CODEX_INSTALL") && !strings.HasPrefix(v, "CODEX_NON_INTERACTIVE=") && !strings.HasPrefix(v, "CODEX_RELEASE=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	home, _ := os.UserHomeDir()
	cmd.Env = append(cmd.Env, "CODEX_NON_INTERACTIVE=1", "PATH="+filepath.Join(home, ".local", "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	if e = cmd.Run(); e != nil {
		return errors.New("Codex 安装未完成，请检查网络和安装目录权限后重试")
	}
	return nil
}
