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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Real pinned Core and real resident/callback admission; only the provider's
// choices and the first /result transport failure are synthetic.
func TestNativeHandoffResultRecoveryKeepsCorrection(t *testing.T) {
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
			source := s.ConversationState().Session
			oldRelease := make(chan struct{})
			finishOld := sync.OnceFunc(func() { close(oldRelease) })
			defer finishOld()
			model.set("CASE_DREAM", modelStep{Name: "bot_dream", Args: map[string]string{"handoff": "Wait for the user before changing the original review scope."}}, modelStep{Block: oldRelease})
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
			if receipt.Outcome != "accepted" || s.ConversationState().Session != source {
				t.Fatal("correction did not keep the original owner", receipt)
			}
			close(release)
			released = true
			receipt, err = resident.SubmitUser(ctx, input, nil)
			if err != nil || receipt.Outcome != "accepted" {
				t.Fatal(receipt, err)
			}
			finishOld()
			waitAcceptance(t, ctx, func() bool { return len(model.seen("CASE_CORRECTION")) > 0 && s.Snapshot().CanSend })
			if steers.Load() != 1 || len(model.seen("CASE_CORRECTION")) != 1 || s.ConversationState().Session != source {
				t.Fatal("native input lost or replayed", steers.Load(), len(model.seen("CASE_CORRECTION")), len(model.seen("CASE_DREAM")))
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			count := 0
			for _, record := range s.state.Calls {
				if record.Call.Name == "bot_dream" {
					count++
					if record.Receipt == nil || record.Receipt.Outcome != "succeeded" {
						t.Fatal("original receipt mutated")
					}
					if record.Call.SessionId != source {
						t.Fatal("original callback changed owners")
					}
				}
			}
			if count != 1 {
				t.Fatal("original callback identity changed", count)
			}
		})
	}
}
