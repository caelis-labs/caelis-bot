package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// File delegation keeps one bounded admission operation. Budget for the maximum
// existing 8 x 20 MiB input, JSON base64 overhead at a 1 MiB/s transfer allowance,
// and the existing 25s control budget. This is a policy allowance, not a claim
// about network speed. Slower/cancelled writes retain their original receipt.
const (
	WorkerAdmissionTimeout  = 25 * time.Second
	workerMaxWireBytes      = ((8*MaxWorkFileBytes + 2) / 3) * 4
	WorkerAttachmentTimeout = WorkerAdmissionTimeout + time.Duration((workerMaxWireBytes+(1<<20)-1)/(1<<20))*time.Second
	WorkerBridgeTimeout     = WorkerAttachmentTimeout + 10*time.Second
	WorkerMCPTimeout        = WorkerBridgeTimeout + 10*time.Second
)

// WorkContentDigests binds the exact bytes serialized for dispatch, rather than
// a second read of paths which may have changed after admission.
func WorkContentDigests(files []WorkFileContent) []string {
	if len(files) == 0 {
		return nil
	}
	out := make([]string, 0, len(files))
	for _, file := range files {
		sum := sha256.Sum256(file.Data)
		out = append(out, WorkFingerprint(struct{ Name, SHA256 string }{file.Name, hex.EncodeToString(sum[:])}))
	}
	return out
}

// WorkFileMetadata is admission information, not uploaded bytes or a receipt.
// The original dispatch still validates the actual bytes and their digests.
type WorkFileMetadata struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

func WorkFileMetadataFor(files []InputFile) ([]WorkFileMetadata, error) {
	out := make([]WorkFileMetadata, 0, len(files))
	for _, f := range files {
		info, err := os.Lstat(f.Path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("worker_file_unavailable_or_too_large")
		}
		out = append(out, WorkFileMetadata{Name: f.Name, Bytes: info.Size()})
	}
	return out, nil
}
func ValidateWorkFileMetadata(files []WorkFileMetadata, caps WorkCapabilities) error {
	if caps.InputMaxFiles <= 0 || caps.InputMaxFileBytes <= 0 {
		return errors.New("worker_input_limits_unavailable")
	}
	if len(files) > caps.InputMaxFiles {
		return errors.New("too_many_worker_files")
	}
	for _, f := range files {
		if f.Bytes < 0 || f.Bytes > caps.InputMaxFileBytes {
			return errors.New("worker_file_unavailable_or_too_large")
		}
		if f.Name == "" || f.Name == "." || f.Name == ".." || f.Name != filepath.Base(f.Name) || strings.ContainsAny(f.Name, "/\\\x00") {
			return errors.New("invalid_worker_file")
		}
	}
	return nil
}
