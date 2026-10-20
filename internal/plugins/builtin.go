package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// desktopWorldBundle reads the build-owned, fully staged plugin. Its inventory
// is generated only after Bot has signed both native executables. On macOS the
// app resource seal protects that inventory and the complete package together.
func desktopWorldBundle() (Entry, fs.FS, bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return Entry{}, nil, false, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return Entry{}, nil, false, err
	}
	base := filepath.Dir(executable)
	app := ""
	if runtime.GOOS == "darwin" {
		marker := ".app" + string(filepath.Separator) + "Contents" + string(filepath.Separator) + "MacOS"
		index := strings.LastIndex(base, marker)
		if index < 0 || index+len(marker) != len(base) {
			return Entry{}, nil, false, nil // source tests and auxiliary binaries
		}
		app = base[:index+len(".app")]
		base = filepath.Join(app, "Contents", "Resources")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return Entry{}, nil, false, nil
	}
	pluginsRoot := filepath.Join(base, "Plugins")
	packageRoot := filepath.Join(pluginsRoot, "desktop-world")
	info, err := os.Lstat(packageRoot)
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, nil, false, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Entry{}, nil, false, errors.New("invalid built-in Desktop World package")
	}
	if app != "" {
		if err := exec.Command("/usr/bin/codesign", "--verify", "--strict", app).Run(); err != nil {
			return Entry{}, nil, false, fmt.Errorf("built-in Desktop World app seal: %w", err)
		}
	}
	body, err := os.ReadFile(filepath.Join(pluginsRoot, "desktop-world-inventory.json"))
	if err != nil {
		return Entry{}, nil, false, err
	}
	entry, err := readDesktopWorldInventory(packageRoot, body)
	if err != nil {
		return Entry{}, nil, false, err
	}
	return entry, os.DirFS(packageRoot), true, nil
}

var desktopWorldReleaseVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func readDesktopWorldInventory(packageRoot string, body []byte) (Entry, error) {
	var entry Entry
	if json.Unmarshal(body, &entry) != nil || entry.ID != "desktop-world" || !desktopWorldReleaseVersion.MatchString(entry.Version) || entry.Source != "bundled:desktop-world/v"+entry.Version || !entry.Builtin || len(entry.Files) == 0 {
		return Entry{}, errors.New("invalid built-in Desktop World inventory")
	}
	if err := Verify(packageRoot, entry.Files); err != nil {
		return Entry{}, err
	}
	if err := verifyExecutableModes(packageRoot, entry); err != nil {
		return Entry{}, err
	}
	p, err := Load(packageRoot)
	if err != nil {
		return Entry{}, err
	}
	if p.Manifest.Name != entry.ID || p.Manifest.Version != entry.Version {
		return Entry{}, errors.New("built-in Desktop World identity differs from inventory")
	}
	return entry, nil
}
