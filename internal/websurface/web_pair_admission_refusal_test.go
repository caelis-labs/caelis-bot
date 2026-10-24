package websurface

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/machines"
)

type noNetworkPreparedPairReview struct {
	*machines.Service
	effects int
}

func (m *noNetworkPreparedPairReview) ConnectPreparedBot(_ context.Context, ref api.BotConnectionReference, _ api.BotConnectionInput) (api.Machine, api.BotConnectionReference, error) {
	m.effects++
	return api.Machine{ID: ref.ID, State: "ready"}, ref, nil
}

func TestReviewKnownUndispatchedPairCanUseItsStillUnusedCode(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	m, e := machines.Open(t.TempDir(), &auditPairOwner{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	controller := &noNetworkPreparedPairReview{Service: m}
	s.services.Backend.ConfigureMachines(controller)
	opPath := filepath.Join(s.config.Directory, "operations.json")
	if e = os.Mkdir(opPath, 0700); e != nil {
		t.Fatal(e)
	}
	args := api.BotConnectionInput{ID: "bot-known-undispatched", Name: "B", Address: "http://127.0.0.1:1", Code: "unused-one-time-code"}
	first := c.request(botConnectionPath, map[string]any{"id": "pair-before-ingress-save-failure", "args": []any{args}})
	if first.Code != 503 || !strings.Contains(first.Body.String(), "not_dispatched") || controller.effects != 0 {
		t.Fatal("setup failed", first.Code, first.Body.String(), controller.effects)
	}
	if e = os.Remove(opPath); e != nil {
		t.Fatal(e)
	}
	second := c.request(botConnectionPath, map[string]any{"id": "pair-after-storage-correction", "args": []any{args}})
	if second.Code != 200 || controller.effects != 1 {
		t.Fatalf("certified no-dispatch burned unused original pairing code: status=%d body=%s effects=%d", second.Code, second.Body.String(), controller.effects)
	}
}
