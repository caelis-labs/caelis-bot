package caelis

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func protectHostFixture(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsHostCredentialACLContract(t *testing.T) {
	file := filepath.Join(t.TempDir(), "auth.token")
	if err := os.WriteFile(file, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	protectHostFixture(t, file)
	if body, err := readHostFile(file, 64); err != nil || string(body) != "synthetic" {
		t.Fatal("Core-owned ACL not accepted", err)
	}
	if _, err := readHostFile(file, 2); err == nil {
		t.Fatal("unbounded read")
	}
	alias := filepath.Join(filepath.Dir(file), "alias")
	if err := os.Link(file, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := readHostFile(file, 64); err == nil {
		t.Fatal("multiply linked token accepted")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	// A read grant to Everyone must be rejected even when the owner and the
	// existing Core grants are unchanged. This requires no symlink privilege.
	user, _ := windows.GetCurrentProcessToken().GetTokenUser()
	sd, _ := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;WD)")
	acl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(file, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := readHostFile(file, 64); err == nil {
		t.Fatal("public token accepted")
	}
}
