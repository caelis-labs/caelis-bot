package caelis

import (
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestLiveCommandJournalIsProgressNotRecovery(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "lost-response"}[lost], func(t *testing.T) {
			arrived, release := make(chan struct{}), make(chan struct{})
			var posts atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				close(arrived)
				<-release
				if lost {
					drop(w)
					return
				}
				writeFixture(w, wire.CommandResult{OperationId: "original", Outcome: "accepted"})
			})
			// Release the HTTP handler even when an assertion fails.
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			done := make(chan error, 1)
			go func() { _, err := s.command(t.Context(), "original", "/sessions/main/prompt", struct{}{}); done <- err }()
			select {
			case <-arrived:
			case <-time.After(3 * time.Second):
				t.Fatal("command was not dispatched")
			}
			snapshot := s.Snapshot()
			if snapshot.Phase != "sending" || snapshot.Message != "" || snapshot.CanSend || snapshot.CanSteer {
				t.Fatal("live POST showed recovery or released admission", snapshot.Phase, snapshot.Message, snapshot.CanSend)
			}
			// A restart cannot inherit the process-local in-flight exemption.
			restored := New(Options{Directory: filepath.Dir(s.path)})
			restored.connected = true
			if recovered := restored.Snapshot(); recovered.Phase != "unknown" || recovered.CanSend {
				t.Fatal("durable uncertainty lost across restart", recovered.Phase, recovered.CanSend)
			}
			close(release)
			select {
			case err := <-done:
				if (err != nil) != lost {
					t.Fatal("unexpected receipt", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("command did not settle")
			}
			snapshot = s.Snapshot()
			if lost {
				if snapshot.Phase != "unknown" || snapshot.Message != "" || snapshot.CanSend {
					t.Fatal("uncertain effect was exposed as an error or permitted replay", snapshot.Phase, snapshot.CanSend)
				}
				if _, err := s.command(t.Context(), "original", "/sessions/main/prompt", struct{}{}); err != nil || posts.Load() != 1 {
					t.Fatal("uncertain original was redispatched", posts.Load(), err)
				}
			} else if !snapshot.CanSend || snapshot.Phase == "unknown" || snapshot.Message != "" {
				t.Fatal("known receipt did not clear progress", snapshot.Phase, snapshot.CanSend)
			}
		})
	}
}
