package caelis

import (
	"encoding/json"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"net/http"
	"testing"
	"time"
)

func TestApprovalEventsWakeAuthoritativeRefresh(t *testing.T) {
	for _, settled := range []bool{false, true} {
		t.Run(fmt.Sprint(settled), func(t *testing.T) {
			var s *Session
			head := wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", Run: wire.RunState{Active: pointer(true), Status: pointer("running")}}
			if settled {
				head.Approval.Active = testApproval()
			}
			s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/control/v1/initialize":
					writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: required})
				case "/api/control/v1/sessions/main/state":
					current := clone(head)
					if settled {
						current.Approval.Active = nil
					} else {
						current.Approval.Active = testApproval()
					}
					// Busy message output during the GET must not starve approval delivery.
					s.mu.Lock()
					s.state.Views["main"].Observed++
					s.mu.Unlock()
					writeFixture(w, current)
				default:
					w.Header().Set("Content-Type", "text/event-stream")
					send := func(event string, v any) {
						b, _ := json.Marshal(v)
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
					}
					send("caelis.control.bootstrap", head)
					e := wire.Envelope{Kind: "session/request_permission", ApprovalRequestId: pointer("approval"), EventId: pointer("permission"), Permission: &wire.RequestPermission{SessionId: "main", ToolCall: wire.ACPToolCallUpdate{ToolCallId: "call", Kind: pointer("execute")}}}
					if settled {
						e.Kind = "caelis/lifecycle"
						e.Permission = nil
						e.Lifecycle = &wire.LifecycleEvent{State: "completed"}
					}
					send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", NextCursor: pointer("new-cursor"), Events: []wire.Envelope{e}})
				}
			})
			s.state.StoreID = "store"
			s.state.Connection.ExpiresAt = time.Now().Add(time.Hour)
			s.streams["main"] = true
			_ = s.watch(t.Context(), s.client, "main", "instance")
			if !s.needsRefreshLocked() {
				t.Fatal("live permission change waits for lease maintenance")
			}
			select {
			case <-s.wake:
			default:
				t.Fatal("approval did not wake polling loop")
			}
			if err := s.refresh(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := 1
			if settled {
				want = 0
			}
			if len(s.Snapshot().Approvals) != want || s.state.Views["main"].ApprovalDirty {
				t.Fatal("authoritative approval not delivered")
			}
		})
	}
}

func TestNewApprovalFencesStaleHeadRead(t *testing.T) {
	v := &view{Seen: map[string]bool{}, State: wire.SessionState{Approval: wire.ApprovalState{Active: testApproval()}}}
	old := v.ApprovalVersion
	applyEnvelope(v, wire.Envelope{Kind: "session/request_permission", EventId: pointer("next"), ApprovalRequestId: pointer("next")})
	reconcileApproval(v, v, old, wire.SessionState{})
	if !v.ApprovalDirty || v.State.Approval.Active == nil {
		t.Fatal("stale read overwrote newer notification")
	}
	replacement := &view{ApprovalDirty: true}
	reconcileApproval(replacement, v, 0, wire.SessionState{Approval: v.State.Approval})
	if !replacement.ApprovalDirty || replacement.State.Approval.Active != nil {
		t.Fatal("old stream overwrote replacement")
	}
}
