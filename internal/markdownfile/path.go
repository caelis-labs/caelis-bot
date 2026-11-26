// Package markdownfile decodes local Markdown file destinations. Callers own
// the scope check and decide whether a destination can acquire a file handle.
package markdownfile

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func Decode(destination string) string {
	value, err := url.PathUnescape(destination)
	if err != nil {
		return destination
	}
	return value
}

// Path decodes a plain path or local file URI. Plain relative paths remain
// relative so the caller can bind them to its own directory.
func Path(destination string) string {
	var path string
	if strings.HasPrefix(strings.ToLower(destination), "file:") {
		path = fileURIPath(destination, filepath.Separator == '\\')
		if !filepath.IsAbs(path) {
			return ""
		}
	} else {
		path = Decode(destination)
	}
	if strings.ContainsAny(path, "\x00\r\n") || strings.HasPrefix(path, "//") ||
		filepath.Separator == '\\' && strings.HasPrefix(filepath.FromSlash(path), `\\`) {
		return ""
	}
	return filepath.FromSlash(path)
}

func fileURIPath(destination string, windows bool) string {
	u, err := url.Parse(destination)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return ""
	}
	if u.Host == "" {
		if windows && len(u.Path) >= 4 && drive(u.Path[1:3]) && u.Path[3] == '/' {
			return u.Path[1:]
		}
		return u.Path
	}
	// Go's URL serializer also emits file://C:/path when Path was C:/path.
	// Only an exact drive letter can occupy that slot, never a network host.
	if windows && drive(u.Host) && !strings.HasPrefix(u.Path, `/\`) {
		return u.Host + u.Path
	}
	return ""
}

func drive(value string) bool {
	return len(value) == 2 && value[1] == ':' &&
		(value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z')
}

// ScopePath is the pathname to check before I/O. Windows treats a colon
// after the volume as stream syntax. Only a final positive line number is
// allowed, and it is removed before the path is passed to the filesystem.
func ScopePath(path string) string {
	if filepath.Separator != '\\' {
		return path
	}
	volume := filepath.VolumeName(path)
	if !strings.Contains(path[len(volume):], ":") {
		return path
	}
	base := withoutLine(path)
	if len(base) <= len(volume) || strings.Contains(base[len(volume):], ":") {
		return ""
	}
	return base
}

// RegularFile resolves a line citation to its file. On Unix, where a colon
// is legal in a filename, an existing literal path wins.
func RegularFile(path string) (string, bool) {
	if name := ScopePath(path); regular(name) {
		return name, true
	}
	if filepath.Separator != '\\' {
		if name := withoutLine(path); regular(name) {
			return name, true
		}
	}
	return "", false
}

func regular(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func withoutLine(path string) string {
	at := strings.LastIndexByte(path, ':')
	if at <= 0 {
		return ""
	}
	if n, err := strconv.Atoi(path[at+1:]); err != nil || n <= 0 {
		return ""
	}
	return path[:at]
}
