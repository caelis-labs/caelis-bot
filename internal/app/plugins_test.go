package app

import (
	"context"
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

type gatedPluginEngine struct {
	*testEngine
	inGate, applied bool
}

func (e *gatedPluginEngine) WithBotPluginAdmission(mutate func(func(context.Context, plugins.Selection) error) error) error {
	e.inGate = true
	defer func() { e.inGate = false }()
	return mutate(func(context.Context, plugins.Selection) error {
		if !e.inGate {
			return errors.New("plugin applied outside turn admission")
		}
		e.applied = true
		return nil
	})
}
func (*gatedPluginEngine) UpdateBotPlugins(context.Context, plugins.Selection) error {
	return errors.New("direct plugin update bypassed admission")
}
func (*gatedPluginEngine) BotPluginHealth(context.Context) []plugins.Issue { return nil }

func TestPluginActionUsesRuntimeAdmissionThroughStateConfirmation(t *testing.T) {
	e := &gatedPluginEngine{testEngine: newTestEngine()}
	a, _ := fixtureApp(t, e, Host{})
	if _, err := a.PluginAction(t.Context(), "markdown-work", "install"); err != nil {
		t.Fatal(err)
	}
	if e.applied || !a.plugins.Snapshot().Items[0].Enabled {
		t.Fatal("pre-start installation unexpectedly entered Runtime")
	}
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	if _, err := a.PluginAction(t.Context(), "markdown-work", "disable"); err != nil {
		t.Fatal(err)
	}
	if !e.applied || a.plugins.Snapshot().Items[0].Enabled {
		t.Fatal("Runtime admission did not cover active plugin mutation")
	}
}
