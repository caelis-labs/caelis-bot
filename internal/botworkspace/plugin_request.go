package botworkspace

import (
	"errors"
	"regexp"
)

const PluginRequestName = "bot-plugin-request.json"

func (p PluginRequest) StatusName() string { return "bot-plugin-status-" + p.ID + ".json" }

// PluginRequest is an intent. Only the running app may mutate its plugin store.
type PluginRequest struct {
	ID       string `json:"id"`
	Action   string `json:"action"`
	PluginID string `json:"pluginId"`
	Version  string `json:"version,omitempty"`
}

var pluginRequestID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var pluginRequestToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)

func (p PluginRequest) HasValidID() bool { return pluginRequestID.MatchString(p.ID) }

func (p PluginRequest) Validate() error {
	if !p.HasValidID() || !pluginRequestToken.MatchString(p.PluginID) || (p.Version != "" && !pluginRequestToken.MatchString(p.Version)) {
		return errors.New("invalid plugin request")
	}
	switch p.Action {
	case "install":
		return nil
	case "update":
		if p.Version != "" {
			return nil
		}
	case "enable", "disable", "uninstall":
		if p.Version == "" {
			return nil
		}
	}
	return errors.New("invalid plugin action or version")
}
