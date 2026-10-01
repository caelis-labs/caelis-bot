//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func supervisorFaultFixture(t *testing.T, fault string, wrongID bool) (*SupervisedProcess, <-chan error) {
	t.Helper()
	owner, server := net.Pipe()
	t.Cleanup(func() { _ = owner.Close(); _ = server.Close() })
	p := &SupervisedProcess{conn: owner, gate: make(chan struct{}, 1), exited: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		req, err := readSupervisor(server)
		if err == nil {
			id := req.ID
			if wrongID {
				id++
			}
			err = writeSupervisor(server, supervisorFrame{ID: id, Fault: fault})
		}
		result <- err
	}()
	return p, result
}

func TestSupervisorStopFaultRequiresExactOriginalReply(t *testing.T) {
	for _, tc := range []struct {
		name, method, fault string
		wrongID, known      bool
	}{
		{name: "original stop proof", method: "stop", fault: "native-stop-unconfirmed", known: true},
		{name: "different request", method: "stop", fault: "native-stop-unconfirmed", wrongID: true},
		{name: "different fault", method: "stop", fault: "prepared-owner-revoked"},
		{name: "different method", method: "renew", fault: "native-stop-unconfirmed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, server := supervisorFaultFixture(t, tc.fault, tc.wrongID)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			reply, err := p.call(ctx, supervisorFrame{Method: tc.method})
			if serverErr := <-server; serverErr != nil {
				t.Fatal(serverErr)
			}
			if err == nil || errors.Is(err, errWatchdogStopUnconfirmed) != tc.known {
				t.Fatal("stop proof classification lost original request", reply, err)
			}
			if tc.known && (reply.ID != 1 || reply.Fault != tc.fault) {
				t.Fatal("native stop fault receipt was rewritten", reply)
			}
			if !tc.known && err.Error() != "owned watchdog fence unavailable" {
				t.Fatal("unrelated reply inferred native stop proof", err)
			}
		})
	}
}
