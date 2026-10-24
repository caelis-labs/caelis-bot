package websurface

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type webArtifactFixture struct {
	testEngine
	path string
}

func (f *webArtifactFixture) Artifact(id string) (string, error) {
	if id != "owned-file" {
		return "", errors.New("unowned")
	}
	return f.path, nil
}
func TestWebFilePreviewIsAuthenticatedAndNeverInlinesActiveContent(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		data       []byte
		inline     bool
	}{
		{"image.png", "image/png", []byte("\x89PNG\r\n\x1a\nfixture"), true},
		{"report.pdf", "application/pdf", []byte("%PDF-1.4\nfixture"), true},
		{"page.html", "application/octet-stream", []byte("<!doctype html><script>alert('fixture')</script>"), false},
		{"image.svg", "application/octet-stream", []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"><script/></svg>"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			s := testServer(t, &webArtifactFixture{path: path})
			c := pairClient(t, s)
			get := func(paired bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", testOrigin+"/api/v1/artifacts/owned-file?preview=1", nil)
				if paired {
					r.AddCookie(c.cookie)
				}
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				return w
			}
			if w := get(false); w.Code == 200 {
				t.Fatal("unauthenticated file preview")
			}
			w := get(true)
			if w.Code != 200 || w.Header().Get("Content-Type") != tc.kind || !bytes.Equal(w.Body.Bytes(), tc.data) {
				t.Fatal("preview lost checked bytes or type", w.Code, w.Header())
			}
			if strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline") != tc.inline || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
				t.Fatal("active content was inlined", w.Header())
			}
		})
	}
}
