package windowsupdate

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type releasePage struct{ body, link string }

func pageURL(page int) string {
	if page == 1 {
		return Releases
	}
	return Releases + "&page=" + strconv.Itoa(page)
}
func nextLink(page int) string {
	return fmt.Sprintf(`<https://api.github.com/repositories/1376961363/releases?per_page=100&page=%d>; rel="next"`, page)
}
func signedRelease(t *testing.T, version string, key ed25519.PrivateKey) (map[string]any, map[string][]byte) {
	t.Helper()
	m := Manifest{Schema: 1, Version: version, Tag: "v" + version, Channel: "stable", Platform: "windows-amd64", Source: strings.Repeat("a", 40), Qualified: true}
	for _, format := range []string{"zip", "nsis"} {
		suffix := ".zip"
		if format == "nsis" {
			suffix = "-Setup.exe"
		}
		m.Artifacts = append(m.Artifacts, Artifact{format, "Caelis-Bot-" + version + "-windows-amd64-stable" + suffix, strings.Repeat("a", 64), 10})
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{DownloadURL(m, "windows-update.json"): data, DownloadURL(m, "windows-update.json.sig"): []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)))}
	var assets []map[string]string
	for _, name := range []string{"windows-update.json", "windows-update.json.sig", m.Artifacts[0].File, m.Artifacts[1].File} {
		assets = append(assets, map[string]string{"name": name, "browser_download_url": DownloadURL(m, name)})
	}
	return map[string]any{"tag_name": m.Tag, "assets": assets}, files
}
func response(body, link string) *http.Response {
	h := make(http.Header)
	if link != "" {
		h.Set("Link", link)
	}
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}
func paginatedClient(t *testing.T, pages map[string]releasePage, assets map[string][]byte, key ed25519.PublicKey) (*Client, *[]string) {
	t.Helper()
	requests := []string{}
	return &Client{Feed: Releases, PublicKey: key, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		if data, ok := assets[r.URL.String()]; ok {
			return response(string(data), ""), nil
		}
		if p, ok := pages[r.URL.String()]; ok {
			return response(p.body, p.link), nil
		}
		t.Fatalf("unexpected URL %s", r.URL)
		return nil, errors.New("unexpected URL")
	})}}, &requests
}
func TestReleasePaginationSelectsAcrossEveryPage(t *testing.T) {
	for _, versions := range [][]string{{"", "1.2.0"}, {"1.1.0", "2.0.0", "1.5.0"}, {"3.0.0", "1.0.0", "2.0.0"}} {
		t.Run(strings.Join(versions, "/"), func(t *testing.T) {
			pub, key, _ := ed25519.GenerateKey(nil)
			pages := map[string]releasePage{}
			assets := map[string][]byte{}
			best := "1.2.0"
			if len(versions) == 3 {
				best = "2.0.0"
				if versions[0] == "3.0.0" {
					best = "3.0.0"
				}
			}
			for i, version := range versions {
				body := "[]"
				if version != "" {
					release, files := signedRelease(t, version, key)
					data, _ := json.Marshal([]any{release})
					body = string(data)
					for k, v := range files {
						assets[k] = v
					}
				}
				link := ""
				if i+1 < len(versions) {
					link = nextLink(i + 2)
				}
				pages[pageURL(i+1)] = releasePage{body, link}
			}
			c, requests := paginatedClient(t, pages, assets, pub)
			offer, err := c.Check(context.Background(), "0.9.0", "stable", "zip")
			if err != nil || offer == nil || offer.Manifest.Version != best {
				t.Fatalf("offer=%+v err=%v", offer, err)
			}
			for page := 1; page <= len(versions); page++ {
				found := false
				for _, r := range *requests {
					found = found || r == pageURL(page)
				}
				if !found {
					t.Fatalf("page %d skipped: %v", page, *requests)
				}
			}
		})
	}
}
func TestReleasePaginationNoCandidateAndCompleteBoundary(t *testing.T) {
	for _, count := range []int{2, maxReleasePages} {
		pages := map[string]releasePage{}
		for p := 1; p <= count; p++ {
			link := ""
			if p < count {
				link = nextLink(p + 1)
			}
			pages[pageURL(p)] = releasePage{`[{"tag_name":"v0.8.0"},{"tag_name":"v9.0.0"}]`, link}
		}
		c, requests := paginatedClient(t, pages, nil, nil)
		offer, err := c.Check(context.Background(), "1.0.0", "stable", "zip")
		if err != nil || offer != nil || len(*requests) != count {
			t.Fatalf("offer=%v err=%v requests=%d", offer, err, len(*requests))
		}
	}
}
func TestReleasePaginationRejectsUntrustedOrInvalidLinks(t *testing.T) {
	for _, target := range []string{
		"http://api.github.com/repos/caelis-labs/caelis-bot/releases?per_page=100&page=2",
		"https://evil.test/repos/caelis-labs/caelis-bot/releases?per_page=100&page=2",
		"https://api.github.com/repos/another/repo/releases?per_page=100&page=2",
		"https://api.github.com/repositories/1/releases?per_page=100&page=2",
		"https://user@api.github.com/repos/caelis-labs/caelis-bot/releases?per_page=100&page=2",
		"https://api.github.com:443/repos/caelis-labs/caelis-bot/releases?per_page=100&page=2",
		Releases + "&page=2&token=private", Releases + "&page=2&per_page=100", Releases + "&page=2&page=3", Releases + "&page=0", Releases + "&page=-1", Releases + "&page=02", Releases + "&page=3", Releases + "&page=2#fragment", Releases + "&page=2&unknown=x", Releases + "&page=1", "/repos/caelis-labs/caelis-bot/releases?per_page=100&page=2",
		strings.ReplaceAll(Releases, "100", "1") + "&page=2",
	} {
		t.Run(target, func(t *testing.T) {
			c, requests := paginatedClient(t, map[string]releasePage{Releases: {"[]", "<" + target + `>; rel="next"`}}, nil, nil)
			offer, err := c.Check(context.Background(), "1.0.0", "stable", "zip")
			if err == nil || offer != nil || len(*requests) != 1 {
				t.Fatalf("untrusted pagination followed: %v %v %v", offer, err, *requests)
			}
		})
	}
	for _, link := range []string{nextLink(2) + ", " + nextLink(2), "not a Link", strings.Repeat("x", 8193), `<` + pageURL(2) + `>; rel="last"`} {
		c, requests := paginatedClient(t, map[string]releasePage{Releases: {"[]", link}}, nil, nil)
		offer, err := c.Check(context.Background(), "1.0.0", "stable", "zip")
		if err == nil || offer != nil || len(*requests) != 1 {
			t.Fatalf("invalid link accepted: %v %v", offer, err)
		}
	}
	c, requests := paginatedClient(t, nil, nil, nil)
	c.Feed = "https://evil.test/releases?per_page=100"
	if _, err := c.Check(context.Background(), "1.0.0", "stable", "zip"); err == nil || len(*requests) != 0 {
		t.Fatal("untrusted starting feed requested")
	}
}
func TestReleasePaginationCycleAndIncompleteLimitNeverReturnPartialOffer(t *testing.T) {
	c, requests := paginatedClient(t, map[string]releasePage{Releases: {"[]", nextLink(2)}, pageURL(2): {"[]", nextLink(1)}}, nil, nil)
	if offer, err := c.Check(context.Background(), "1.0.0", "stable", "zip"); err == nil || offer != nil || len(*requests) != 2 {
		t.Fatalf("cycle: %v %v %v", offer, err, *requests)
	}
	pub, key, _ := ed25519.GenerateKey(nil)
	release, assets := signedRelease(t, "2.0.0", key)
	data, _ := json.Marshal([]any{release})
	pages := map[string]releasePage{}
	for p := 1; p <= maxReleasePages; p++ {
		body := "[]"
		if p == 1 {
			body = string(data)
		}
		pages[pageURL(p)] = releasePage{body, nextLink(p + 1)}
	}
	c, requests = paginatedClient(t, pages, assets, pub)
	offer, err := c.Check(context.Background(), "1.0.0", "stable", "zip")
	if !errors.Is(err, ErrIncompleteFeed) || offer != nil || len(*requests) != maxReleasePages+2 {
		t.Fatalf("partial selection escaped limit: %v %v %v", offer, err, *requests)
	}
}
func TestReleasePaginationBoundsEachResponseAndEntryCount(t *testing.T) {
	for _, body := range []string{strings.Repeat(" ", maxReleasePageBytes+1), "[" + strings.Repeat("{},", 100) + "{}]"} {
		c, requests := paginatedClient(t, map[string]releasePage{Releases: {body, ""}}, nil, nil)
		if offer, err := c.Check(context.Background(), "1.0.0", "stable", "zip"); err == nil || offer != nil || len(*requests) != 1 {
			t.Fatal("oversized page accepted", offer, err)
		}
	}
}
func TestReleasePaginationCancellationAndRedirects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, requests := paginatedClient(t, nil, nil, nil)
	if _, err := c.Check(ctx, "1.0.0", "stable", "zip"); !errors.Is(err, context.Canceled) || len(*requests) != 0 {
		t.Fatal("cancelled scan requested feed", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	arrived := make(chan struct{})
	calls := 0
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response("[]", nextLink(2)), nil
		}
		close(arrived)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	done := make(chan error, 1)
	go func() { _, err := c.Check(ctx, "1.0.0", "stable", "zip"); done <- err }()
	<-arrived
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatal("page cancellation lost", err, calls)
	}
	for _, target := range []string{"https://evil.test/releases", pageURL(2)} {
		calls = 0
		c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})
		if offer, err := c.Check(context.Background(), "1.0.0", "stable", "zip"); err == nil || offer != nil || calls != 1 {
			t.Fatal("redirect bypassed pagination", offer, err, calls)
		}
	}
}
func TestReleasePaginationKeepsSignatureVerificationOnLaterPage(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(nil)
	release, assets := signedRelease(t, "2.0.0", key)
	for url, data := range assets {
		if strings.HasSuffix(url, "windows-update.json") {
			data[1] ^= 1
		}
	}
	data, _ := json.Marshal([]any{release})
	c, _ := paginatedClient(t, map[string]releasePage{Releases: {"[]", nextLink(2)}, pageURL(2): {string(data), ""}}, assets, pub)
	if offer, err := c.Check(context.Background(), "1.0.0", "stable", "zip"); err == nil || !strings.Contains(err.Error(), "signature") || offer != nil {
		t.Fatal("tampered later manifest accepted", offer, err)
	}
}

func TestReleasePaginationAcceptsRealGitHubRelations(t *testing.T) {
	h := make(http.Header)
	h.Set("Link", nextLink(2)+`, <https://api.github.com/repositories/1376961363/releases?per_page=100&page=36>; rel="last"`)
	if next, err := nextReleasePage(h, 1); err != nil || next != pageURL(2) {
		t.Fatal(next, err)
	}
	h.Set("Link", `<`+pageURL(1)+`>; rel="first", <`+pageURL(35)+`>; rel="prev"`)
	if next, err := nextReleasePage(h, 36); err != nil || next != "" {
		t.Fatal(next, err)
	}
	h.Set("Link", `<`+pageURL(36)+`>; rel="last"`)
	if _, err := nextReleasePage(h, 1); !errors.Is(err, ErrIncompleteFeed) {
		t.Fatal("missing next reported complete", err)
	}
}
