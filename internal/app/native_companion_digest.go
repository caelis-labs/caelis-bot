package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

func nativeCompanionDigest(path string) (string, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		return "", errors.New("verified native host bytes unavailable")
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !os.SameFile(info, s) || !s.Mode().IsRegular() || s.Size() > 256<<20 {
		return "", errors.New("verified native host bytes unavailable")
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
