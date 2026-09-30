package workerwire

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOriginPairIsImmutableAcrossRestartAndCannotAdoptOldTasks(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	pair := testPair()
	if err = BindPair(directory, pair, true); err == nil {
		t.Fatal("foreign assembly adopted unpaired retained tasks")
	}
	if _, err = os.Stat(filepath.Join(directory, "worker-pair.json")); !os.IsNotExist(err) {
		t.Fatal("failed pairing published metadata", err)
	}
	if err = BindPair(directory, pair, false); err != nil {
		t.Fatal(err)
	}
	if err = BindPair(directory, pair, true); err != nil {
		t.Fatal("original pair lost on restart", err)
	}
	path := filepath.Join(directory, "worker-pair.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Pair){func(p *Pair) { p.BotID = "new-bot" }, func(p *Pair) { p.SourceNode = "new-source" }, func(p *Pair) { p.SourceBackend = "caelis" }, func(p *Pair) { p.Target.NodeID = "new-node" }} {
		changed := pair
		change(&changed)
		if err = BindPair(directory, changed, false); err == nil {
			t.Fatal("pairing identity changed")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(original) {
			t.Fatal("changed pair overwrote original", err)
		}
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err = BindPair(directory, pair, false); err == nil {
		t.Fatal("public pairing metadata admitted")
	}
}
