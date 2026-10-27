package api

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
type SSHTarget struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	// Empty Authentication selects an existing OpenSSH Host. Manual connections
	// carry only connection metadata; passwords and key passphrases use SSHAnswer.
	SSHPort        int    `json:"sshPort,omitempty"`
	User           string `json:"user,omitempty"`
	Authentication string `json:"authentication,omitempty"`
	Identity       string `json:"identity,omitempty"`
}
type SSHIdentity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type SSHHosts struct {
	Hosts []string `json:"hosts"`
	Issue string   `json:"issue,omitempty"`
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
	Host   string     `json:"host"`
	Port   int        `json:"port"`
	ID     string     `json:"id"`
	Value  string     `json:"value"`
	Cancel bool       `json:"cancel"`
	Target *SSHTarget `json:"target,omitempty"`
}
type SSHConnections interface {
	SSHHosts() (SSHHosts, error)
	RegisterSSHIdentity(string) (SSHIdentity, error)
	StartSSHConnection(SSHTarget) (SSHConnectionStatus, error)
	SSHConnectionStatus(SSHTarget) SSHConnectionStatus
	AnswerSSH(SSHAnswer) error
	StopSSHConnection(SSHTarget) error
}
