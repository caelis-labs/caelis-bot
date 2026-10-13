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
	status := api.RuntimeStatus{Installed: true, Path: p, Version: strings.TrimSpace(strings.TrimPrefix(string(b), "codex-cli "))}
	source := runtimeinstall.Detect(ctx, p, "codex", "@openai/codex")
	status.Source = source.Kind
	if status.Source == "unknown" && officialCodex(p) {
		status.Source = "official"
	}
	return status, nil
}
func officialCodex(path string) bool {
	home, _ := os.UserHomeDir()
	entry := filepath.Join(home, ".local", "bin", "codex")
	actual, err := filepath.EvalSymlinks(entry)
	selected, selectedErr := filepath.EvalSymlinks(path)
	if err != nil || selectedErr != nil || actual != selected {
		return false
	}
	root := filepath.Join(home, ".codex", "packages", "standalone")
	rel, err := filepath.Rel(root, selected)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
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
		if action == "install" && current.Installed {
			return current, errors.New("已找到 Codex，请使用更新")
		}
		if action == "update" {
			if !current.Installed {
				return current, errors.New("尚未安装 Codex，请先安装")
			}
			if current.Source == "brew-formula" || current.Source == "brew-cask" || current.Source == "npm" {
				source := runtimeinstall.Detect(ctx, current.Path, "codex", "@openai/codex")
				if source.Kind != current.Source {
					return current, errors.New("Codex 安装来源已变化，请重新检测后再更新")
				}
				if err := runtimeinstall.Upgrade(ctx, source); err != nil {
					return current, err
				}
				next, err := InspectRuntime(ctx, source.Entry)
				if err != nil || !next.Installed || next.Source != current.Source {
					return next, errors.New("更新后的 Codex 路径或版本未能验证，请重新检测并核对原安装渠道")
				}
				if order, err := updates.CompareVersions(next.Version, current.Version); err != nil || order < 0 {
					return next, errors.New("更新后的 Codex 版本低于原版本，请核对安装渠道")
				}
				if next.Version == current.Version {
					next.UpdateState = "current"
					next.Message = "原安装渠道当前没有更新版本"
				}
				return next, nil
			}
			if current.Source != "official" {
				return current, errors.New("无法确认 Codex 安装来源。请选择原程序并核对官方、Homebrew 或 npm 的安装记录后重试")
			}
		} else if path != "" {
			return current, errors.New("自动安装仅支持用户默认目录，请清除自定义程序路径后重试")
		}
		if current.Source == "official" && path != "" && !officialCodex(path) {
			return current, errors.New("自定义 Codex 路径与官方安装位置不一致，请重新检测")
		}
		if e = runtimeinstall.Codex(ctx); e != nil {
			return current, e
		}
		selected := ""
		if action == "update" {
			selected = current.Path
		}
		next, err := InspectRuntime(ctx, selected)
		if err != nil || !next.Installed || next.Source != "official" {
			return next, errors.New("Codex 官方安装完成后，程序路径或版本未能验证；请重新检测")
		}
		if action == "update" {
			if order, err := updates.CompareVersions(next.Version, current.Version); err != nil || order < 0 {
				return next, errors.New("Codex 更新后的版本低于原版本，请重新检测")
			}
		}
		return next, nil
	default:
		return current, errors.New("不支持的 Codex 安装操作")
	}
}
