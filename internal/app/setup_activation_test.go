package app

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

type unusedSetupEngine struct {
	*updateEngine
	unused bool
}

func (e *unusedSetupEngine) CanLeaveUnsubmittedSetup() bool { return e.unused }
func TestFirstActivationExceptionDoesNotWeakenRuntimeRecovery(t *testing.T) {
	e := &unusedSetupEngine{updateEngine: &updateEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Phase: "unknown"}}, unused: true}
	a, _ := fixtureApp(t, e, Host{})
	if err := a.guardRuntimeActivation("caelis"); err != nil {
		t.Fatal("empty first setup blocked", err)
	}
	if err := a.guardRuntimeActivation("fixture"); err == nil {
		t.Fatal("same provider recovery bypassed")
	}
	if err := a.guardRuntimeChange(); err == nil {
		t.Fatal("ordinary runtime guard weakened")
	}
	e.unused = false
	if err := a.guardRuntimeActivation("caelis"); err == nil {
		t.Fatal("unknown work ignored")
	}
	e.unused = true
	if _, err := a.initialization.Initialize(context.Background(), api.BotIntroduction{Name: "isolated test"}); err != nil {
		t.Fatal(err)
	}
	if err := a.guardRuntimeActivation("caelis"); err == nil {
		t.Fatal("saved introduction bypassed guard")
	}
}
