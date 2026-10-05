//go:build darwin && cgo

package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNativePACResolvesProxyAndExplicitDirect(t *testing.T) {
	for _, result := range []struct{ directive, want string }{
		{"PROXY 127.0.0.1:7890; DIRECT", "http://127.0.0.1:7890"},
		{"SOCKS 127.0.0.1:1080; DIRECT", "socks5://127.0.0.1:1080"},
		{"DIRECT", ""},
	} {
		t.Run(result.directive, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
				fmt.Fprintf(w, `function FindProxyForURL(url, host) { if (host === "api.telegram.org") return %q; return "DIRECT"; }`, result.directive)
			}))
			defer server.Close()
			proxy, err := resolveSystemProxy(t.Context(), "https://api.telegram.org/botfixture/getUpdates", server.URL+"/proxy.pac")
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if proxy != nil {
				got = proxy.String()
			}
			if got != result.want {
				t.Fatalf("PAC result = %q, want %q", got, result.want)
			}
		})
	}
}

func TestNativePACFailureCannotBecomeDirectOrExposeTarget(t *testing.T) {
	for _, script := range []string{"invalid javascript", `function FindProxyForURL(url, host) { throw new Error(url); }`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, script)
		}))
		proxy, err := resolveSystemProxy(t.Context(), "https://api.telegram.org/botPRIVATE_FIXTURE/getUpdates", server.URL)
		server.Close()
		if err == nil || proxy != nil || strings.Contains(err.Error(), "PRIVATE_FIXTURE") {
			t.Fatal("invalid PAC became direct or exposed target")
		}
	}
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	if proxy, err := resolveSystemProxy(t.Context(), "https://api.telegram.org/", server.URL); err == nil || proxy != nil {
		t.Fatal("failed PAC download became direct")
	}
}

func TestNativePACCancellationReleasesResolver(t *testing.T) {
	requested := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(requested) })
		<-release
		fmt.Fprint(w, `function FindProxyForURL(url, host) { return "DIRECT"; }`)
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := resolveSystemProxy(ctx, "https://api.telegram.org/", server.URL); done <- err }()
	select {
	case <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("PAC download did not start")
	}
	// Waiting for the shared resolver also honors the request's deadline.
	waiting, end := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer end()
	if _, err := resolveSystemProxy(waiting, "https://api.telegram.org/", server.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("resolver acquisition ignored cancellation")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("PAC cancellation lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PAC source did not cancel")
	}
	// The cancelled source must not strand the process-wide resolver.
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `function FindProxyForURL(url, host) { return "DIRECT"; }`)
	}))
	defer ready.Close()
	if proxy, err := resolveSystemProxy(t.Context(), "https://api.telegram.org/", ready.URL); err != nil || proxy != nil {
		t.Fatal("resolver did not recover after cancellation", err)
	}
}

func TestNativePACRoutesHTTPRequestThroughResolvedProxy(t *testing.T) {
	proxied := make(chan string, 1)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		proxied <- req.URL.Host
		fmt.Fprint(w, "proxy fixture")
	}))
	defer proxyServer.Close()
	pacServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `function FindProxyForURL(url, host) { return %q; }`, "PROXY "+strings.TrimPrefix(proxyServer.URL, "http://")+"; DIRECT")
	}))
	defer pacServer.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		return resolveSystemProxy(req.Context(), req.URL.String(), pacServer.URL)
	}
	defer transport.CloseIdleConnections()
	h := &http.Client{Timeout: 3 * time.Second, Transport: transport}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://api.telegram.org/fixture", nil)
	response, err := h.Do(req)
	if err != nil {
		t.Fatal("resolved proxy was not usable", err)
	}
	response.Body.Close()
	select {
	case host := <-proxied:
		if host != "api.telegram.org" {
			t.Fatal("proxy lost destination")
		}
	default:
		t.Fatal("request bypassed PAC proxy")
	}
}
