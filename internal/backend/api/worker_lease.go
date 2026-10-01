package api

import "errors"

// WorkerLeaseGrant is host-attested dispatch provenance. It is not a renderer
// or model tool argument. Value fields preserve exact Source comparability;
// the enclosing Source uses json omitzero to retain unleased receipt digests.
type WorkerLeaseGrant struct {
	BotID        string `json:"botId"`
	BrokerNodeID string `json:"brokerNodeId"`
	SourceNodeID string `json:"sourceNodeId"`
	Backend      string `json:"backend"`
	Epoch        string `json:"epoch"`
}

func (g WorkerLeaseGrant) Validate() error {
	if g == (WorkerLeaseGrant{}) {
		return nil
	}
	if g.BotID == "" || g.BrokerNodeID == "" || g.SourceNodeID == "" || (g.Backend != "codex" && g.Backend != "caelis") || g.Epoch == "" {
		return errors.New("worker lease grant must retain complete native authority")
	}
	return nil
}

// A leased resident can delegate only through a target that verifies its live
// paired broker lease at actual native admission and stops owned execution at
// the conservative deadline. Installation/authentication do not prove this.
type LeaseAwareWorkRuntime interface{ LeaseAwareAdmission() bool }
