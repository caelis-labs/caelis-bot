package backend

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

var chatCardRequest = regexp.MustCompile(`^a2ui-[a-f0-9]{32}-[a-f0-9]{32}-[0-9a-z]{1,6}$`)
var chatCardField = regexp.MustCompile(`^[\pL_][\pL\pN_]{0,39}$`)

func (s *Service) SubmitChatCard(ctx context.Context, id, text string, fields []api.ChatCardField) (api.Receipt, error) {
	rejected := api.Receipt{ID: id, Outcome: "rejected"}
	text = strings.TrimSpace(text)
	if s.chat == nil || !chatCardRequest.MatchString(id) || text == "" || len(text) > 64<<10 || len(fields) > 12 {
		return rejected, nil
	}
	size := 0
	names := make(map[string]bool, len(fields))
	for _, field := range fields {
		size += len(field.Name) + len(field.Value)
		if !chatCardField.MatchString(field.Name) || names[field.Name] || strings.TrimSpace(field.Value) == "" || size > 64<<10 {
			return rejected, nil
		}
		names[field.Name] = true
	}

	card := &api.ChatCardSnapshot{Fields: append(make([]api.ChatCardField, 0, len(fields)), fields...)}
	input := api.Submission{ID: id, Text: text, ChatCard: card}
	before, err := s.chat.ClaimCardInput(ctx, api.Item{
		ID: "outgoing:" + id, RequestID: id, Kind: "user", Text: text, Status: "unknown",
		SeenAt: time.Now().UnixMicro(), ChatCard: card,
	})
	if err != nil {
		// The history read could not establish whether this ID already owns
		// an attempt. Keep it fenced until the original receipt is readable.
		return api.Receipt{ID: id, Outcome: "unknown"}, nil
	}
	if before != nil {
		if before.ChatCard == nil || before.Text != text {
			return api.Receipt{ID: id, Outcome: "unknown"}, nil
		}
		switch before.Status {
		case "completed", "accepted":
			return api.Receipt{ID: id, Outcome: "accepted"}, nil
		case "rejected":
			return rejected, nil
		default:
			return api.Receipt{ID: id, Outcome: "unknown"}, nil
		}
	}
	receipt, err := s.submitChat(ctx, input, false)
	if errors.Is(err, api.ErrRecoveryPending) {
		receipt = rejected
	}
	if receipt.ID != id || receipt.Outcome != "accepted" && receipt.Outcome != "rejected" {
		receipt = api.Receipt{ID: id, Outcome: "unknown"}
	}
	// Admission can reject before submitChat stages the user's row. Settle its
	// existing claim as well, using the same IM and original request identity.
	s.recordInput(input, nil, receipt)
	return receipt, nil
}
