package workerurl

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOriginalFormTransportNeverExposesHiddenStateOrRunsScripts(t *testing.T) {
	var posts atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("credential forwarded")
		}
		if r.Method == "GET" {
			http.SetCookie(w, &http.Cookie{Name: "request-only", Value: "fresh"})
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<title>Mock challenge</title><form method="post"><label for="name">Display name</label><input name="name" id="name" required><input type="hidden" name="nonce" value="native-only"><input type="password" name="secret" value="never-project"><select name="choice"><option value="a">Original A</option><option value="b">Original B</option></select><button>Continue original</button></form>`)
			return
		}
		if r.Header.Get("Origin") != server.URL || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("original origin/encoding lost")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		cookie, err := r.Cookie("request-only")
		if err != nil || cookie.Value != "fresh" {
			t.Error("fresh request cookie missing")
		}
		if r.Form.Get("nonce") != "native-only" || r.Form.Get("name") != "Synthetic" || r.Form.Get("secret") != "temporary-secret" || r.Form.Get("choice") != "b" {
			t.Error("native form values changed")
		}
		posts.Add(1)
		w.WriteHeader(204)
	}))
	defer server.Close()
	p, err := Fetch(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.View.Fields) != 3 || p.View.Fields[0].Label != "Display name" || p.View.Fields[1].Value != "" || p.View.SubmitLabel != "Continue original" {
		t.Fatalf("unsafe/incomplete projection: %+v", p.View)
	}
	if _, err = p.Encode(map[string]string{"name": "Synthetic", "nonce": "forged"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("hidden override allowed", err)
	}
	if _, err = p.Encode(map[string]string{"name": "Synthetic", "choice": "forged"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("native option changed", err)
	}
	body, err := p.Encode(map[string]string{"name": "Synthetic", "secret": "temporary-secret", "choice": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Submit(t.Context(), body); err != nil || posts.Load() != 1 {
		t.Fatal(err, posts.Load())
	}
}
func TestUnsafeTargetsAndNonportablePagesFailClosed(t *testing.T) {
	for _, raw := range []string{"http://localhost:1234/", "http://example.invalid:1234/", "http://192.168.0.1:1234/", "http://user@127.0.0.1:1234/", "https://127.0.0.1:1234/", "http://127.0.0.1:1234/#fragment", "file:///etc/passwd"} {
		if _, err := targetURL(t.Context(), raw); err == nil {
			t.Fatal("target allowed", raw)
		}
	}
	addr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:1234")
	ctx := context.WithValue(t.Context(), http.LocalAddrContextKey, addr)
	if _, err := targetURL(ctx, "http://127.0.0.1:01234/api/v1/bootstrap"); err == nil {
		t.Fatal("own product listener allowed")
	}
	u, _ := url.Parse("http://127.0.0.1:1234/challenge/original?nonce=native")
	for _, body := range []string{
		`<script>steal()</script><form method="post"><button>Continue</button></form>`,
		`<form method="post" onsubmit="oauth()"><button>Continue</button></form>`,
		`<form method="post" action="/api/control"><button>Continue</button></form>`,
		`<form method="post" action="http://127.0.0.1:1235/challenge/original"><button>Continue</button></form>`,
		`<form method="post"><input name="file" type="file"><button>Continue</button></form>`,
		`<form method="post"><button formaction="/elsewhere">Continue</button></form>`,
		`<form method="get"><button>Continue</button></form>`,
		`<form method="post"><input name="n"><input name="n"><button>Continue</button></form>`,
		`<form method="post"><input name="n" pattern=".+"><button>Continue</button></form>`,
		`<form method="post"><textarea name="n">Original multiline</textarea><button>Continue</button></form>`,
		`<form method="post"><input name="n" type="number"><button>Continue</button></form>`,
		`<form method="post"><select name="n"><option>Implicit value</option></select><button>Continue</button></form>`,
	} {
		if _, err := parse(u, body); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unsupported page accepted", body, err)
		}
	}
}
func TestPageLimitsRedirectExpiryAndCancelledPost(t *testing.T) {
	for _, mode := range []string{"redirect", "expired", "large", "json"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, "http://127.0.0.1:1/", 302)
				case "expired":
					w.WriteHeader(410)
				case "large":
					w.Header().Set("Content-Type", "text/html")
					io.WriteString(w, strings.Repeat("x", MaxPageBytes+1))
				case "json":
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{}`)
				}
			}))
			defer server.Close()
			if _, err := Fetch(t.Context(), server.URL); err == nil {
				t.Fatal("unsafe/unavailable page read")
			}
		})
	}
}

func TestEmptyOriginalSelectedOptionIsNotReplaced(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:1234/original")
	page, err := parse(u, `<p>Review the original scope.</p><form method="post"><select name="scope"><option value="all">All</option><option value="" selected>None</option></select><button>Confirm</button></form>`)
	if err != nil || page.View.Fields[0].Value != "" || !strings.Contains(page.View.Text, "Review the original scope.") {
		t.Fatal("original empty option/scope changed", err, page)
	}
}

func TestUnimplementedBrowserControlSemanticsAreNotRewritten(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:1234/original")
	for name, body := range map[string]string{
		"disabled-fieldset": `<form method="post"><fieldset disabled><input name="scope" value="admin"></fieldset><button>Continue</button></form>`,
		"readonly":          `<form method="post"><input readonly name="scope" value="read"><button>Continue</button></form>`,
		"inert-template":    `<form method="post"><template><input name="scope" value="admin"></template><button>Continue</button></form>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(u, body); !errors.Is(err, ErrUnsupported) {
				t.Fatal("native browser form semantics changed", err)
			}
		})
	}
}
