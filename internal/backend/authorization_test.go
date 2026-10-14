package backend

import (
	"context"
	"testing"
)

type authorizationEngine struct {
	executionFake
	scope string
}

func (e *authorizationEngine) AuthorizeFullAccess(_ context.Context, scope string) error {
	e.scope = scope
	return nil
}

func TestFullAccessAuthorizationDispatchRequiresExactRuntimeAndScope(t *testing.T) {
	engine := &authorizationEngine{executionFake: executionFake{providerID: "codex"}}
	s := NewService(engine, nil, nil, nil, nil)
	if err := s.AuthorizeCaelisFullAccess("bot"); err == nil || engine.scope != "" {
		t.Fatal("Codex Bot was allowed to request Caelis Host grant", err)
	}
	if err := s.AuthorizeCaelisFullAccess("both"); err == nil || engine.scope != "" {
		t.Fatal("combined scope was accepted", err)
	}
	engine.providerID = "caelis"
	if err := s.AuthorizeCaelisFullAccess("bot"); err != nil || engine.scope != "bot" {
		t.Fatal("Caelis Bot grant did not reach current owner", err, engine.scope)
	}
}
