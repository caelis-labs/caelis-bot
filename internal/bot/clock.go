package bot

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Return a real IANA name, never the process-local sentinel "Local" or an
// ambiguous abbreviation inferred from the current UTC offset. The supported
// macOS adapter exposes the system zone via /etc/localtime. Other adapters can
// supply a named time.Location; unknown stays explicit rather than guessed.
func clockZone(loc *time.Location) string {
	if name := loc.String(); name != "Local" {
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
		return ""
	}
	if name := strings.TrimPrefix(os.Getenv("TZ"), ":"); name != "" {
		if name != "Local" && !filepath.IsAbs(name) {
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
		// An explicit process override must not be mislabeled as the OS zone.
		return ""
	}
	resolved, err := filepath.EvalSymlinks("/etc/localtime")
	if err != nil {
		return ""
	}
	if _, name, ok := strings.Cut(filepath.ToSlash(resolved), "/zoneinfo/"); ok {
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}
	return ""
}
