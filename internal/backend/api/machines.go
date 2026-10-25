package api

import "context"

type MachineInput struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Address          string `json:"address"`
	Port             int    `json:"port"`
	User             string `json:"user"`
	Authentication   string `json:"authentication"`
	SSHConfig        bool   `json:"sshConfig"`
	PrivateKey       string `json:"privateKey"`
	Secret           string `json:"secret"`
	Remember         bool   `json:"remember"`
	TrustFingerprint string `json:"trustFingerprint"`
}
type Machine struct {
	RemoteName     string                 `json:"remoteName,omitempty"`
	CheckedAt      int64                  `json:"checkedAt,omitempty"`
	Archived       bool                   `json:"archived,omitempty"`
	ConnectionType string                 `json:"connectionType,omitempty"`
	PeerID         string                 `json:"peerId,omitempty"`
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Address        string                 `json:"address"`
	Port           int                    `json:"port"`
	User           string                 `json:"user"`
	Authentication string                 `json:"authentication"`
	SSHConfig      bool                   `json:"sshConfig"`
	PrivateKey     string                 `json:"privateKey"`
	Remember       bool                   `json:"remember"`
	State          string                 `json:"state"`
	Issue          string                 `json:"issue"`
	Fingerprint    string                 `json:"fingerprint"`
	Runtime        string                 `json:"runtime"`
	Available      []string               `json:"available"`
	Setup          SetupState             `json:"setup"`
	Work           WorkExecutionSettings  `json:"work"`
	Models         []ModelOption          `json:"models"`
	RuntimeDefault *WorkExecutionSettings `json:"runtimeDefault"`
	Configuration  *RuntimeConfiguration  `json:"configuration,omitempty"`
	AdvancedIssue  string                 `json:"advancedIssue"`
}

type BotConnectionInput struct {
	Reconnect bool   `json:"reconnect,omitempty"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Code      string `json:"code"`
}
type BotConnectionController interface {
	ConnectBot(context.Context, BotConnectionInput) (Machine, error)
}
type BotWorkRecovery interface {
	RecoverBotWork(context.Context, string) (Machine, error)
}
type MachineModel struct {
	ID        string                `json:"id"`
	Selection WorkExecutionSettings `json:"selection"`
}
type MachineTeamChange struct {
	ID     string                     `json:"id"`
	Change RuntimeConfigurationChange `json:"change"`
}
type MachineController interface {
	Machines() []Machine
	SSHConfigHosts() ([]string, error)
	ConnectMachine(context.Context, MachineInput) (Machine, error)
	InspectMachine(context.Context, string, string) (Machine, error)
	SaveMachineModel(context.Context, MachineModel) (Machine, error)
	ChangeMachineTeam(context.Context, MachineTeamChange) (RuntimeMutationResult, error)
	MachineTerminal(context.Context, string) (TerminalTarget, error)
	ReadMachineAdvanced(context.Context, string) (Machine, error)
	RemoveMachine(context.Context, string) error
}

type WorkerPeerSource struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PairedAt int64  `json:"pairedAt"`
}
type WorkerSharing struct {
	Enabled   bool               `json:"enabled"`
	Address   string             `json:"address"`
	Listen    string             `json:"listen"`
	Listening bool               `json:"listening"`
	Managed   bool               `json:"managed"`
	Peer      string             `json:"peer"`
	Code      string             `json:"code,omitempty"`
	ExpiresAt int64              `json:"expiresAt,omitempty"`
	Sources   []WorkerPeerSource `json:"sources"`
}
type WorkerSharingInput struct {
	Listen  string `json:"listen"`
	Address string `json:"address"`
}
type WorkerSharingEndpoint interface {
	SetWorkerEndpoint(WorkerSharingInput) (WorkerSharing, error)
}
type WorkerSharingRecovery interface {
	RecoverWorkerSharing(context.Context) (WorkerSharing, error)
}
type WorkerSharingController interface {
	WorkerSharing() WorkerSharing
	SetWorkerSharing(bool) (WorkerSharing, error)
	CreateWorkerPairing(context.Context) (WorkerSharing, error)
	RevokeWorkerPeer(string) error
}

// BotConnectionReference is a host-captured original proof reference, never a
// credential or browser-supplied authority. Reads and explicit fresh authorization
// resolve this reference at its existing machines owner; it never grants access.
// Admission metadata contains only original intent/rollback correlation, never
// the pairing code or bearer token. The machines owner alone consumes it.
type BotConnectionPreparation struct {
	PreviousPairing    string `json:"previousPairing,omitempty"`
	PreviousCodeDigest string `json:"previousCodeDigest,omitempty"`
	Configuration      string `json:"configuration"`
	Name               string `json:"name"`
	Retained           bool   `json:"retained,omitempty"`
	Created            bool   `json:"created,omitempty"`
	Rename             bool   `json:"rename,omitempty"`
}
type BotConnectionReference struct {
	Preparation   *BotConnectionPreparation `json:"preparation,omitempty"`
	CodeDigest    string                    `json:"codeDigest,omitempty"`
	Pairing       string                    `json:"pairing,omitempty"`
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

// Explicit new user authorization; OriginalOperationID is an ingress reference
// only on Web. It never replaces an original pairing/task operation identity.
type BotConnectionAuthorization struct {
	Connection          BotConnectionInput `json:"connection"`
	OriginalOperationID string             `json:"originalOperationId,omitempty"`
}
type BotConnectionAuthorizer interface {
	BotAuthorizationReference(string) (BotConnectionReference, error)
	AuthorizeBotConnection(context.Context, BotConnectionReference, BotConnectionInput) (Machine, BotConnectionReference, error)
	ValidateBotAuthorizationCode(BotConnectionReference, string) error
}

// Host-only pre-admission seam on the existing machines pairing owner.
type BotConnectionLifecycle interface {
	PrepareBotConnection(BotConnectionInput, *BotConnectionReference) (BotConnectionReference, error)
	ConnectPreparedBot(context.Context, BotConnectionReference, BotConnectionInput) (Machine, BotConnectionReference, error)
	AbortPreparedBotConnection(BotConnectionReference) error
	BotConnectionQuiescent(BotConnectionReference) bool
}
