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

func TestWindowsPrivateDefaultOwnerNormalization(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state")
	if err := os.WriteFile(path, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Equals(user.User.Sid) {
		if !windows.GetCurrentProcessToken().IsElevated() || !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			t.Fatal("unexpected default owner for test fixture")
		}
		if IsPrivate(path) {
			t.Fatal("group owner passed individual-user attestation")
		}
		after, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		afterOwner, _, err := after.Owner()
		if err != nil || !afterOwner.Equals(owner) {
			t.Fatal("read-only attestation changed the owner", err)
		}
		t.Log("verifying elevated default Administrators owner normalization")
	}
	if err := ProtectDirectory(root); err != nil {
		t.Fatal(err)
	}
	if err := ProtectFile(path); err != nil {
		t.Fatal(err)
	}
	if !IsPrivate(root) || !IsPrivate(path) {
		t.Fatal("protection did not establish individual-user ownership and ACL")
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "retained" {
		t.Fatal("protection changed file contents", err)
	}
}

func TestWindowsPrivateOwnerClaimRejectsOtherIdentities(t *testing.T) {
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name              string
		owner, tokenOwner *windows.SID
		want              bool
	}{
		{"default-admin-group", admins, admins, true},
		{"foreign-owner", system, admins, false},
		{"admin-not-token-default", admins, system, false},
		{"other-default-identity", system, system, false},
		{"missing-owner", nil, admins, false},
		{"missing-token-owner", admins, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimablePrivateOwner(tc.owner, tc.tokenOwner); got != tc.want {
				t.Fatalf("owner claim allowed = %v, want %v", got, tc.want)
			}
		})
	}
}
