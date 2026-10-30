package caelis

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Real pinned Core and real resident/callback admission; only the provider's
// choices and the first /result transport failure are synthetic.
func TestNativeTerminalResultRecoveryKeepsCorrection(t *testing.T) {
	for _, lost := range []bool{false, true} {
		name := "before-Core-acceptance"
		if lost {
			name = "response-lost-after-Core-acceptance"
		}
		t.Run(name, func(t *testing.T) {
			first, release := make(chan struct{}), make(chan struct{})
			var attempts, steers atomic.Int32
			ctx, s, resident, model := nativeDreamFixture(t, func(settings api.RuntimeSettings) {
				d, _, err := Discover(settings)
				if err != nil {
					t.Fatal(err)
				}
				origin, err := url.Parse(d.Endpoint)
				if err != nil {
					t.Fatal(err)
				}
				proxy := httputil.NewSingleHostReverseProxy(origin)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/steer") {
						steers.Add(1)
					}
					if !strings.HasSuffix(r.URL.Path, "/result") {
						proxy.ServeHTTP(w, r)
						return
					}
					if attempts.Add(1) == 1 {
						if lost {
							// Read the real Core response, then lose it at the transport boundary.
							req := r.Clone(r.Context())
							req.URL.Scheme = origin.Scheme
							req.URL.Host = origin.Host
							req.RequestURI = ""
							response, err := http.DefaultTransport.RoundTrip(req)
							if err != nil {
								t.Error(err)
							} else {
								_ = response.Body.Close()
								if response.StatusCode != 200 {
									t.Errorf("Core result status %d", response.StatusCode)
								}
							}
						}
						close(first)
						http.Error(w, "injected result transport failure", http.StatusServiceUnavailable)
						return
					}
					select {
					case <-release:
						proxy.ServeHTTP(w, r)
					case <-r.Context().Done():
					}
				}))
				t.Cleanup(server.Close)
				path := filepath.Join(settings.CaelisStore, "runtime/service/discovery.json")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var discovery map[string]any
				if err = json.Unmarshal(raw, &discovery); err != nil {
					t.Fatal(err)
				}
				discovery["endpoint"] = server.URL
				raw, _ = json.Marshal(discovery)
				if err = os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			})
			// Ensure the proxy cannot retain a native callback when this test fails.
			released := false
			defer func() {
				if !released {
					close(release)
				}
			}()
			if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "dream-original", Text: "CASE_DREAM"}, nil); err != nil || receipt.Outcome != "accepted" {
				t.Fatal(receipt, err)
			}
			select {
			case <-first:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			input := api.Submission{ID: "correction-original", Text: "CASE_CORRECTION keep the review and revoke deployment."}
			receipt, err := resident.SubmitUser(ctx, input, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !lost && (receipt.Outcome != "rejected" || !receipt.Deferred) {
				t.Fatal("accepted correction into a Turn awaiting terminal replay", receipt)
			}
			if receipt.Outcome != "accepted" && (receipt.Outcome != "rejected" || !receipt.Deferred) {
				t.Fatal(receipt)
			}
			close(release)
			released = true
			waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
			// No accepted steer was queued behind the immutable terminal result. An
			// unaccepted correction retains its original ID and becomes a new prompt.
			for range 2 {
				receipt, err = resident.SubmitUser(ctx, input, nil)
				if err != nil || receipt.Outcome != "accepted" {
					t.Fatal(receipt, err)
				}
			}
			waitAcceptance(t, ctx, func() bool { return len(model.seen("CASE_CORRECTION")) > 0 && s.Snapshot().CanSend })
			if steers.Load() != 0 || len(model.seen("CASE_CORRECTION")) != 1 || len(model.seen("CASE_DREAM")) != 1 {
				t.Fatal("native input lost or replayed", steers.Load(), len(model.seen("CASE_CORRECTION")), len(model.seen("CASE_DREAM")))
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			count := 0
			for _, record := range s.state.Calls {
				if record.Call.Name == "bot_dream" {
					count++
					if record.Receipt == nil || !value(record.Receipt.TurnComplete) {
						t.Fatal("original receipt mutated")
					}
					if !terminalDream(s.state.Views[record.Call.SessionId].Turns[record.Call.TurnId]) {
						t.Fatal("original Turn not confirmed terminal")
					}
				}
			}
			if count != 1 {
				t.Fatal("original callback identity changed", count)
			}
		})
	}
}
