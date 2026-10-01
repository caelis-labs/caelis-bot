package runtimemanagement

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// NewLocalNode binds only the runtime child of an already trusted, canonical,
// same-user private in-process Node slot. APP profiles may live outside HOME.
// It never creates an installation or expands the remote New HOME policy.
func NewLocalNode(nodeDirectory string) (*Manager, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil, errors.New("local Node installation unsupported")
	}
	canonical, e := filepath.EvalSymlinks(nodeDirectory)
	info, err := os.Lstat(nodeDirectory)
	if !filepath.IsAbs(nodeDirectory) || e != nil || err != nil || canonical != nodeDirectory || !info.IsDir() || info.Mode().Perm() != 0700 || !privateOwner(info) {
		return nil, errors.New("trusted canonical private Node directory required")
	}
	releases := officialReleases
	if runtime.GOOS == "darwin" {
		releases = darwinReleases
	}
	return newManager(filepath.Join(nodeDirectory, "runtime"), runtime.GOARCH, releases), nil
}

// ReviewedReleases projects only this manager's platform/architecture. A local
// Darwin manager must never select a Linux release with the same version.
func (m *Manager) ReviewedReleases() []Release {
	releases := []Release{}
	for _, r := range m.releases {
		if r.Arch == m.arch {
			releases = append(releases, r)
		}
	}
	return releases
}
