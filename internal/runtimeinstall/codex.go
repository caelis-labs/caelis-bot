// Package runtimeinstall owns explicit, user-triggered official installation.
package runtimeinstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: codexRedirect}
	res, e := client.Do(req)
	if e != nil {
		return codexDownloadError(e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Codex 官方安装程序下载失败（HTTP %d），请稍后重试", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if e != nil {
		return errors.New("Codex 安装程序下载中断，请重试")
	}
	if len(b) > 1<<20 || len(b) == 0 {
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
	return runCodexInstaller(ctx, f.Name())
}

func runCodexInstaller(ctx context.Context, script string) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", script)
	configureInstallerCommand(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "PATH=") && !strings.HasPrefix(v, "CODEX_INSTALL") && !strings.HasPrefix(v, "CODEX_NON_INTERACTIVE=") && !strings.HasPrefix(v, "CODEX_RELEASE=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	home, _ := os.UserHomeDir()
	cmd.Env = append(cmd.Env, "CODEX_NON_INTERACTIVE=1", "PATH="+filepath.Join(home, ".local", "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return codexDownloadError(ctx.Err())
		}
		var exit *exec.ExitError
		if errors.As(e, &exit) {
			return fmt.Errorf("Codex 官方安装脚本执行失败（退出码 %d），请检查下载连接和安装目录权限", exit.ExitCode())
		}
		return errors.New("无法启动 Codex 官方安装脚本，请检查本机执行权限")
	}
	return nil
}

var errCodexRedirect = errors.New("Codex 安装地址跳转到未允许的地址，已停止下载")
var errCodexRedirectLimit = errors.New("Codex 安装地址重定向次数过多，已停止下载")

func codexRedirect(r *http.Request, via []*http.Request) error {
	if len(via) > 4 {
		return errCodexRedirectLimit
	}
	if r.URL.Scheme != "https" || r.URL.User != nil || (r.URL.Port() != "" && r.URL.Port() != "443") {
		return errCodexRedirect
	}
	switch r.URL.Hostname() {
	case "chatgpt.com", "releases.openai.com":
		return nil
	default:
		return errCodexRedirect
	}
}
func codexDownloadError(err error) error {
	if errors.Is(err, errCodexRedirect) {
		return errCodexRedirect
	}
	if errors.Is(err, errCodexRedirectLimit) {
		return errCodexRedirectLimit
	}
	if errors.Is(err, context.Canceled) {
		return errors.New("Codex 安装已取消")
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return errors.New("Codex 安装请求超时，请检查网络后重试")
	}
	return errors.New("无法连接 Codex 官方下载服务，请检查网络、代理或证书设置")
}
