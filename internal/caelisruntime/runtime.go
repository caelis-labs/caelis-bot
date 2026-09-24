// Package caelisruntime uses the public Caelis CLI for installation and service lifecycle.
// It never embeds the runtime or takes ownership of a shared Host process.
package caelisruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

const InstallerURL = "https://caelis.dev/install.sh"

type Status struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Message   string `json:"message"`
}

func Find(path string) (string, error) {
	if path != "" {
		if !filepath.IsAbs(path) {
			return "", errors.New("请输入 Caelis 可执行文件的完整路径")
		}
		i, e := os.Stat(path)
		if e != nil || !i.Mode().IsRegular() || i.Mode()&0111 == 0 {
			return "", errors.New("Caelis 路径不是可执行文件")
		}
		return filepath.Clean(path), nil
	}
	if p, e := exec.LookPath("caelis"); e == nil {
		return filepath.Abs(p)
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".local/bin/caelis"), "/opt/homebrew/bin/caelis", "/usr/local/bin/caelis"} {
		if f, e := Find(p); e == nil {
			return f, nil
		}
	}
	return "", errors.New("尚未找到 Caelis，请安装或选择本机可执行文件")
}
func Store(path string) (string, error) {
	if path != "" {
		if !filepath.IsAbs(path) {
			return "", errors.New("Caelis 数据目录必须是完整路径")
		}
		return filepath.Clean(path), nil
	}
	h, e := os.UserHomeDir()
	return filepath.Join(h, ".caelis"), e
}

// run bounds output and never returns raw stderr (which can include private configuration).
func run(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.Env = runtimeenv.Clean(os.Environ())
	if err := cmd.Run(); err != nil {
		return nil, errors.New("Caelis 命令未成功，请检查本机安装和运行时配置")
	}
	return out.Bytes(), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, errors.New("输出超过限制")
	}
	return b.Buffer.Write(p)
}
func Inspect(ctx context.Context, path string) (Status, error) {
	p, e := Find(path)
	if e != nil {
		return Status{Message: e.Error()}, nil
	}
	ctx, c := context.WithTimeout(ctx, 8*time.Second)
	defer c()
	b, e := run(ctx, p, "version", "--format", "json")
	if e != nil {
		return Status{Path: p, Message: e.Error()}, e
	}
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &v) != nil || v.Version == "" {
		return Status{}, errors.New("该文件没有返回有效的 Caelis 版本")
	}
	return Status{Installed: true, Path: p, Version: v.Version, Message: "Caelis 已安装"}, nil
}
func Manage(ctx context.Context, action, path, store string) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if action == "detect" {
		return Inspect(ctx, path)
	}
	if action == "install" {
		if runtime.GOOS != "darwin" {
			return Status{}, errors.New("当前平台尚未提供安装入口")
		}
		if _, e := Find(path); e == nil {
			return Status{}, errors.New("Caelis 已安装，请使用检查更新或更新")
		}
		if path != "" {
			return Status{}, errors.New("自动安装只适用于默认位置；自定义路径请自行安装")
		}
		if e := install(ctx); e != nil {
			return Status{}, e
		}
		return Inspect(ctx, "")
	}
	p, e := Find(path)
	if e != nil {
		return Status{}, e
	}
	dir, e := Store(store)
	if e != nil {
		return Status{}, e
	}
	switch action {
	case "check-update", "update":
		args := []string{"update", "--store-dir", dir}
		if action == "check-update" {
			args = append(args, "--check")
		}
		b, e := run(ctx, p, args...)
		if e != nil {
			return Status{}, e
		}
		v, e := Inspect(ctx, p)
		v.Message = strings.TrimSpace(string(b))
		if len(v.Message) > 2000 {
			v.Message = "Caelis 已完成更新操作，请重新检测"
		}
		return v, e
	case "start":
		raw, e := run(ctx, p, "service", "status", "--store-dir", dir, "--format", "json")
		if e != nil {
			return Status{}, e
		}
		var service struct {
			State string `json:"state"`
		}
		if json.Unmarshal(raw, &service) != nil {
			return Status{}, errors.New("无法核对 Caelis 服务状态")
		}
		if service.State == "running" {
			v, e := Inspect(ctx, p)
			v.Message = "Caelis 服务已在运行；如需升级运行中的服务，请通过 Caelis 完成。"
			return v, e
		}
		if service.State != "stopped" {
			return Status{}, errors.New("Caelis 服务状态不明确，未启动新进程")
		}
		_, e = run(ctx, p, "service", "start", "--store-dir", dir, "--format", "json")
		if e != nil {
			return Status{}, e
		}
		v, e := Inspect(ctx, p)
		v.Message = "Caelis 服务已启动"
		return v, e
	default:
		return Status{}, errors.New("不支持的运行时操作")
	}
}
func install(ctx context.Context) error {
	ctx, c := context.WithTimeout(ctx, 5*time.Minute)
	defer c()
	req, e := http.NewRequestWithContext(ctx, "GET", InstallerURL, nil)
	if e != nil {
		return e
	}
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 4 || r.URL.Scheme != "https" || r.URL.Hostname() != "caelis.dev" {
			return errors.New("不安全的安装重定向")
		}
		return nil
	}}
	res, e := client.Do(req)
	if e != nil {
		return errors.New("无法下载 Caelis 官方安装程序")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Caelis 安装程序下载失败（%d）", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if e != nil || len(b) > 1<<20 {
		return errors.New("安装程序内容无效")
	}
	file, e := os.CreateTemp("", "caelis-installer-*.sh")
	if e != nil {
		return e
	}
	defer os.Remove(file.Name())
	_, e = file.Write(b)
	ce := file.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	_, e = run(ctx, "/bin/sh", file.Name())
	return e
}
