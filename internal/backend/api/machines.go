package api

import "context"

type Machine struct {
	SavedAddress   string `json:"savedAddress,omitempty"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	RemoteName     string `json:"remoteName,omitempty"`
	Address        string `json:"address"`
	ConnectionType string `json:"connectionType"`
	State          string `json:"state"`
	Issue          string `json:"issue"`
	Runtime        string `json:"runtime"`
	CheckedAt      int64  `json:"checkedAt,omitempty"`
	Archived       bool   `json:"archived,omitempty"`
	PeerID         string `json:"peerId,omitempty"`
	PairingStatus  string `json:"pairingStatus"`
}
type BotConnectionInput struct {
	Reconnect bool   `json:"reconnect,omitempty"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
}
type BotConnectionController interface {
	ConnectBot(context.Context, BotConnectionInput) (Machine, error)
}
type BotWorkRecovery interface {
	RecoverBotWork(context.Context, string) (Machine, error)
}
type MachineController interface {
	Machines() []Machine
	InspectMachine(context.Context, string, string) (Machine, error)
	RemoveMachine(context.Context, string) error
}
type WorkerPeerSource struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PairedAt int64  `json:"pairedAt"`
}

// Name is self-reported; the fingerprint binds the exact source/proof. An
// observed source address (including loopback) never implies authorization.
type WorkerPairingRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	Address     string `json:"address"`
	ExpiresAt   int64  `json:"expiresAt"`
}
type WorkerPairingDecision struct {
	ID    string `json:"id"`
	Allow bool   `json:"allow"`
}
type WorkerPairingRequests interface {
	DecideWorkerPairing(context.Context, WorkerPairingDecision) error
}
type WorkerSharing struct {
	Issue     string                 `json:"issue,omitempty"`
	Requests  []WorkerPairingRequest `json:"requests"`
	Sources   []WorkerPeerSource     `json:"sources"`
	Address   string                 `json:"address"`
	Listen    string                 `json:"listen"`
	Listening bool                   `json:"listening"`
	Managed   bool                   `json:"managed"`
	Peer      string                 `json:"peer"`
}

type WorkerSharingController interface {
	WorkerSharing() WorkerSharing
	RevokeWorkerPeer(string) error
}

// Host-captured correlation for the one original request, never a bearer token
// or a second authorization authority. Ingress persists this before delivery.
type BotConnectionPreparation struct {
	Read    bool `json:"read,omitempty"`
	Created bool `json:"created,omitempty"`
	Rename  bool `json:"rename,omitempty"`
}
type BotConnectionReference struct {
	Preparation   *BotConnectionPreparation `json:"preparation,omitempty"`
	Pairing       string                    `json:"pairing"`
	Configuration string                    `json:"configuration"`
	ID            string                    `json:"id"`
	Address       string                    `json:"address"`
	Peer          string                    `json:"peer,omitempty"`
	Proof         string                    `json:"proof"`
}
type BotConnectionUnconfirmedError struct {
	Reference BotConnectionReference
	Cause     error
}

func (e *BotConnectionUnconfirmedError) Error() string { return e.Cause.Error() }
func (e *BotConnectionUnconfirmedError) Unwrap() error { return e.Cause }

type BotConnectionReceiptReader interface {
	ReadBotConnectionReceipt(context.Context, BotConnectionReference) (Machine, error)
}
type BotConnectionLifecycle interface {
	PrepareBotConnection(BotConnectionInput, *BotConnectionReference) (BotConnectionReference, error)
	ConnectPreparedBot(context.Context, BotConnectionReference, BotConnectionInput) (Machine, BotConnectionReference, error)
	AbortPreparedBotConnection(BotConnectionReference) error
	BotConnectionQuiescent(BotConnectionReference) bool
}

// Local list metadata has its own original operation identity. It never pairs,
// unpairs, reconnects or stops a running channel.
type MachineMutation struct {
	Address   string `json:"address,omitempty"`
	ID        string `json:"id"`
	MachineID string `json:"machineId"`
	Action    string `json:"action"`
	Name      string `json:"name,omitempty"`
}
type MachineMutationResult struct {
	ID      string  `json:"id"`
	Status  string  `json:"status"`
	Code    string  `json:"code,omitempty"`
	Machine Machine `json:"machine"`
	Digest  string  `json:"digest,omitempty"`
}
