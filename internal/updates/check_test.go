package updates

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublishedReleaseSelection(t *testing.T) {
	for _, tt := range []struct {
		name, current, body, state, latest string
		status                             int
	}{
		{"empty", "0.0.1", `[]`, "unpublished", "", 200},
		{"unavailable", "0.0.1", `{}`, "unavailable", "", 403},
		{"invalid", "0.0.1", `not json`, "unavailable", "", 200},
		{"metadata only", "0.0.1", `[{"tag_name":"v1.0.0"}]`, "unpublished", "", 200},
		{"stable update", "0.0.1", `[{"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-arm64.dmg"}]}]`, "available", "v0.0.2", 200},
		{"reject preview", "0.0.1", `[{"tag_name":"v0.0.2-preview","prerelease":true,"assets":[{"name":"Caelis-Bot-0.0.2-preview-macos-arm64.dmg"}]}]`, "unpublished", "", 200},
		{"local dev build", "0.0.1-dev", `[{"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-arm64.dmg"}]}]`, "unavailable", "", 200},
		{"never downgrade", "0.0.3", `[{"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-arm64.dmg"}]}]`, "current", "v0.0.2", 200},
		{"checksum only", "0.0.1", `[{"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-arm64.dmg.sha256"}]}]`, "unpublished", "", 200},
		{"legacy zip", "0.0.1", `[{"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-arm64.zip"}]}]`, "unpublished", "", 200},
		{"other architecture", "0.0.1", `[{"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-x86_64.dmg"}]}]`, "unpublished", "", 200},
		{"draft", "0.0.1", `[{"draft":true,"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-arm64.dmg"}]}]`, "unpublished", "", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.String() != endpoint || r.Header.Get("Authorization") != "" || r.Body != nil {
					t.Fatalf("unexpected update request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})}
			result := check(context.Background(), client, tt.current, "arm64")
			if result.State != tt.state || result.Latest != tt.latest {
				t.Fatalf("got %+v", result)
			}
		})
	}
}
func TestTargetPlatformRequiresItsPublishedPackage(t *testing.T) {
	body := `[{"tag_name":"v1.2.0","assets":[{"name":"Caelis-Bot-1.2.0-macos-arm64.dmg"}]},{"tag_name":"v1.1.0","assets":[{"name":"Caelis-Bot-1.1.0-windows-amd64.msix"}]}]`
	client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if result := checkTarget(context.Background(), client, "1.0.0", "windows", "amd64"); result.Latest != "v1.1.0" || result.State != "available" {
		t.Fatalf("Windows selected another platform: %+v", result)
	}
	if result := checkTarget(context.Background(), client, "1.0.0", "darwin", "arm64"); result.Latest != "v1.2.0" {
		t.Fatalf("Mac selected another platform: %+v", result)
	}
	if result := checkTarget(context.Background(), client, "1.0.0", "windows", "arm64"); result.State != "unpublished" {
		t.Fatalf("unshipped architecture looked available: %+v", result)
	}
}
func TestLocalDevBuildDoesNotQueryPublicReleases(t *testing.T) {
	client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("local development build queried public releases")
		return nil, nil
	})}
	if result := check(context.Background(), client, "0.12.0-dev", "arm64"); result.State != "unavailable" {
		t.Fatalf("local build offered an update: %+v", result)
	}
}
func TestVersionOrdering(t *testing.T) {
	versions := []string{"0.0.1-alpha.2", "0.0.1-alpha.10", "0.0.1-preview", "v0.0.1", "0.0.2", "0.1.0", "1.0.0", "10.0.0"}
	for i := 1; i < len(versions); i++ {
		if compare(parseVersion(versions[i]), parseVersion(versions[i-1])) <= 0 {
			t.Fatalf("wrong ordering %s", versions[i])
		}
	}
	for _, s := range []string{"dev", "1.2", "01.2.3", "1.2.3-01"} {
		if parseVersion(s) != nil {
			t.Fatalf("accepted %s", s)
		}
	}
	if compare(parseVersion("v1.2.3+build.1"), parseVersion("1.2.3+build.2")) != 0 {
		t.Fatal("build metadata changed precedence")
	}
}

func TestCompareRuntimeVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{{"v0.62.0", "v0.61.0", 1}, {"0.156.1", "0.157.0", -1}, {"v1.2.3+local", "1.2.3", 0}} {
		if got, err := CompareVersions(tc.a, tc.b); err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	if _, err := CompareVersions("dev", "v0.62.0"); err == nil {
		t.Fatal("unknown build ordered as a release")
	}
}
