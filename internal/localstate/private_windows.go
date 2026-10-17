package localstate

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func EnsurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return ProtectDirectory(path)
}

// Open the final object itself, never a reparse target. Protect/check through
// that handle so replacing the path cannot redirect the security operation.
func privateHandle(path string, write bool) (windows.Handle, *windows.Tokenuser, error) {
	if !filepath.IsAbs(path) {
		return 0, nil, errors.New("private path must be absolute")
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, nil, err
	}
	token := windows.GetCurrentProcessToken()
	elevated := write && token.IsElevated()
	access := uint32(windows.READ_CONTROL | windows.FILE_READ_ATTRIBUTES)
	if write {
		access |= windows.WRITE_DAC
	}
	if elevated {
		access |= windows.WRITE_OWNER
	}
	h, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, nil, err
	}
	fail := func(err error) (windows.Handle, *windows.Tokenuser, error) {
		windows.CloseHandle(h)
		return 0, nil, err
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		return fail(err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.NumberOfLinks > 1 {
		return fail(errors.New("private path is redirected or multiply linked"))
	}
	user, err := token.GetTokenUser()
	if err != nil {
		return fail(err)
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fail(err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return fail(errors.New("private path owner mismatch"))
	}
	if !owner.Equals(user.User.Sid) {
		// Elevated Windows processes can create objects owned by the token's
		// default Administrators group rather than TokenUser. Only protection
		// may normalize that exact default owner, through this checked handle.
		// Read-only attestation still requires the individual user's SID.
		var buffer [256]byte // TOKEN_OWNER pointer and its SID (at most 68 bytes).
		var size uint32
		if !elevated || windows.GetTokenInformation(token, windows.TokenOwner, &buffer[0], uint32(len(buffer)), &size) != nil ||
			!claimablePrivateOwner(owner, *(**windows.SID)(unsafe.Pointer(&buffer[0]))) {
			return fail(errors.New("private path owner mismatch"))
		}
		if err := windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, user.User.Sid, nil, nil, nil); err != nil {
			return fail(err)
		}
	}
	return h, user, nil
}

func claimablePrivateOwner(owner, defaultOwner *windows.SID) bool {
	return owner != nil && defaultOwner != nil && owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) && owner.Equals(defaultOwner)
}

func protectPath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if directory != info.IsDir() || (!directory && !info.Mode().IsRegular()) {
		return errors.New("invalid private object type")
	}
	h, user, err := privateHandle(path, true)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;" + inheritance + ";FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

func ProtectDirectory(path string) error  { return protectPath(path, true) }
func ProtectFile(path string) error       { return protectPath(path, false) }
func ProtectOpenFile(file *os.File) error { return ProtectFile(file.Name()) }

// POSIX mode bits on Windows cannot attest access. Require an owned protected
// DACL with exactly one effective full-control grant to the current user.
func IsPrivate(path string) bool {
	h, user, err := privateHandle(path, false)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		return false
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if windows.GetAce(acl, 0, &ace) != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
		return false
	}
	return ace.Mask == 0x1f01ff && (*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user.User.Sid)
}

func HasPrivateMode(path string, _ os.FileInfo, _ os.FileMode) bool { return IsPrivate(path) }
func RestrictedAccess(path string, _ os.FileInfo) bool              { return IsPrivate(path) }
