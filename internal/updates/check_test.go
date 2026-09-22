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
		{"empty", "0.0.1-preview", `[]`, "unpublished", "", 200},
		{"unavailable", "0.0.1-preview", `{}`, "unavailable", "", 403},
		{"invalid", "0.0.1-preview", `not json`, "unavailable", "", 200},
		{"metadata only", "0.0.1-preview", `[{"tag_name":"v1.0.0"}]`, "unpublished", "", 200},
		{"new preview", "0.0.1-preview", `[{"tag_name":"v0.0.2-preview","prerelease":true,"assets":[{"name":"Caelis-Bot-0.0.2-preview-macos-arm64.dmg"}]}]`, "available", "v0.0.2-preview", 200},
		{"stable skips preview", "0.0.1", `[{"tag_name":"v0.0.2-preview","prerelease":true,"assets":[{"name":"Caelis-Bot-0.0.2-preview-macos-arm64.dmg"}]}]`, "unpublished", "", 200},
		{"never downgrade", "0.0.3-preview", `[{"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-arm64.dmg"}]}]`, "current", "v0.0.2", 200},
		{"checksum only", "0.0.1-preview", `[{"tag_name":"v0.0.2-preview.1","prerelease":true,"assets":[{"name":"Caelis-Bot-0.0.2-preview.1-macos-arm64.dmg.sha256"}]}]`, "unpublished", "", 200},
		{"legacy zip", "0.0.1-preview", `[{"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-arm64.zip"}]}]`, "unpublished", "", 200},
		{"other architecture", "0.0.1-preview", `[{"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-x86_64.dmg"}]}]`, "unpublished", "", 200},
		{"draft", "0.0.1-preview", `[{"draft":true,"tag_name":"v0.0.2","assets":[{"name":"Caelis-Bot-0.0.2-macos-arm64.dmg"}]}]`, "unpublished", "", 200},
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
