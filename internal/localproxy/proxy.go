// Package localproxy bridges a host-only browser endpoint to a container port
// published exclusively on host loopback. It is not a general reverse proxy.
package localproxy

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func New(origin, upstream, key string) (http.Handler, error) {
	local, err := url.Parse(origin)
	if err != nil || local.Scheme != "http" || !loopback(local.Hostname()) || local.Path != "" || local.User != nil || local.RawQuery != "" || local.Fragment != "" {
		return nil, errors.New("relay origin must be exact HTTP loopback")
	}
	target, err := url.Parse(upstream)
	if err != nil || target.Scheme != "http" || !loopback(target.Hostname()) || target.User != nil || target.Path != "" || target.RawQuery != "" || target.Fragment != "" {
		return nil, errors.New("upstream must be a host-loopback published port")
	}
	if len(key) != 64 {
		return nil, errors.New("private 256-bit proof required")
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.Out.Host = local.Host
		r.Out.Header.Set("X-Caelis-Local-Proof", key)
	}, FlushInterval: -1}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !loopback(host) || !strings.EqualFold(r.Host, local.Host) || (r.Header.Get("Origin") != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Origin")), []byte(origin)) != 1) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Forbidden", 403)
			return
		}
		for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "X-Caelis-Local-Proof"} {
			if r.Header.Get(name) != "" {
				http.Error(w, "Forbidden", 403)
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}), nil
}
func loopback(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
