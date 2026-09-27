package caelis

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

const modelCapabilities = "application-model-capabilities-v1"

func (s *Session) ImageInput(ctx context.Context) (api.ImageInputCapability, error) {
	s.mu.Lock()
	c, sid := s.client, s.state.Session.SessionId
	available := s.connected && !s.closed && slices.Contains(s.info.Capabilities, modelCapabilities)
	s.mu.Unlock()
	out := api.ImageInputCapability{State: "unknown"}
	if !available || c == nil || sid == "" {
		return out, nil
	}
	// The creation profile and Host-wide union can both describe a different
	// model. Observe the current application configuration through its scoped API.
	var observed wire.ApplicationModelCapabilities
	if err := c.json(ctx, "GET", "/application/sessions/"+idPath(sid)+"/model-capabilities", nil, &observed, "", ""); err != nil {
		return out, err
	}
	revision, err := strconv.ParseUint(string(observed.ConfigurationRevision), 10, 64)
	if err != nil || revision == 0 || observed.SessionId != sid || strings.TrimSpace(observed.Model) == "" {
		return out, errors.New("Caelis 模型能力回执不匹配")
	}
	out.Model = observed.Model
	if observed.ImageInput != nil {
		out.State = "unsupported"
		if *observed.ImageInput {
			out.State = "supported"
		}
	}
	return out, nil
}
