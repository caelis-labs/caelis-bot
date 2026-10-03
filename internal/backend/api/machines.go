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
