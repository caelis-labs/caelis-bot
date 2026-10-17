// Package testutil supplies native rejection fixtures without requiring an
// administrator or changing the machine's Developer Mode setting.
package testutil

import (
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/dirlink"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"golang.org/x/sys/windows"
)

// Redirect tests refusal of redirected paths. A directory uses an NTFS
// junction. A file-shaped path uses a junction to its parent instead: this is
// a native reparse rejection fixture, not evidence for privileged file symlinks.
// Never use it in tests that need to follow a link or assert its exact target.
func Redirect(target, link string) error {
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		target = filepath.Dir(target)
	}
	return dirlink.Create(target, link)
}

// SetMode models private/public read access for permission rejection tests.
// It deliberately does not model executable bits or a read-only owner.
func SetMode(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if mode.Perm()&0077 == 0 {
		if info.IsDir() {
			return localstate.ProtectDirectory(path)
		}
		return localstate.ProtectFile(path)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;WD)")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
