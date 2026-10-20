//go:build darwin || linux

package managedupdate

import (
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func owned(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid())
}
func privateFile(path string) error {
	i, e := os.Lstat(path)
	if e != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 || !owned(i) {
		return problem("private_file_required")
	}
	return nil
}
func privateDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return problem("managed_directory_required")
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil || resolved != path {
		return problem("managed_directory_required")
	}
	i, e := os.Lstat(path)
	if e != nil || !i.IsDir() || i.Mode().Perm()&0077 != 0 || !owned(i) {
		return problem("managed_directory_required")
	}
	return nil
}
func acquireLease(root string) (*os.File, error) {
	if e := privateDirectory(root); e != nil {
		return nil, e
	}
	fd, e := unix.Open(filepath.Join(root, "launcher.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "managed-lock")
	i, e := f.Stat()
	if e != nil || !owned(i) || i.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, problem("invalid_lock")
	}
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, problem("installation_owned")
	}
	return f, nil
}
func diskBudget(dir string, needed int64) error {
	var st unix.Statfs_t
	if e := unix.Statfs(dir, &st); e != nil {
		return e
	}
	margin := max(int64(256<<20), needed/10)
	if needed < 0 || uint64(needed+margin) > uint64(st.Bavail)*uint64(st.Bsize) || (st.Files > 0 && st.Ffree < 128) {
		return problem("disk_budget")
	}
	return nil
}

// Close only this descriptor: LOCK_UN would release a lease still held by an
// orphaned Bot child after a supervisor crash. ExtraFiles shares the flock.
func lockDirectory(root string) (func(), error) {
	f, e := acquireLease(root)
	if e != nil {
		return nil, e
	}
	return func() { _ = f.Close() }, nil
}

// The Bot retains descriptor 3 for its lifetime, but its Runtime/helpers must
// never inherit the installation lease. The launcher sets this fixed descriptor.
func RetainChildLease(root string) error {
	var inherited, expected unix.Stat_t
	if unix.Fstat(3, &inherited) != nil || unix.Stat(filepath.Join(root, "launcher.lock"), &expected) != nil || inherited.Dev != expected.Dev || inherited.Ino != expected.Ino {
		return problem("installation_lease_missing")
	}
	unix.CloseOnExec(3)
	return nil
}
