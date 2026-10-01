//go:build darwin || linux

package caelis

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// ProbeOwnedStore checks only native ownership metadata. It never initializes
// or adopts a Store, reads account/configuration data, or starts a Host. The
// node ownership marker is the only file whose bounded contents are read.
func ProbeOwnedStore(nodeID, store string) (bool, string) {
	if nodeID == "" || len(store) > 4096 || !filepath.IsAbs(store) || filepath.Clean(store) != store || strings.ContainsAny(store, "\x00\r\n") {
		return false, "owned-store-path-invalid"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, "owned-store-state-unavailable"
	}
	if store == filepath.Join(home, ".caelis") {
		return false, "shared-default-store"
	}
	for path := store; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return false, "owned-store-path-invalid"
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, "owned-store-state-unavailable"
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	info, err := os.Lstat(store)
	if errors.Is(err, os.ErrNotExist) {
		return false, "owned-store-setup-required"
	}
	if err != nil || !info.IsDir() || !privateOwnedStoreMetadata(info) {
		return false, "owned-store-not-private"
	}
	canonical, err := filepath.EvalSymlinks(store)
	if err != nil || canonical != store {
		return false, "owned-store-path-invalid"
	}
	root, err := os.OpenRoot(store)
	if err != nil {
		return false, "owned-store-state-unavailable"
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return false, "owned-store-state-unavailable"
	}
	markerInfo, err := root.Lstat(".caelis-bot-node-owner.json")
	if errors.Is(err, os.ErrNotExist) {
		return false, "owned-store-setup-required"
	}
	if err != nil || !markerInfo.Mode().IsRegular() || !privateOwnedStoreMetadata(markerInfo) || markerInfo.Size() > 4096 {
		return false, "owned-store-marker-unavailable"
	}
	marker, err := root.OpenFile(".caelis-bot-node-owner.json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "owned-store-setup-required"
		}
		return false, "owned-store-marker-unavailable"
	}
	openedMarker, err := marker.Stat()
	if err != nil || !os.SameFile(markerInfo, openedMarker) || !openedMarker.Mode().IsRegular() || !privateOwnedStoreMetadata(openedMarker) || openedMarker.Size() > 4096 {
		marker.Close()
		return false, "owned-store-marker-unavailable"
	}
	data, err := io.ReadAll(io.LimitReader(marker, 4097))
	marker.Close()
	var owner struct {
		NodeID string `json:"nodeId"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err != nil || len(data) > 4096 || decoder.Decode(&owner) != nil || decoder.Decode(new(any)) != io.EOF {
		return false, "owned-store-marker-unavailable"
	}
	if owner.NodeID != nodeID {
		return false, "owned-store-node-mismatch"
	}
	lockInfo, err := root.Lstat(".caelis-bot-node.lock")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, "owned-store-state-unavailable"
	}
	if err == nil {
		if !lockInfo.Mode().IsRegular() || !privateOwnedStoreMetadata(lockInfo) {
			return false, "owned-store-state-unavailable"
		}
		lock, err := root.OpenFile(".caelis-bot-node.lock", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return false, "owned-store-state-unavailable"
		}
		opened, err := lock.Stat()
		if err != nil || !os.SameFile(lockInfo, opened) {
			lock.Close()
			return false, "owned-store-state-unavailable"
		}
		if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			lock.Close()
			return false, "owned-store-controller-busy"
		}
		_ = lock.Close() // Release the transient read-only advisory probe.
	}
	for _, path := range []string{"runtime", "runtime/service"} {
		info, err := root.Lstat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, "owned-store-state-unavailable"
		}
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return false, "owned-store-path-invalid"
		}
	}
	if _, err := root.Lstat("runtime/service/discovery.json"); !errors.Is(err, os.ErrNotExist) {
		return false, "owned-store-discovery-present"
	}
	if info, err := root.Lstat(".native-home"); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !privateOwnedStoreMetadata(info) {
			return false, "owned-store-native-home-invalid"
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, "owned-store-state-unavailable"
	}
	return true, ""
}

func privateOwnedStoreMetadata(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0077 == 0
}
