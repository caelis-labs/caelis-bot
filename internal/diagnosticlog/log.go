// Package diagnosticlog records bounded, private, structured diagnostics. It
// never accepts native payloads, conversation text, credentials or environment
// values as log messages. Fingerprints correlate omitted bytes with runtime logs.
package diagnosticlog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	MaxBytes = 2 << 20
	MaxFiles = 5 // Includes the active file.
	MaxAge   = 7 * 24 * time.Hour
)

type Record struct {
	Time        time.Time `json:"time"`
	Level       string    `json:"level"`
	Component   string    `json:"component"`
	Code        string    `json:"code"`
	Method      string    `json:"method,omitempty"`
	Thread      string    `json:"thread,omitempty"`
	Turn        string    `json:"turn,omitempty"`
	Item        string    `json:"item,omitempty"`
	Sequence    uint64    `json:"sequence,omitempty"`
	Server      string    `json:"server,omitempty"`
	Reason      string    `json:"reason,omitempty"` // Only classified/locally generated descriptions.
	Fingerprint string    `json:"fingerprint,omitempty"`
	Bytes       int       `json:"bytes,omitempty"`
}

type Logger struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	maxFiles int
	maxAge   time.Duration
	now      func() time.Time
	written  uint64
	failed   uint64
}

func New(directory string) *Logger {
	return &Logger{dir: directory, maxBytes: MaxBytes, maxFiles: MaxFiles, maxAge: MaxAge, now: time.Now}
}

func Fingerprint(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Native error text can contain headers, command arguments and private output.
// Record the diagnostic class plus a fingerprint, never the original text.
func Reason(message string) string {
	m := strings.ToLower(message)
	for _, c := range []struct{ match, reason string }{
		{"no such file or directory", "executable or file not found"},
		{"permission denied", "permission denied"},
		{"deadline exceeded", "operation timed out"},
		{"timed out", "operation timed out"},
		{"connection refused", "connection refused"},
		{"unauthorized", "authentication required"},
		{"authentication", "authentication failed"},
	} {
		if strings.Contains(m, c.match) {
			return c.reason
		}
	}
	return "runtime reported an error; private details omitted"
}

func DecodeReason(err error) string {
	var mismatch *json.UnmarshalTypeError
	var syntax *json.SyntaxError
	switch {
	case errors.As(err, &mismatch):
		kind, _, _ := strings.Cut(mismatch.Value, " ")
		return fmt.Sprintf("JSON type mismatch: field=%s actual=%s expected=%s offset=%d", token(mismatch.Field), token(kind), mismatch.Type, mismatch.Offset)
	case errors.As(err, &syntax):
		return fmt.Sprintf("invalid JSON at offset %d", syntax.Offset)
	default:
		return "missing or invalid required protocol fields"
	}
}

func token(s string) string {
	if len(s) > 160 {
		return "sha256:" + Fingerprint([]byte(s))
	}
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/._:-", c) {
			continue
		}
		return "sha256:" + Fingerprint([]byte(s))
	}
	return s
}

// Write is deliberately best effort: logging failure must not turn a recoverable
// component error into a failed user turn. Status exposes failed writes to the
// existing diagnostics export, without adding chat notices.
func (l *Logger) Write(r Record) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	r.Time = l.now().UTC()
	if r.Level != "error" && r.Level != "info" {
		r.Level = "warning"
	}
	for _, p := range []*string{&r.Component, &r.Code, &r.Method, &r.Thread, &r.Turn, &r.Item, &r.Server} {
		*p = token(*p)
	}
	b, err := json.Marshal(r)
	if err == nil {
		err = l.append(append(b, '\n'))
	}
	if err != nil {
		l.failed++
	} else {
		l.written++
	}
}

func (l *Logger) Status() map[string]any {
	if l == nil {
		return map[string]any{"enabled": false}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return map[string]any{"enabled": true, "written": l.written, "failedWrites": l.failed, "maxFileBytes": l.maxBytes, "maxFiles": l.maxFiles, "retentionDays": int(l.maxAge.Hours() / 24)}
}

func (l *Logger) path(index int) string {
	name := "error.jsonl"
	if index > 0 {
		name = fmt.Sprintf("error.%d.jsonl", index)
	}
	return filepath.Join(l.dir, name)
}

func (l *Logger) append(b []byte) error {
	if !filepath.IsAbs(l.dir) || int64(len(b)) > l.maxBytes {
		return errors.New("invalid diagnostic log destination or record size")
	}
	if err := os.MkdirAll(l.dir, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(l.dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("diagnostic directory is not a real directory")
	}
	if err := os.Chmod(l.dir, 0700); err != nil {
		return err
	}
	var size int64
	for i := 0; i < l.maxFiles; i++ {
		p := l.path(i)
		info, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("diagnostic file is not a regular file")
		}
		if l.now().Sub(info.ModTime()) >= l.maxAge {
			if err := os.Remove(p); err != nil {
				return err
			}
		} else if i == 0 {
			size = info.Size()
		}
	}
	if size+int64(len(b)) > l.maxBytes {
		if err := os.Remove(l.path(l.maxFiles - 1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for i := l.maxFiles - 2; i >= 0; i-- {
			if err := os.Rename(l.path(i), l.path(i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	f, err := os.OpenFile(l.path(0), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}
