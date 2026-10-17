package codex

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWindowsNPMLongTempNativeHandshake(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX_BINARY")
	if binary == "" {
		t.Skip("explicit native Codex acceptance")
	}
	prefix := filepath.Join(t.TempDir(), "npm 中文")
	root := filepath.Join(prefix, "node_modules", "@openai", "codex")
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	triple := map[string]string{"amd64": "x86_64-pc-windows-msvc", "arm64": "aarch64-pc-windows-msvc"}[runtime.GOARCH]
	platformPackage := filepath.Join(root, "node_modules", "@openai", "codex-win32-"+arch)
	native := filepath.Join(platformPackage, "vendor", triple, "bin", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(native), 0700); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(native, b, 0700); err != nil {
		t.Fatal(err)
	}
	native, err = filepath.EvalSymlinks(native)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"@openai/codex"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(platformPackage, "package.json"), []byte(`{"name":"@openai/codex","os":["win32"],"cpu":["`+arch+`"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(prefix, "codex.cmd")
	if err = os.WriteFile(shim, []byte("@exit /b 99\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	longTemp := filepath.Join(t.TempDir(), strings.Repeat("长目录", 20))
	if err = os.Mkdir(longTemp, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEMP", longTemp)
	t.Setenv("TMP", longTemp)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	status, err := InspectRuntime(ctx, shim)
	if err != nil || !status.Installed || status.Path != native {
		t.Fatal("npm version detection", status, err)
	}
	client, err := Start(ctx, Options{Binary: shim, Directory: prefix, CLIOnly: true, Attachable: true})
	if err != nil {
		t.Fatal("npm native socket handshake", err)
	}
	defer client.Close()
	if endpoint := strings.TrimPrefix(client.retainedSocket(), "unix://"); endpoint == "" || len(endpoint) > 107 {
		t.Fatal("unbounded endpoint")
	}
}

func TestWindowsNPMShimResolvesNativeInstallation(t *testing.T) {
	for _, layout := range []string{"nested", "hoisted", "legacy", "local", "npm-alias"} {
		t.Run(layout, func(t *testing.T) {
			prefix := filepath.Join(t.TempDir(), "中文 npm path")
			root := filepath.Join(prefix, "node_modules", "@openai", "codex")
			shim := filepath.Join(prefix, "codex.cmd")
			if layout == "local" {
				shim = filepath.Join(prefix, "node_modules", ".bin", "codex.cmd")
			}
			arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
			triple := map[string]string{"amd64": "x86_64-pc-windows-msvc", "arm64": "aarch64-pc-windows-msvc"}[runtime.GOARCH]
			optional := filepath.Join(root, "node_modules", "@openai", "codex-win32-"+arch)
			if layout == "hoisted" {
				optional = filepath.Join(filepath.Dir(root), "codex-win32-"+arch)
			}
			binary := filepath.Join(optional, "vendor", triple, "bin", "codex.exe")
			if layout == "legacy" {
				binary = filepath.Join(root, "vendor", triple, "codex", "codex.exe")
			}
			files := map[string]string{shim: "@echo off\r\n", filepath.Join(root, "package.json"): `{"name":"@openai/codex"}`, filepath.Join(optional, "package.json"): `{"name":"@openai/codex-win32-` + arch + `"}`, binary: "native fixture"}
			if layout == "npm-alias" {
				files[filepath.Join(optional, "package.json")] = `{"name":"@openai/codex","os":["win32"],"cpu":["` + arch + `"]}`
			}
			for path, body := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := filepath.EvalSymlinks(binary)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := runtimeBinary(shim); err != nil || got != want {
				t.Fatal("native npm discovery", got, err)
			}
			os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"foreign"}`), 0600)
			if got := installedBinary(shim); got != shim {
				t.Fatal("foreign package adopted", got)
			}
		})
	}
}
