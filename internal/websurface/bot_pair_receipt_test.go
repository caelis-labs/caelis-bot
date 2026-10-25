package websurface

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type auditPairOwner struct{ api.WorkRuntime }

func (*auditPairOwner) Connect(context.Context) error       { return nil }
func (*auditPairOwner) Close(context.Context) error         { return nil }
func (*auditPairOwner) WorkAdmission(context.Context) error { return nil }
func (*auditPairOwner) WorkCapabilities(context.Context, string) (api.WorkCapabilities, error) {
	return api.WorkCapabilities{Contract: api.WorkerContract, Owner: "original-native", Build: "fixture", Runtime: "codex", History: true, Observe: true, Interactions: true, Files: true, Reconcile: true}, nil
}
