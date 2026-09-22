// Package updates checks public releases only on explicit user request. It never
// downloads code, installs an update, reads credentials or sends local state.
package updates

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Version is set by the bundle build from package.json, including the channel.
var Version = "dev"

const ReleasePage = "https://github.com/caelis-labs/caelis-bot/releases"
const endpoint = "https://api.github.com/repos/caelis-labs/caelis-bot/releases?per_page=100"

type Result struct {
	State   string `json:"state"`
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Message string `json:"message"`
}
type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

func Check(ctx context.Context) Result {
	return check(ctx, &http.Client{Timeout: 12 * time.Second}, Version, runtime.GOARCH)
}
func check(ctx context.Context, client *http.Client, current, arch string) Result {
	result := Result{State: "unavailable", Current: current, Message: "暂时无法检查更新，请重试或前往发布页查看。"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return result
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Caelis-Bot-update-check")
	resp, err := client.Do(req)
	if err != nil {
		return result
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 {
		return result
	}
	var releases []release
	if json.Unmarshal(data, &releases) != nil {
		return result
	}
	installed := parseVersion(current)
	if installed == nil {
		result.Message = "当前为开发构建，请在发布页查看可下载版本。"
		return result
	}
	if arch == "amd64" {
		arch = "x86_64"
	}
	var latest []string
	for _, release := range releases {
		v := parseVersion(release.Tag)
		if release.Draft || v == nil || (installed[3] == "" && (release.Prerelease || v[3] != "")) {
			continue
		}
		compatible := false
		for _, asset := range release.Assets {
			if strings.HasPrefix(asset.Name, "Caelis-Bot-") && strings.HasSuffix(asset.Name, "-macos-"+arch+".dmg") {
				compatible = true
				break
			}
		}
		if compatible && (latest == nil || compare(v, latest) > 0) {
			latest = v
			result.Latest = release.Tag
		}
	}
	if latest == nil {
		result.State = "unpublished"
		result.Message = "暂时没有适用于这台 Mac 的公开下载版本。"
		return result
	}
	if compare(latest, installed) > 0 {
		result.State = "available"
		result.Message = "发现新版本，可前往发布页下载。"
	} else {
		result.State = "current"
		result.Message = "当前已是最新可用版本。"
	}
	return result
}

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func parseVersion(s string) []string {
	if len(s) > 128 {
		return nil
	}
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	for _, part := range strings.Split(m[4], ".") {
		if numeric(part) && len(part) > 1 && part[0] == '0' {
			return nil
		}
	}
	return m[1:]
}
func numeric(s string) bool { return s != "" && strings.Trim(s, "0123456789") == "" }
func numberCompare(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}
func compare(a, b []string) int {
	for i := range 3 {
		if c := numberCompare(a[i], b[i]); c != 0 {
			return c
		}
	}
	if a[3] == b[3] {
		return 0
	}
	if a[3] == "" {
		return 1
	}
	if b[3] == "" {
		return -1
	}
	ap, bp := strings.Split(a[3], "."), strings.Split(b[3], ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		x, y := ap[i], bp[i]
		c := strings.Compare(x, y)
		if numeric(x) && numeric(y) {
			c = numberCompare(x, y)
		} else if numeric(x) {
			c = -1
		} else if numeric(y) {
			c = 1
		}
		if c != 0 {
			return c
		}
	}
	if len(ap) < len(bp) {
		return -1
	}
	if len(ap) > len(bp) {
		return 1
	}
	return 0
}
