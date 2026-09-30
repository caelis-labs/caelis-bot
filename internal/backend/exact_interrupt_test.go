package backend

import (
	"context"
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type exactInterruptFixture struct {
	api.Engine
	current string
	calls   int
}

func (e *exactInterruptFixture) InterruptTurn(_ context.Context, expected string, before func()) error {
	if expected != e.current {
		return errors.New("stale observed turn")
	}
	if before != nil {
		before()
	}
	e.calls++
	return nil
}

func TestExactInterruptPreservesObserverOnlyForAcceptedTarget(t *testing.T) {
	e := &exactInterruptFixture{current: "current"}
	s := NewService(e, nil, nil, nil, nil)
	observed := 0
	s.SetInterruptObserver(func() { observed++ })
	if !s.ExactInterruptAvailable() {
		t.Fatal("implemented exact-target port hidden")
	}
	for _, target := range []string{"", "stale"} {
		if s.InterruptTurn(t.Context(), target) == nil || observed != 0 || e.calls != 0 {
			t.Fatal("stale request revoked or interrupted newer turn")
		}
	}
	if err := s.InterruptTurn(t.Context(), "current"); err != nil || observed != 1 || e.calls != 1 {
		t.Fatal("accepted target lost interruption observer", err)
	}
	if NewService(snapshotEngine{}, nil, nil, nil, nil).ExactInterruptAvailable() {
		t.Fatal("missing exact port advertised")
	}
}
