package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStageExecutableFromRunningImageCanBeRemoved(t *testing.T) {
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	target := filepath.Join(root, "fixture.exe")
	if err := StageExecutable(source, target); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(original, staged) || original.Size() != staged.Size() {
		t.Fatal("staged executable must have its own file identity and the original size")
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal("remove fixture while original image is mapped:", err)
	}
}
