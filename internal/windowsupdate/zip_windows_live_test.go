//go:build windows

package windowsupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsZIPPackageLifecycle(t *testing.T) {
	archive, root := os.Getenv("CAELIS_BOT_TEST_ZIP"), os.Getenv("CAELIS_BOT_TEST_ZIP_DIRECTORY")
	if archive == "" || root == "" {
		t.Skip("explicit disposable ZIP and fresh evidence directory")
	}
	if !filepath.IsAbs(archive) || !filepath.IsAbs(root) {
		t.Fatal("absolute fixture paths required")
	}
	if _, e := os.Lstat(root); !os.IsNotExist(e) {
		t.Fatal("preserve existing ZIP evidence directory")
	}
	var build struct {
		Version, Format, Channel, SHA256, Source string
		Signed                                   bool
	}
	b, e := os.ReadFile(archive + ".build.json")
	if e != nil || json.Unmarshal(b, &build) != nil || build.Format != "zip" || build.Channel != "preview" || build.Signed {
		t.Fatal("unsigned preview ZIP required")
	}
	info, e := os.Stat(archive)
	if e != nil {
		t.Fatal(e)
	}
	if e := VerifyArchive(archive, Artifact{SHA256: build.SHA256, Length: info.Size()}); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(root, "免安装 中文")
	p, e := Unpack(archive, target, "dev.caelis.bot.preview", build.Version)
	if e != nil {
		t.Fatal(e)
	}
	if format, e := Installation(target, p.Identity); e != nil || format != "zip" {
		t.Fatal("ZIP updater ownership unavailable", e)
	}
	exe := filepath.Join(target, "caelis-bot.exe")
	app, _ := windows.UTF16PtrFromString(exe)
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if e := windows.CreateProcess(app, nil, nil, nil, false, windows.CREATE_SUSPENDED|windows.CREATE_NO_WINDOW, nil, nil, &si, &pi); e != nil {
		t.Fatal(e)
	}
	func() {
		defer windows.CloseHandle(pi.Thread)
		defer windows.CloseHandle(pi.Process)
		defer func() { windows.TerminateProcess(pi.Process, 0); windows.WaitForSingleObject(pi.Process, 5000) }()
		if release, e := stoppedLease(exe); e == nil {
			release()
			t.Fatal("real packaged live image accepted for replacement")
		}
	}()
	stage := filepath.Join(root, "replacement")
	if _, e := Unpack(archive, stage, p.Identity, build.Version); e != nil {
		t.Fatal(e)
	}
	canary := filepath.Join(target, "user-file.txt")
	os.WriteFile(canary, []byte("kept"), 0600)
	data := filepath.Join(root, "Notebook-canary.txt")
	os.WriteFile(data, []byte("outside-kept"), 0600)
	release, e := stoppedLease(exe)
	if e != nil {
		t.Fatal(e)
	}
	e = ReplacePortable(target, stage, filepath.Join(root, "backup"), p.Identity, build.Version)
	release()
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range p.Files {
		if e := verifyFile(filepath.Join(target, filepath.FromSlash(f.Name)), f); e != nil {
			t.Fatal(e)
		}
	}
	if b, _ := os.ReadFile(canary); string(b) != "kept" {
		t.Fatal("unowned ZIP file changed")
	}
	if b, _ := os.ReadFile(data); string(b) != "outside-kept" {
		t.Fatal("external data changed")
	}
	receipt := map[string]any{"schema": 1, "format": "zip", "source": build.Source, "sha256": build.SHA256, "qualified": false, "programDirectory": target, "checks": map[string]bool{"extraction": true, "allInventoryHashes": true, "unicodePath": true, "ownedReplacement": true, "liveImageRefused": true, "foreignDataPreserved": true}}
	b, _ = json.MarshalIndent(receipt, "", "  ")
	if e := os.WriteFile(filepath.Join(root, "acceptance.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	t.Logf("ZIP inventory (%d files), Unicode location, owned replacement, live image refusal and data retention passed", len(p.Files))
}
