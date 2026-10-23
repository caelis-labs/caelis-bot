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
