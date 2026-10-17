package windowsupdate

import (
	"archive/zip"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// These switches exist only in the test executable. The shipping binary always
// uses the embedded public key. Real child processes exercise inherited owner
// handles, handoff, image locks, replacement and relaunch without a public feed.
func TestMain(m *testing.M) {
	if key := os.Getenv("CAELIS_BOT_UPDATE_TEST_KEY"); key != "" {
		publicKey = key
	}
	if handled, err := Run(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if root := os.Getenv("CAELIS_BOT_UPDATE_TEST_ROOT"); root != "" {
		if filepath.Base(os.Args[0]) == "caelis-bot.exe" {
			os.WriteFile(filepath.Join(root, "relaunched"), []byte("yes"), 0600)
			os.Exit(0)
		}
		var p Plan
		data, err := os.ReadFile(filepath.Join(root, "fixture.json"))
		if err != nil || json.Unmarshal(data, &p) != nil {
			os.Exit(2)
		}
		key, _ := Key()
		manifest, err := Verify(p.Manifest, p.Signature, key)
		if err != nil {
			os.Exit(3)
		}
		offer := &Offer{Manifest: manifest, Bytes: p.Manifest, Signature: p.Signature}
		for _, a := range manifest.Artifacts {
			if a.Format == p.Format {
				offer.Artifact = a
			}
		}
		_, noRelaunch := os.Stat(filepath.Join(root, "no-relaunch-fixture"))
		dir, err := start(p.Root, p.Target, p.Identity, p.Current, offer, p.Archive, os.IsNotExist(noRelaunch))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		os.WriteFile(filepath.Join(root, "acknowledged"), []byte(dir), 0600)
		if _, err := os.Stat(filepath.Join(root, "uncommitted-fixture")); os.IsNotExist(err) {
			if err := Commit(dir, manifest.Version); err != nil {
				os.Exit(5)
			}
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNativeWindowsUpdateHandoffReplacementRelaunchAndLiveImageRefusal(t *testing.T) {
	for _, scenario := range []struct{ inUse, uncommitted bool }{{false, false}, {true, false}, {false, true}} {
		t.Run(fmt.Sprintf("imageInUse=%v/uncommitted=%v", scenario.inUse, scenario.uncommitted), func(t *testing.T) {
			inUse := scenario.inUse
			root := filepath.Join(t.TempDir(), "更新 验收")
			target := filepath.Join(root, "ZIP")
			download := filepath.Join(root, "Updates", "download-fixture")
			if e := os.MkdirAll(target, 0700); e != nil {
				t.Fatal(e)
			}
			os.MkdirAll(download, 0700)
			if scenario.uncommitted {
				os.WriteFile(filepath.Join(root, "uncommitted-fixture"), []byte("simulate unsuccessful shutdown"), 0600)
			}
			exe, _ := os.Executable()
			pe, e := os.ReadFile(exe)
			if e != nil {
				t.Fatal(e)
			}
			portableFixture(t, target, "1.0.0", map[string][]byte{"caelis-bot.exe": pe, "Resources/obsolete.txt": []byte("old")})
			os.WriteFile(filepath.Join(target, "personal.txt"), []byte("foreign-kept"), 0600)
			os.WriteFile(filepath.Join(root, "Notebook.txt"), []byte("data-kept"), 0600)
			stage := filepath.Join(root, "next")
			os.Mkdir(stage, 0700)
			portableFixture(t, stage, "1.0.1", map[string][]byte{"caelis-bot.exe": pe})
			archiveName := "Caelis-Bot-1.0.1-windows-amd64-preview.zip"
			archive := filepath.Join(download, archiveName)
			file, e := os.Create(archive)
			if e != nil {
				t.Fatal(e)
			}
			z := zip.NewWriter(file)
			e = filepath.WalkDir(stage, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				name, _ := filepath.Rel(stage, path)
				entry, err := z.Create(filepath.ToSlash(name))
				if err != nil {
					return err
				}
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				_, err = entry.Write(b)
				return err
			})
			if e != nil {
				t.Fatal(e)
			}
			z.Close()
			file.Close()
			b, _ := os.ReadFile(archive)
			manifest := Manifest{Schema: 1, Version: "1.0.1", Tag: "v1.0.1", Channel: "preview", Platform: "windows-amd64", Source: strings.Repeat("a", 40), Qualified: true, Artifacts: []Artifact{{Format: "zip", File: archiveName, SHA256: Digest(b), Length: int64(len(b))}, {Format: "nsis", File: "Caelis-Bot-1.0.1-windows-amd64-preview-Setup.exe", SHA256: strings.Repeat("b", 64), Length: 10}}}
			pub, priv, _ := ed25519.GenerateKey(nil)
			mb, _ := json.Marshal(manifest)
			sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, mb)))
			plan := Plan{1, root, target, "dev.caelis.bot.preview", "1.0.0", "zip", mb, sig, archive, true}
			config, _ := json.Marshal(plan)
			os.WriteFile(filepath.Join(root, "fixture.json"), config, 0600)
			if inUse {
				app, _ := windows.UTF16PtrFromString(filepath.Join(target, "caelis-bot.exe"))
				var si windows.StartupInfo
				si.Cb = uint32(unsafe.Sizeof(si))
				var pi windows.ProcessInformation
				if e := windows.CreateProcess(app, nil, nil, nil, false, windows.CREATE_SUSPENDED|windows.CREATE_NO_WINDOW, nil, nil, &si, &pi); e != nil {
					t.Fatal(e)
				}
				defer windows.CloseHandle(pi.Thread)
				defer windows.CloseHandle(pi.Process)
				defer func() { windows.TerminateProcess(pi.Process, 0); windows.WaitForSingleObject(pi.Process, 5000) }()
			}
			parent := exec.Command(exe)
			parent.Env = append(os.Environ(), "CAELIS_BOT_UPDATE_TEST_ROOT="+root, "CAELIS_BOT_UPDATE_TEST_KEY="+base64.StdEncoding.EncodeToString(pub))
			if b, e := parent.CombinedOutput(); e != nil {
				t.Fatalf("handoff failed: %v %s", e, b)
			}
			var receipt ApplyResult
			deadline := time.Now().Add(20 * time.Second)
			for {
				data, _ := os.ReadFile(filepath.Join(download, "result.json"))
				json.Unmarshal(data, &receipt)
				_, relaunched := os.Stat(filepath.Join(root, "relaunched"))
				if receipt.State == "failed" || receipt.State == "installed" && relaunched == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("update did not finish: %+v", receipt)
				}
				time.Sleep(50 * time.Millisecond)
			}
			if _, e := os.Stat(filepath.Join(root, "acknowledged")); e != nil {
				t.Fatal("helper did not acknowledge exact parent")
			}
			p, e := readPortable(filepath.Join(target, "portable.json"))
			if e != nil {
				t.Fatal(e)
			}
			if inUse || scenario.uncommitted {
				if receipt.State != "failed" || p.Version != "1.0.0" {
					t.Fatalf("live image not refused: %+v %+v", receipt, p)
				}
			} else {
				if receipt.State != "installed" || p.Version != "1.0.1" {
					t.Fatalf("replacement failed: %+v %+v", receipt, p)
				}
				if _, e := os.Stat(filepath.Join(target, "Resources/obsolete.txt")); !os.IsNotExist(e) {
					t.Fatal("obsolete inventory remained")
				}
			}
			for path, want := range map[string]string{filepath.Join(target, "personal.txt"): "foreign-kept", filepath.Join(root, "Notebook.txt"): "data-kept"} {
				got, _ := os.ReadFile(path)
				if string(got) != want {
					t.Fatal("foreign data changed")
				}
			}
			keyBefore := publicKey
			publicKey = base64.StdEncoding.EncodeToString(pub)
			defer func() { publicKey = keyBefore }()
			var artifact Artifact
			for _, a := range manifest.Artifacts {
				if a.Format == "zip" {
					artifact = a
				}
			}
			if _, e := Start(root, target, plan.Identity, p.Version, &Offer{manifest, artifact, mb, sig}, archive); e == nil {
				t.Fatal("original transaction was replayed")
			}
		})
	}
}
