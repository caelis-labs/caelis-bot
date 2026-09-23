// Package runtimeinstall owns explicit, user-triggered official installation.
package runtimeinstall

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const CodexURL = "https://chatgpt.com/codex/install.sh"

func Codex(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return errors.New("此平台暂未支持自动安装")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", CodexURL, nil)
	if e != nil {
		return e
	}
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 4 || r.URL.Scheme != "https" || r.URL.Hostname() != "chatgpt.com" {
			return errors.New("安装地址重定向不受支持")
		}
		return nil
	}}
	res, e := client.Do(req)
	if e != nil {
		return errors.New("无法下载 Codex 官方安装程序，请检查网络")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("Codex 官方安装程序暂不可用")
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if e != nil || len(b) > 1<<20 || len(b) == 0 {
		return errors.New("安装程序内容无效")
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
