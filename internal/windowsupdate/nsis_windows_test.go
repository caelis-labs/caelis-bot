package windowsupdate

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// Explicitly opted-in real Setup bytes; preserve an existing installation and
// registration. Exercise the same signed-plan/apply route used by the GUI.
func TestNativeNSISUpdateTransaction(t *testing.T) {
	installer := os.Getenv("CAELIS_BOT_TEST_INSTALLER")
	if installer == "" {
		t.Skip("explicit disposable preview Setup.exe")
	}
	installer, e := filepath.Abs(installer)
	if e != nil {
		t.Fatal(e)
	}
	var build struct {
		Version, Format, Channel, SHA256 string
		Signed                           bool
	}
	metadata, e := os.ReadFile(installer + ".build.json")
	if e != nil || json.Unmarshal(metadata, &build) != nil || build.Format != "nsis" || build.Channel != "preview" || build.Signed {
		t.Fatal("unsigned preview build receipt required")
	}
	data, e := os.ReadFile(installer)
	if e != nil || Digest(data) != build.SHA256 {
		t.Fatal("exact installer hash required")
	}
	target := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Caelis Bot Preview")
	if _, e := os.Lstat(target); !os.IsNotExist(e) {
		t.Fatal("preserve existing install root")
	}
	key := `Software\Microsoft\Windows\CurrentVersion\Uninstall\dev.caelis.bot.preview`
	if k, e := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE); e == nil {
		defer k.Close()
		if _, _, e := k.GetStringValue("dev.caelis.bot.preview"); !os.IsNotExist(e) && e != registry.ErrNotExist {
			t.Fatal("preserve existing preview startup entry")
		}
	}
	if k, e := registry.OpenKey(registry.CURRENT_USER, key, registry.READ); e == nil {
		k.Close()
		t.Fatal("preserve existing install registration")
	}
	root := filepath.Join(t.TempDir(), "更新 NSIS")
	download := filepath.Join(root, "Updates", "download-fixture")
	os.MkdirAll(download, 0700)
	// The previous payload is an owned disposable executable with a truthful
	// fixture version. No existing app, data or startup entry is adopted.
	os.MkdirAll(target, 0700)
	exe, _ := os.Executable()
	pe, _ := os.ReadFile(exe)
	os.WriteFile(filepath.Join(target, "caelis-bot.exe"), pe, 0600)
	os.WriteFile(filepath.Join(target, "installation.ini"), []byte("[CaelisBot]\nIdentity=dev.caelis.bot.preview\nVersion=0.9.0\n"), 0600)
	os.WriteFile(filepath.Join(target, "build.json"), []byte(`{"version":"0.9.0"}`), 0600)
	canary := filepath.Join(target, "update-canary.txt")
	os.WriteFile(canary, []byte("foreign-kept"), 0600)
	name := "Caelis-Bot-" + build.Version + "-windows-amd64-preview-Setup.exe"
	archive := filepath.Join(download, name)
	os.WriteFile(archive, data, 0600)
	manifest := Manifest{Schema: 1, Version: build.Version, Tag: "v" + build.Version, Channel: "preview", Platform: "windows-amd64", Source: strings.Repeat("a", 40), Qualified: true, Artifacts: []Artifact{{Format: "nsis", File: name, SHA256: Digest(data), Length: int64(len(data))}, {Format: "zip", File: "Caelis-Bot-" + build.Version + "-windows-amd64-preview.zip", SHA256: strings.Repeat("b", 64), Length: 10}}}
	pub, priv, _ := ed25519.GenerateKey(nil)
	mb, _ := json.Marshal(manifest)
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, mb)))
	plan := Plan{1, root, target, "dev.caelis.bot.preview", "0.9.0", "nsis", mb, sig, archive, true}
	config, _ := json.Marshal(plan)
	os.WriteFile(filepath.Join(root, "fixture.json"), config, 0600)
	// A separately opted-in fixture parent suppresses relaunch by writing a
	// control flag consumed only by TestMain; the production helper honors Plan.
	os.WriteFile(filepath.Join(root, "no-relaunch-fixture"), []byte("yes"), 0600)
	parent := exec.Command(exe)
	parent.Env = append(os.Environ(), "CAELIS_BOT_UPDATE_TEST_ROOT="+root, "CAELIS_BOT_UPDATE_TEST_KEY="+base64.StdEncoding.EncodeToString(pub))
	if b, e := parent.CombinedOutput(); e != nil {
		t.Fatalf("NSIS handoff failed: %v %s", e, b)
	}
	deadline := time.Now().Add(40 * time.Second)
	var receipt ApplyResult
	for {
		b, _ := os.ReadFile(filepath.Join(download, "result.json"))
		json.Unmarshal(b, &receipt)
		if receipt.State == "installed" || receipt.State == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("NSIS update unresolved: %+v", receipt)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if receipt.State != "installed" {
		t.Fatalf("NSIS update failed: %+v", receipt)
	}
	if v, e := installationVersion(target, "nsis"); e != nil || v != build.Version {
		t.Fatal("new native installed version missing", v, e)
	}
	if b, _ := os.ReadFile(canary); string(b) != "foreign-kept" {
		t.Fatal("foreign file changed")
	}
	uninstaller := exec.Command(filepath.Join(target, "Uninstall.exe"), "/S")
	if b, e := uninstaller.CombinedOutput(); e != nil {
		t.Fatalf("fixture cleanup failed: %v %s", e, b)
	}
	for {
		_, e := os.Stat(filepath.Join(target, "caelis-bot.exe"))
		if os.IsNotExist(e) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture uninstall cleanup incomplete")
		}
		time.Sleep(50 * time.Millisecond)
	}
	os.Remove(canary)
	os.Remove(target)
	t.Log("exact signed plan, helper handoff, per-user NSIS install, installed version and foreign-file retention passed")
}
