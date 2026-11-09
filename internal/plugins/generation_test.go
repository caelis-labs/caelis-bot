package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestGenerationCheckAlwaysReadsFreshBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	check := generationCheck{path: path, packageID: "fixture", generation: "version-a"}
	write := func(item installed) {
		t.Helper()
		body, err := json.Marshal(state{Installed: map[string]installed{"fixture": item}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	current := installed{Root: "version-a", Enabled: true}
	write(current)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !check.active() || !check.active() {
		t.Fatal("active generation rejected")
	}
	// Same-length replacement with the old mtime must revoke the cached root.
	current.Root = "version-b"
	write(current)
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if check.active() {
		t.Fatal("mtime or length reused stale generation authority")
	}
	current.Root = "version-a"
	write(current)
	if !check.active() {
		t.Fatal("fresh restored generation rejected")
	}
	current.Enabled = false
	write(current)
	if check.active() {
		t.Fatal("disabled generation accepted")
	}
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if check.active() {
		t.Fatal("invalid state did not fail closed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if check.active() {
		t.Fatal("missing state reused last successful check")
	}
	write(installed{Root: "version-a", Enabled: true})
	if !check.active() {
		t.Fatal("recreated state was not freshly observed")
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if check.active() {
		t.Fatal("uninstalled generation accepted")
	}
}

func TestGenerationCheckConcurrentReadersObserveAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	body, _ := json.Marshal(state{Installed: map[string]installed{"fixture": {Root: "generation", Enabled: true}}})
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	check := generationCheck{path: path, packageID: "fixture", generation: "generation"}
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for range 25 {
				if !check.active() {
					t.Error("unchanged generation rejected")
				}
			}
		})
	}
	readers.Wait()
	changed := path + ".new"
	if err := os.WriteFile(changed, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(changed, path); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		readers.Go(func() {
			if check.active() {
				t.Error("atomic uninstall accepted by an RPC/timer reader")
			}
		})
	}
	readers.Wait()
}

func BenchmarkGenerationCheck(b *testing.B) {
	path := filepath.Join(b.TempDir(), "state.json")
	saved := state{Installed: map[string]installed{}}
	for i := range 128 {
		saved.Installed[fmt.Sprint(i)] = installed{Version: "1.0.0", Digest: "synthetic-digest", Root: "generation", Enabled: true}
	}
	body, _ := json.Marshal(saved)
	if err := os.WriteFile(path, body, 0600); err != nil {
		b.Fatal(err)
	}
	b.Run("full_decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			body, err := os.ReadFile(path)
			var saved state
			if err != nil || json.Unmarshal(body, &saved) != nil || !saved.Installed["0"].Enabled {
				b.Fatal("fixture read failed")
			}
		}
	})
	b.Run("fresh_read_cached_decode", func(b *testing.B) {
		check := generationCheck{path: path, packageID: "0", generation: "generation"}
		b.ReportAllocs()
		for b.Loop() {
			if !check.active() {
				b.Fatal("fixture generation rejected")
			}
		}
	})
}
