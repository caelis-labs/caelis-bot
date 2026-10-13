package runtimeinstall

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

// Source is established from the actual executable and the package manager's
// installed root. A matching name on PATH alone is never installation proof.
type Source struct {
	Kind    string // brew-formula, brew-cask, npm, or unknown
	Manager string
	Package string
	Prefix  string
	Entry   string
}

func canonical(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}
func within(root, target string) bool {
	rel, err := filepath.Rel(canonical(root), canonical(target))
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
func query(ctx context.Context, executable string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = runtimeenv.Clean(os.Environ())
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil || len(output) > 4096 {
		return ""
	}
	return strings.TrimSpace(string(output))
}
func listed(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == name {
			return true
		}
	}
	return false
}
func packageName(path string, expected string) bool {
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil || len(data) > 1<<20 {
		return false
	}
	var manifest struct {
		Name string `json:"name"`
	}
	return json.Unmarshal(data, &manifest) == nil && manifest.Name == expected
}

// Detect is read-only. It requires the selected binary to live in the exact
// installed package root, and checks npm's global prefix and bin entrypoint.
func Detect(ctx context.Context, binary, name, npmPackage string) Source {
	source := Source{Kind: "unknown", Package: name, Entry: binary}
	real := canonical(binary)
	if brew, err := exec.LookPath("brew"); err == nil {
		prefix := query(ctx, brew, "--prefix")
		if filepath.IsAbs(prefix) {
			formula := within(filepath.Join(prefix, "Cellar", name), real) && listed(query(ctx, brew, "list", "--formula", "--versions", name), name)
			cask := within(filepath.Join(prefix, "Caskroom", name), real) && listed(query(ctx, brew, "list", "--cask", "--versions", name), name)
			if formula != cask {
				source.Kind = "brew-formula"
				if cask {
					source.Kind = "brew-cask"
				}
				source.Manager, source.Prefix, source.Entry = brew, prefix, filepath.Join(prefix, "bin", name)
				if _, err := os.Stat(source.Entry); err != nil || !within(prefix, source.Entry) {
					source.Kind = "unknown"
				}
				return source
			}
		}
	}
	if npm, err := exec.LookPath("npm"); err == nil && npmPackage != "" {
		root, prefix := query(ctx, npm, "root", "-g"), query(ctx, npm, "prefix", "-g")
		packageDir := filepath.Join(root, filepath.FromSlash(npmPackage))
		entry := filepath.Join(prefix, "bin", name)
		if filepath.IsAbs(root) && filepath.IsAbs(prefix) && packageName(packageDir, npmPackage) && within(packageDir, real) && within(packageDir, entry) {
			source.Kind, source.Manager, source.Package, source.Prefix, source.Entry = "npm", npm, npmPackage, prefix, entry
			return source
		}
	}
	return source
}

// Upgrade keeps the original package manager and global installation prefix.
// Output is discarded because package managers may print private configuration.
func Upgrade(ctx context.Context, source Source) error {
	if source.Manager == "" || source.Kind == "unknown" {
		return errors.New("无法确认安装来源，请选择现有程序或核对原安装渠道")
	}
	var args []string
	switch source.Kind {
	case "brew-formula":
		args = []string{"upgrade", "--formula", source.Package}
	case "brew-cask":
		args = []string{"upgrade", "--cask", source.Package}
	case "npm":
		if query(ctx, source.Manager, "prefix", "-g") != source.Prefix || query(ctx, source.Manager, "root", "-g") == "" {
			return errors.New("npm 全局目录已变化，请核对 Node 与 npm 的安装位置后重试")
		}
		args = []string{"update", "-g", source.Package, "--no-audit", "--no-fund", "--yes"}
	default:
		return errors.New("无法确认安装来源，请核对原安装渠道")
	}
	command := exec.CommandContext(ctx, source.Manager, args...)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.Env = append(runtimeenv.Clean(os.Environ()), "CI=1")
	if source.Kind == "npm" {
		command.Env = append(command.Env, "npm_config_prefix="+source.Prefix)
		command.Env = append(command.Env, "PATH="+filepath.Dir(source.Manager)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return errors.New("更新已中断；请重新检测版本，确认结果后再重试")
		}
		if source.Kind == "npm" {
			return errors.New("npm 更新未完成；请检查当前 Node、全局目录权限与 npm 配置，再重新检测")
		}
		return errors.New("Homebrew 更新未完成；请检查所选 formula/cask 的安装状态与目录权限，再重新检测")
	}
	return nil
}
