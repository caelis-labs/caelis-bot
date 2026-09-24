package codex

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/runtimeinstall"
	"github.com/caelis-labs/caelis-bot/internal/updates"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func InspectRuntime(ctx context.Context, path string) (api.RuntimeStatus, error) {
	if path != "" && !filepath.IsAbs(path) {
		return api.RuntimeStatus{}, errors.New("请输入 Codex 程序的完整路径")
	}
	p, e := runtimeBinary(path)
	if e != nil {
		return api.RuntimeStatus{Message: "尚未找到 Codex"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, "--version")
	cmd.Stderr = io.Discard
	b, e := cmd.Output()
	if e != nil || len(b) > 4096 || !strings.HasPrefix(string(b), "codex-cli ") {
		return api.RuntimeStatus{Path: p}, errors.New("该程序没有返回有效的 Codex 版本")
	}
	return api.RuntimeStatus{Installed: true, Path: p, Version: strings.TrimSpace(strings.TrimPrefix(string(b), "codex-cli "))}, nil
}
func ManageRuntime(ctx context.Context, action, path string) (api.RuntimeStatus, error) {
	current, e := InspectRuntime(ctx, path)
	if e != nil {
		return current, e
	}
	switch action {
	case "detect":
		return current, nil
	case "check-update":
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		req, e := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/openai/codex/releases/latest", nil)
		if e != nil {
			return current, e
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			return current, errors.New("无法检查 Codex 更新")
		}
		defer res.Body.Close()
		var v struct {
			Tag string `json:"tag_name"`
		}
		if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&v) != nil || v.Tag == "" {
			return current, errors.New("无法读取 Codex 最新版本")
		}
		current.LatestVersion = strings.TrimPrefix(v.Tag, "rust-v")
		current.Message = "最新版本 " + current.LatestVersion + " · 当前 " + current.Version
		if order, err := updates.CompareVersions(current.LatestVersion, current.Version); err == nil {
			current.UpdateState = "current"
			if order > 0 {
				current.UpdateState = "available"
			}
		}
		return current, nil
	case "install", "update":
		if path != "" {
			return current, errors.New("自定义位置请通过原安装方式更新，或切回自动发现安装官方版本")
		}
		if action == "install" && current.Installed {
			return current, errors.New("已找到 Codex，请使用更新")
		}
		// Do not overwrite a Homebrew/npm installation with another package manager.
		home, _ := os.UserHomeDir()
		standard := filepath.Join(home, ".local", "bin", "codex")
		if action == "update" && current.Installed && current.Path != standard {
			return current, errors.New("当前 Codex 来自其他安装方式，请使用原安装方式更新，或选择官方安装的程序")
		}
		if e = runtimeinstall.Codex(ctx); e != nil {
			return current, e
		}
		return InspectRuntime(ctx, "")
	default:
		return current, errors.New("不支持的 Codex 安装操作")
	}
}
