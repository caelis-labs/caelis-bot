package desktop

import (
	"path/filepath"
	"testing"
)

func TestWindowsNotificationActivationKeepsTheResidentInstanceIdentity(t *testing.T) {
	base, err := nativeDataDirectoryBase()
	if err != nil {
		t.Fatal(err)
	}
	name, id := applicationIdentity()
	root := filepath.Join(base, name)
	t.Setenv("CAELIS_BOT_DATA_DIR", "")
	if got := applicationInstanceID(root); got != id {
		t.Fatal(got)
	}
	// Cold protocol activation carries the original profile explicitly.
	t.Setenv("CAELIS_BOT_DATA_DIR", root)
	if got := applicationInstanceID(root); got != id {
		t.Fatal("activation would launch a duplicate resident", got)
	}
	if buildChannel != "release" && applicationInstanceID(t.TempDir()) == id {
		t.Fatal("fixture collided with resident")
	}
}
