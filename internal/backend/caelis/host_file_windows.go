package caelis

import (
	"errors"
	"io"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Core's public Windows discovery/token contract permits current-user,
// LocalSystem and Administrators grants on a protected, user-owned file.
// Its service directory inherits the user's ACL. Do not rewrite external Store
// ACLs or apply the stricter Bot-owned state layout to a Core-owned file.
func readHostFile(path string, limit int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("Caelis Host file is redirected")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, errors.New("Caelis Host file changed")
	}
	if !hostFilePrivate(windows.Handle(f.Fd())) {
		return nil, errors.New("Caelis Host file ACL is unsafe")
	}
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("Caelis Host file cannot be read")
	}
	return body, nil
}

func hostFilePrivate(h windows.Handle) bool {
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || info.NumberOfLinks != 1 {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return false
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	defer runtime.KeepAlive(sd)
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return false
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return false
	}
	userAccess := false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&(windows.INHERITED_ACE|windows.INHERIT_ONLY_ACE) != 0 {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(user.User.Sid) && !sid.Equals(system) && !sid.Equals(admins) {
			return false
		}
		mask := ace.Mask
		if mask&windows.GENERIC_ALL != 0 {
			mask |= windows.STANDARD_RIGHTS_ALL | windows.SPECIFIC_RIGHTS_ALL
		}
		if mask&windows.GENERIC_READ != 0 {
			mask |= windows.FILE_GENERIC_READ
		}
		if mask&windows.GENERIC_WRITE != 0 {
			mask |= windows.FILE_GENERIC_WRITE
		}
		if sid.Equals(user.User.Sid) && mask&(windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE) == windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE {
			userAccess = true
		}
	}
	return userAccess
}
