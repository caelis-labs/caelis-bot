package productmanagement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Selection is the existing Bot preference subset. Permissions, service tiers,
// provider configuration and credentials cannot be changed through this port.
type Selection struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type ExecutionView struct {
	Scope
	ConversationDefault bool              `json:"conversationDefault"`
	Conversation        Selection         `json:"conversation"`
	Work                *Selection        `json:"work,omitempty"`
	Revision            string            `json:"revision"`
	Models              []api.ModelOption `json:"models"`
}

type ExecutionCommand struct {
	Scope
	ID               string    `json:"id"`
	Target           string    `json:"target"`
	ExpectedRevision string    `json:"expectedRevision"`
	Selection        Selection `json:"selection"`
}

type ExecutionResult struct {
	Scope
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	Code    string `json:"code,omitempty"`
}

// ExecutionState is native only, never a wire payload. Its revision covers the
// policy fields preserved when the selected model/effort is applied atomically.
type ExecutionState struct {
	ConversationDefault bool
	Conversation        api.ExecutionSettings
	Work                *api.WorkExecutionSettings
}

var (
	ErrExecutionConflict    = errors.New("execution revision changed")
	ErrExecutionInvalid     = errors.New("invalid model selection")
	ErrExecutionUnavailable = errors.New("execution settings unavailable")
	ErrExecutionCancelled   = errors.New("execution cancelled before dispatch")
)

type ExecutionSource interface {
	ReadModelSettings(context.Context) (ExecutionState, []api.ModelOption, error)
	ApplyModelSettings(context.Context, string, string, Selection) error
}

// ExecutionPort is optional and independent of Host installation/configuration.
// Its owner journals original IDs before dispatch; it never retries mutations.
type ExecutionPort interface {
	ExecutionSettings(context.Context, Scope) (ExecutionView, error)
	ChangeExecutionSettings(context.Context, ExecutionCommand) (ExecutionResult, error)
}

type ExecutionController struct {
	scope  Scope
	source ExecutionSource
}

func NewExecution(scope Scope, source ExecutionSource) (*ExecutionController, error) {
	if !validScope(scope) || source == nil {
		return nil, ErrExecutionUnavailable
	}
	return &ExecutionController{scope: scope, source: source}, nil
}

func ExecutionRevision(state ExecutionState) string {
	b, _ := json.Marshal(state)
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

func ValidExecutionCommand(c ExecutionCommand) bool {
	if !identifier.MatchString(c.ID) || (c.Target != "conversation" && c.Target != "work") || len(c.ExpectedRevision) != 64 {
		return false
	}
	if _, err := hex.DecodeString(c.ExpectedRevision); err != nil {
		return false
	}
	return validSelection(c.Selection)
}
func validSelection(v Selection) bool {
	return publicExecutionText(v.Model, 256) && publicExecutionText(v.Effort, 64) && api.ValidateExecutionSettings(api.ExecutionSettings{Model: v.Model, Effort: v.Effort}) == nil
}
func publicExecutionText(v string, max int) bool {
	return len(v) <= max && utf8.ValidString(v) && !strings.ContainsAny(v, "\x00\r\n")
}

// Only the typed public model catalog is exposed. Bounded fields exclude any
// extensible provider/native metadata object or raw configuration response.
func ValidExecutionView(v ExecutionView) bool {
	if !validScope(v.Scope) || !validSelection(v.Conversation) || (v.Work != nil && !validSelection(*v.Work)) || len(v.Revision) != 64 || len(v.Models) > 2000 {
		return false
	}
	if _, err := hex.DecodeString(v.Revision); err != nil {
		return false
	}
	seen := make(map[string]bool)
	for _, m := range v.Models {
		if m.Model == "" || seen[m.Model] || !publicExecutionText(m.Model, 256) || !publicExecutionText(m.Name, 512) || len(m.Description) > 8192 || !utf8.ValidString(m.Description) || strings.ContainsRune(m.Description, '\x00') || !publicExecutionText(m.DefaultEffort, 64) || len(m.Efforts) > 32 || len(m.ServiceTiers) > 32 {
			return false
		}
		seen[m.Model] = true
		for _, e := range m.Efforts {
			if !publicExecutionText(e, 64) {
				return false
			}
		}
		for _, t := range m.ServiceTiers {
			if !publicExecutionText(t.ID, 64) || !publicExecutionText(t.Name, 512) || len(t.Description) > 8192 || !utf8.ValidString(t.Description) || strings.ContainsRune(t.Description, '\x00') {
				return false
			}
		}
	}
	return true
}

func (c *ExecutionController) ExecutionSettings(ctx context.Context, scope Scope) (ExecutionView, error) {
	if scope != c.scope {
		return ExecutionView{}, ErrExecutionUnavailable
	}
	state, models, err := c.source.ReadModelSettings(ctx)
	if err != nil {
		return ExecutionView{}, err
	}
	v := ExecutionView{ConversationDefault: state.ConversationDefault, Scope: c.scope, Conversation: Selection{state.Conversation.Model, state.Conversation.Effort}, Revision: ExecutionRevision(state), Models: models}
	if state.Work != nil {
		v.Work = &Selection{state.Work.Model, state.Work.Effort}
	}
	if !ValidExecutionView(v) {
		return ExecutionView{}, ErrExecutionInvalid
	}
	return v, nil
}
func (c *ExecutionController) ChangeExecutionSettings(ctx context.Context, in ExecutionCommand) (ExecutionResult, error) {
	r := ExecutionResult{Scope: c.scope, ID: in.ID, Outcome: "rejected", Code: "invalid-command"}
	if in.Scope != c.scope || !ValidExecutionCommand(in) {
		return r, nil
	}
	if ctx.Err() != nil {
		r.Code = "cancelled-before-dispatch"
		return r, nil
	}
	err := c.source.ApplyModelSettings(ctx, in.ExpectedRevision, in.Target, in.Selection)
	switch {
	case err == nil:
		r.Outcome, r.Code = "accepted", ""
	case errors.Is(err, ErrExecutionConflict):
		r.Code = "execution-revision-conflict"
	case errors.Is(err, ErrExecutionInvalid):
		r.Code = "invalid-model-selection"
	case errors.Is(err, ErrExecutionUnavailable):
		r.Code = "execution-unavailable"
	case errors.Is(err, ErrExecutionCancelled):
		r.Code = "cancelled-before-dispatch"
	default:
		r.Outcome, r.Code = "unknown", "native-operation-unresolved"
	}
	// The source error remains observable internally; the closed public receipt
	// never serializes provider errors that may contain native details.
	if r.Outcome == "unknown" {
		return r, err
	}
	return r, nil
}
