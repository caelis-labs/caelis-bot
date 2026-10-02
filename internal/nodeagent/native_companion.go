package nodeagent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

type NativeCompanionMetadata struct{ NodeID, Directory, Helper, HelperSHA256, Architecture, OS string }
type OutgoingRoute struct{ Target, Helper, Directory string }

func ReadNativeCompanionMetadata(directory, nodeID string) (NativeCompanionMetadata, error) {
	value := NativeCompanionMetadata{NodeID: nodeID, Directory: directory, Architecture: runtime.GOARCH, OS: runtime.GOOS}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" || CheckPrivateDirectory(directory) != nil {
		return value, errors.New("native process ownership unsupported")
	}
	var identity struct {
		ID string `json:"id"`
	}
	if e := readPrivateJSON(filepath.Join(directory, "node.json"), &identity); e != nil || identity.ID != nodeID {
		return value, errors.New("existing native enrollment required")
	}
	executable, e := os.Executable()
	if e != nil {
		return value, e
	}
	value.Helper = filepath.Join(filepath.Dir(executable), "caelis-node")
	if filepath.Base(executable) == "caelis-node" {
		value.Helper = executable
	}
	info, e := os.Lstat(value.Helper)
	if errors.Is(e, os.ErrNotExist) {
		value.Helper = filepath.Join(directory, "caelis-node")
		info, e = os.Lstat(value.Helper)
	}
	if !errors.Is(e, os.ErrNotExist) && (e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 || info.Size() <= 0 || info.Size() > 256<<20) {
		return value, errors.New("verified installed companion unavailable")
	}
	if e == nil {
		f, e := os.Open(value.Helper)
		if e != nil {
			return value, e
		}
		defer f.Close()
		opened, e := f.Stat()
		if e != nil || !os.SameFile(info, opened) {
			return value, errors.New("companion changed")
		}
		h := sha256.New()
		if _, e = io.Copy(h, f); e != nil {
			return value, e
		}
		value.HelperSHA256 = hex.EncodeToString(h.Sum(nil))
	}
	return value, nil
}

// ReadNativeCompanionMetadata additionally validates the existing outward
// pairing; helper discovery is shared with approved native readiness actions.

type deploymentOutput struct{ bytes.Buffer }

func (b *deploymentOutput) Write(v []byte) (int, error) {
	if b.Len()+len(v) > 256<<10 {
		return 0, errors.New("native output limit")
	}
	return b.Buffer.Write(v)
}
