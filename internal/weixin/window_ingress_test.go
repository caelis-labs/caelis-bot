package weixin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestEveryDistinctOwnerTextIncludingControlsRefreshesWindowBeforeDispatch(t *testing.T) {
	for _, body := range []string{"ordinary", "/r", "/answer Q1 2", "/approve A1 1"} {
		t.Run(body, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					_, _ = fmt.Fprintf(w, `{"ret":0,"get_updates_buf":"next","msgs":[{"message_id":"new","from_user_id":"owner","to_user_id":"bot","message_type":1,"context_token":"fresh","item_list":[{"type":1,"text_item":{"text":%q}}]},{"message_id":"new","from_user_id":"owner","to_user_id":"bot","message_type":1,"context_token":"duplicate","item_list":[{"type":1,"text_item":{"text":"duplicate"}}]},{"message_id":"foreign","from_user_id":"foreign","to_user_id":"bot","message_type":1,"context_token":"foreign","item_list":[{"type":1,"text_item":{"text":"not owner"}}]}]}`, body)
					return
				}
				select {
				case <-r.Context().Done():
				case <-time.After(20 * time.Millisecond):
					_, _ = w.Write([]byte(`{"ret":0}`))
				}
			}))
			defer server.Close()
			b, err := Open(t.TempDir(), Host{})
			if err != nil {
				t.Fatal(err)
			}
			b.state.BotID, b.state.OwnerID, b.state.BaseURL = "bot", "owner", defaultBase
			freshWindow(b, "old")
			b.state.Window.Used = 10
			b.state.Window.Exhausted = true
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				b.receive(ctx, &protocol{client: server.Client(), base: server.URL, token: "fixture"})
			}()
			deadline := time.After(3 * time.Second)
			for {
				b.mu.Lock()
				received := b.state.Cursor == "next"
				b.mu.Unlock()
				if received {
					break
				}
				select {
				case <-deadline:
					t.Fatal("ingress not received")
				case <-time.After(time.Millisecond):
				}
			}
			cancel()
			<-done
			if b.state.Window.Used != 0 || b.state.Window.Exhausted || b.state.Window.ContextToken != "fresh" || b.state.Window.InputID != "weixin:bot:new" || len(b.state.Inbox) != 1 {
				t.Fatal(b.state.Window, b.state.Inbox)
			}
			reopened, err := Open(b.path[:len(b.path)-len("/weixin.json")], Host{})
			if err != nil {
				t.Fatal(err)
			}
			if reopened.state.Window != b.state.Window {
				t.Fatal("restart changed ingress allowance")
			}
		})
	}
}
