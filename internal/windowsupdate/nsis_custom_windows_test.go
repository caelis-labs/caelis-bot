package windowsupdate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Opt in with an exact local installer using the isolated Dev identity (including
// untagged stable/preview candidates). All files belong to this test;
// refuse to run if the user already has a registered Dev installation.
func TestNativeNSISCustomDirectory(t *testing.T) {
	installer := os.Getenv("CAELIS_BOT_TEST_LOCAL_INSTALLER")
	if installer == "" {
		t.Skip("explicit disposable local Setup.exe with Dev identity required")
	}
	installer, err := filepath.Abs(installer)
	if err != nil {
		t.Fatal(err)
	}
	var build struct {
		Version, Format, Channel, SHA256 string
		Qualified, Signed                bool
	}
	metadata, err := os.ReadFile(installer + ".build.json")
	if err != nil || json.Unmarshal(metadata, &build) != nil || build.Format != "nsis" || build.Qualified || build.Signed ||
		!(build.Channel == "development" && strings.HasSuffix(build.Version, "-dev") ||
			(build.Channel == "stable" || build.Channel == "preview") && strings.HasSuffix(build.Version, "-local")) {
		t.Fatal("untagged local installer receipt with Dev identity required")
	}
	data, err := os.ReadFile(installer)
	if err != nil || Digest(data) != build.SHA256 {
		t.Fatal("exact installer hash required")
	}
	const identity = "dev.caelis.bot.dev"
	const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + identity
	if key, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.READ); err == nil {
		key.Close()
		t.Fatal("preserve existing Dev install registration")
	} else if err != registry.ErrNotExist {
		t.Fatal(err)
	}
	root := t.TempDir()
	target := filepath.Join(root, "自选目录 App")
	plan := Plan{Target: target, Archive: installer, Format: "nsis", Identity: identity}
	manifest := Manifest{Version: build.Version}
	foreignWritten := false
	// Use the native uninstaller before t.TempDir removes its disposable root.
	// Register cleanup before install, so partial failure cannot leave registration.
	t.Cleanup(func() {
		uninstaller := filepath.Join(target, "Uninstall.exe")
		if _, err := os.Stat(uninstaller); err == nil {
			cmd := exec.Command(uninstaller)
			cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW,
				CmdLine: windows.ComposeCommandLine([]string{uninstaller, "/S"}) + " _?=" + target}
			if err := cmd.Run(); err != nil {
				t.Error("native uninstall:", err)
			}
		}
		if _, err := os.Stat(filepath.Join(target, "caelis-bot.exe")); !os.IsNotExist(err) {
			t.Error("uninstall left program executable")
		}
		if b, _ := os.ReadFile(filepath.Join(target, "foreign.txt")); foreignWritten && string(b) != "preserve" {
			t.Error("uninstall changed foreign file")
		}
		if key, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.READ); err == nil {
			key.Close()
			t.Error("uninstall left registration")
		}
	})
	if err := apply(plan, manifest, root); err != nil {
		t.Fatal("custom directory install:", err)
	}
	if format, err := Installation(target, identity); err != nil || format != "nsis" {
		t.Fatal("Unicode installation marker:", format, err)
	}
	if err := os.WriteFile(filepath.Join(target, "foreign.txt"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	foreignWritten = true
	if err := apply(plan, manifest, root); err != nil {
		t.Fatal("update at custom location:", err)
	}
	// Manual reinstall without /D must also remember the user's directory.
	if err := exec.Command(installer, "/S").Run(); err != nil {
		t.Fatal("remembered location:", err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.READ)
	if err != nil {
		t.Fatal(err)
	}
	location, _, err := key.GetStringValue("InstallLocation")
	key.Close()
	if err != nil || !strings.EqualFold(location, target) {
		t.Fatal("registration lost custom directory", location, err)
	}
	moved := plan
	moved.Target = filepath.Join(root, "another folder")
	if err := apply(moved, manifest, root); err == nil {
		t.Fatal("installer silently moved a registered installation")
	}
	if _, err := os.Stat(moved.Target); !os.IsNotExist(err) {
		t.Fatal("rejected location was modified")
	}
	t.Log("custom Unicode path, updater /D, remembered directory, move rejection and native uninstall verified")
}
