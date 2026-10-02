//go:build darwin || linux

package caelis

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// PrepareOwnedStore is an explicit native setup operation before human
// authentication. It creates only a new private Store and exact node marker;
// it never reads credentials, initializes Caelis, starts a Host or adopts any
// existing directory, including a previously prepared Store.
var ownedStoreNodeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func PrepareOwnedStore(nodeID, store string) error {
	if !ownedStoreNodeID.MatchString(nodeID) || len(store) > 4096 || !filepath.IsAbs(store) || filepath.Clean(store) != store || strings.ContainsAny(store, "\x00\r\n") {
		return errors.New("exact node and clean absolute owned Store path required")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return errors.New("native home unavailable")
	}
	canonicalHome, homeErr := filepath.EvalSymlinks(home)
	if homeErr != nil {
		return errors.New("native home unavailable")
	}
	if store == filepath.Join(home, ".caelis") || store == filepath.Join(canonicalHome, ".caelis") {
		return errors.New("shared default Caelis Store cannot be prepared for ownership")
	}
	parent := filepath.Dir(store)
	canonical, e := filepath.EvalSymlinks(parent)
	if e != nil || canonical != parent {
		return errors.New("owned Store parent must exist without symlink traversal")
	}
	root, e := os.OpenRoot(parent)
	if e != nil {
		return e
	}
	defer root.Close()
	info, e := root.Stat(".")
	if e != nil {
		return e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return errors.New("owned Store parent must belong to this user and disallow other writers")
	}
	name := filepath.Base(store)
	// Exclusive directory creation is the authority to place this marker. An
	// existing Store is never marked, repaired, inspected or silently reused.
	if e = root.Mkdir(name, 0700); e != nil {
		return errors.New("owned Store must be absent before explicit preparation")
	}
	prepared, e := root.Lstat(name)
	if e != nil || !prepared.IsDir() || prepared.Mode().Perm() != 0700 {
		return errors.New("new private Store identity unavailable")
	}
	actual, e := os.Lstat(store)
	if e != nil || !os.SameFile(prepared, actual) {
		return errors.New("new owned Store path changed")
	}
	storeRoot, e := root.OpenRoot(name)
	if e != nil {
		return e
	}
	defer storeRoot.Close()
	opened, e := storeRoot.Stat(".")
	if e != nil || !os.SameFile(prepared, opened) {
		return errors.New("new private Store identity changed")
	}
	marker, e := storeRoot.OpenFile(".caelis-bot-node-owner.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	body, e := json.Marshal(struct {
		NodeID string `json:"nodeId"`
	}{nodeID})
	if e == nil {
		_, e = marker.Write(body)
	}
	if e == nil {
		e = marker.Sync()
	}
	e = errors.Join(e, marker.Close())
	if e != nil {
		return e
	}
	if e = storeRoot.Mkdir(".native-home", 0700); e != nil {
		return e
	}
	directory, e := storeRoot.Open(".")
	if e != nil {
		return e
	}
	e = directory.Sync()
	e = errors.Join(e, directory.Close())
	if e != nil {
		return e
	}
	parentHandle, e := root.Open(".")
	if e != nil {
		return e
	}
	e = parentHandle.Sync()
	e = errors.Join(e, parentHandle.Close())
	return e
}
