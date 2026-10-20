package localproxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRelayTrustAndProof(t *testing.T) {
	key := strings.Repeat("a", 64)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Caelis-Local-Proof") != key || r.Header.Get("X-Forwarded-For") != "" || r.Host != "127.0.0.1:9317" {
			t.Error("invalid forwarded boundary")
		}
		w.WriteHeader(204)
	}))
	defer upstream.Close()
	proxy, err := New("http://127.0.0.1:9317", upstream.URL, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		peer, host, origin, forwarded string
		want                          int
	}{
		{"127.0.0.1:1234", "127.0.0.1:9317", "http://127.0.0.1:9317", "", 204},
		{"192.168.1.2:1234", "127.0.0.1:9317", "http://127.0.0.1:9317", "", 403},
		{"127.0.0.1:1234", "public.invalid", "http://127.0.0.1:9317", "", 403},
		{"127.0.0.1:1234", "127.0.0.1:9317", "https://evil.invalid", "", 403},
		{"127.0.0.1:1234", "127.0.0.1:9317", "http://127.0.0.1:9317", "198.51.100.9", 403},
	} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:9317/api/v1/bootstrap", nil)
		r.RemoteAddr = tc.peer
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Forwarded", tc.forwarded)
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("got %d want %d", w.Code, tc.want)
		}
	}
	if _, err := New("http://0.0.0.0:9317", upstream.URL, key); err == nil {
		t.Fatal("public relay accepted")
	}
	if _, err := New("http://127.0.0.1:9317", "http://192.168.1.2:9318", key); err == nil {
		t.Fatal("unprotected upstream accepted")
	}
}
