package localstate

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsStateACLAndReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "资料", "state.json")
	for _, value := range []string{"first", "second"} {
		if err := WriteConfirmed(path, value); err != nil {
			t.Fatal(err)
		}
		if !IsPrivate(path) || !IsPrivate(filepath.Dir(path)) {
			t.Fatal("state not protected after replacement")
		}
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if IsPrivate(path) {
		t.Fatal("public ACL accepted as private")
	}
	if err := ProtectFile(path); err != nil {
		t.Fatal(err)
	}
	if !IsPrivate(path) {
		t.Fatal("repaired ACL not recognized")
	}
}

func TestWindowsPrivateProtectionRejectsHardlink(t *testing.T) {
	root := t.TempDir()
	original, linked := filepath.Join(root, "original"), filepath.Join(root, "linked")
	if err := os.WriteFile(original, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, linked); err != nil {
		t.Fatal(err)
	}
	if err := ProtectFile(linked); err == nil {
		t.Fatal("changed ACL through hardlink")
	}
	if IsPrivate(linked) {
		t.Fatal("multiply linked state accepted")
	}
	body, err := os.ReadFile(original)
	if err != nil || string(body) != "original" {
		t.Fatal("original changed")
	}
}
