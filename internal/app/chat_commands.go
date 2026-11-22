package app

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/telegram"
	"github.com/caelis-labs/caelis-bot/internal/weixin"
)

func (a *Application) legacyRemoteCommand(item api.Item) bool {
	if item.RequestID == "" || item.ID != "channel-input:"+item.RequestID && item.ID != "control-input:"+item.RequestID {
		return false
	}
	if len(item.Artifacts) != 0 || item.Media != nil {
		return false // A file caption is not a transport command.
	}
	origin, _ := a.textControl.OriginOf(item.RequestID)
	switch origin.Channel {
	case "telegram":
		return telegram.RecognizedCommand(item.Text)
	case "weixin":
		return weixin.RecognizedCommand(item.Text)
	}
	return false
}
