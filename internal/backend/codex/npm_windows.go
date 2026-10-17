package codex

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Resolve only the selected npm installation; never execute a .cmd/.ps1 shim
// through a shell or change which installation owns the App Server process.
func installedWindowsBinary(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	var roots []string
	switch strings.ToLower(filepath.Base(real)) {
	case "codex.js":
		roots = []string{filepath.Dir(filepath.Dir(real))}
	case "codex.cmd", "codex.ps1":
		dir := filepath.Dir(real)
		if strings.EqualFold(filepath.Base(dir), ".bin") {
			roots = []string{filepath.Join(filepath.Dir(dir), "@openai", "codex")}
		} else {
			roots = []string{filepath.Join(dir, "node_modules", "@openai", "codex")}
		}
	default:
		return path
	}
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	triple := map[string]string{"amd64": "x86_64-pc-windows-msvc", "arm64": "aarch64-pc-windows-msvc"}[runtime.GOARCH]
	if triple == "" {
		return path
	}
	for _, root := range roots {
		if !npmPackageNamed(root, "@openai/codex") {
			continue
		}
		packageName := "codex-win32-" + arch
		bases := []string{filepath.Join(root, "node_modules", "@openai", packageName), filepath.Join(filepath.Dir(root), packageName)}
		vendors := []string{}
		for _, base := range bases {
			if npmPlatformPackage(base, "@openai/"+packageName, arch) {
				vendors = append(vendors, filepath.Join(base, "vendor"))
			}
		}
		vendors = append(vendors, filepath.Join(root, "vendor"))
		for _, vendor := range vendors {
			// Current packages use bin; earlier npm releases used codex.
			for _, sub := range []string{"bin", "codex"} {
				if binary, e := exec.LookPath(filepath.Join(vendor, triple, sub, "codex.exe")); e == nil {
					return binary
				}
			}
		}
	}
	return path
}
func npmPlatformPackage(root, name, arch string) bool {
	b, err := os.ReadFile(filepath.Join(root, "package.json"))
	var pkg struct {
		Name string   `json:"name"`
		OS   []string `json:"os"`
		CPU  []string `json:"cpu"`
	}
	if err != nil || len(b) > 1<<20 || json.Unmarshal(b, &pkg) != nil {
		return false
	}
	// npm aliases retain the original package name (@openai/codex), not the
	// codex-win32-x64 dependency key. Validate its platform metadata as well.
	return pkg.Name == name || pkg.Name == "@openai/codex" && slices.Contains(pkg.OS, "win32") && slices.Contains(pkg.CPU, arch)
}
func npmPackageNamed(root, name string) bool {
	b, err := os.ReadFile(filepath.Join(root, "package.json"))
	var pkg struct {
		Name string `json:"name"`
	}
	return err == nil && len(b) <= 1<<20 && json.Unmarshal(b, &pkg) == nil && pkg.Name == name
}
