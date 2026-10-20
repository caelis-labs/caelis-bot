package managedupdate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/updates"
)

func qaOrigin(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "http" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" &&
		(u.Hostname() == "127.0.0.1" || u.Hostname() == "::1") && u.Port() != ""
}

func (t Trust) client() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 4 || r.URL.User != nil || r.URL.Fragment != "" {
			return problem("redirect_denied")
		}
		if t.QA {
			u, _ := url.Parse(t.Feed)
			if r.URL.Scheme == "http" && r.URL.Host == u.Host {
				return nil
			}
		} else if r.URL.Scheme == "https" && (r.URL.Port() == "" || r.URL.Port() == "443") {
			switch r.URL.Hostname() {
			case "github.com", "api.github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
				return nil
			}
		}
		return problem("redirect_denied")
	}}
}
func (t Trust) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CaelisBot-managed-update")
	res, err := t.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, problem("feed_unavailable")
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, problem("response_invalid")
	}
	return b, nil
}

func (t Trust) Check(ctx context.Context, current, platform string) (*Offer, error) {
	body, err := t.get(ctx, t.Feed, 2<<20)
	if err != nil {
		return nil, err
	}
	var releases []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.Unmarshal(body, &releases) != nil {
		return nil, problem("feed_invalid")
	}
	var best *Offer
	for _, r := range releases {
		if r.Draft || (!t.QA && r.Prerelease) {
			continue
		}
		order, e := updates.CompareVersions(r.Tag, current)
		if e != nil || order <= 0 {
			continue
		}
		name := platform + "-update.json"
		assets := map[string]bool{}
		for _, a := range r.Assets {
			if a.URL == t.Base+"/"+r.Tag+"/"+a.Name {
				assets[a.Name] = true
			}
		}
		if !assets[name] || !assets[name+".sig"] {
			continue
		}
		if len(t.Key) != 32 {
			return nil, problem("release_trust_unconfigured")
		}
		data, e := t.get(ctx, t.Base+"/"+r.Tag+"/"+name, 65536)
		if e != nil {
			return nil, e
		}
		sig, e := t.get(ctx, t.Base+"/"+r.Tag+"/"+name+".sig", 1024)
		if e != nil {
			return nil, e
		}
		o, e := Verify(data, sig, t.Key, platform, t.QA)
		if e != nil {
			return nil, e
		}
		if r.Tag != "v"+o.Manifest.Version || !assets[o.Manifest.File] {
			return nil, problem("release_mismatch")
		}
		if best == nil {
			best = &o
		} else if n, _ := updates.CompareVersions(o.Manifest.Version, best.Manifest.Version); n > 0 {
			best = &o
		}
	}
	return best, nil
}

func (t Trust) Download(ctx context.Context, o Offer, dir string, progress func(int64)) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := privateDirectory(dir); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "archive.tar.gz")
	if verifyFile(target, o.Manifest.Length, o.Manifest.SHA256) == nil {
		progress(o.Manifest.Length)
		return target, nil
	}
	if err := diskBudget(dir, o.Manifest.Length); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", t.Base+"/v"+o.Manifest.Version+"/"+o.Manifest.File, nil)
	if err != nil {
		return "", err
	}
	res, err := t.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", problem("download_unavailable")
	}
	f, err := os.CreateTemp(dir, ".download-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	counter := &progressWriter{writer: f, report: progress}
	_, err = io.Copy(counter, io.LimitReader(res.Body, o.Manifest.Length+1))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = verifyFile(f.Name(), o.Manifest.Length, o.Manifest.SHA256); err != nil {
		return "", err
	}
	if err = os.Rename(f.Name(), target); err != nil {
		return "", err
	}
	return target, localstate.SyncParent(target)
}

type progressWriter struct {
	writer io.Writer
	report func(int64)
	n      int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, e := w.writer.Write(p)
	w.n += int64(n)
	w.report(w.n)
	return n, e
}
