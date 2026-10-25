package api

import "context"

// SSH is network transport only. Bot authorization is a separate original peer request.
type SSHConfiguration struct {
	Path       string   `json:"path"`
	UserPath   string   `json:"userPath"`
	SystemPath string   `json:"systemPath"`
	Text       string   `json:"text"`
	Revision   string   `json:"revision"`
	Hosts      []string `json:"hosts"`
	Issue      string   `json:"issue,omitempty"`
}
type SSHConfigurationSave struct {
	Text     string `json:"text"`
	Revision string `json:"revision"`
}
type SSHTarget struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}
type SSHPrompt struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Confirm bool   `json:"confirm"`
}
type SSHConnectionStatus struct {
	State  string     `json:"state"`
	Issue  string     `json:"issue,omitempty"`
	Prompt *SSHPrompt `json:"prompt,omitempty"`
}
type SSHAnswer struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	ID     string `json:"id"`
	Value  string `json:"value"`
	Cancel bool   `json:"cancel"`
}
type SSHConnections interface {
	SSHConfiguration() (SSHConfiguration, error)
	SaveSSHConfiguration(context.Context, SSHConfigurationSave) (SSHConfiguration, error)
	StartSSHConnection(SSHTarget) (SSHConnectionStatus, error)
	SSHConnectionStatus(SSHTarget) SSHConnectionStatus
	AnswerSSH(SSHAnswer) error
	StopSSHConnection(SSHTarget) error
}

// Scope is a host-owned selector, never an arbitrary client file path.
type SSHConfigurationDocument struct {
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}
type SSHConfigurationFiles interface {
	ReadSSHConfiguration(string) (SSHConfigurationDocument, error)
	PrepareSSHConfigurationEdit(string) (SSHConfigurationDocument, error)
}
