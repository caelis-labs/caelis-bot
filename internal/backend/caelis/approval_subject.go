package caelis

import (
	"encoding/json"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// This projection is a display copy. Native permission bytes, targets and
// option IDs remain untouched in ActiveApproval for identity and resolution.
func approvalDisplay(a *wire.ActiveApproval, p nativePermission) api.Approval {
	out := api.Approval{Status: "pending", Choices: []api.Choice{}}
	sensitive, opaque := approvalInputPresence(a.Permission)
	if a.Subject != nil {
		subject := approvalDisplaySubject(a.Subject)
		out.Title = approvalFieldText(subject.Title)
		out.Description = approvalFieldText(subject.Description)
		out.Action = approvalFieldText(&subject.Operation)
		var targets []string
		for _, target := range subject.Targets {
			if text := approvalFieldText(&target); text != "" {
				targets = append(targets, text)
			}
		}
		out.Target = strings.Join(targets, "\n")
		// An explicit unavailable/opaque/sensitive field is not permission to
		// reconstruct it from older tool input or endpoint-private context.
		if !sensitive && !opaque && !value(subject.SensitiveInput) && !value(subject.OpaqueInput) {
			if subject.Title == nil {
				out.Title = permissionTitle(p)
			}
			if subject.Description == nil {
				out.Description = permissionDescription(p)
			}
		}
		details, _ := json.MarshalIndent(struct {
			Subject *wire.ApprovalSubject `json:"subject"`
		}{subject}, "", "  ")
		out.Details = string(details)
	} else {
		if !sensitive && !opaque {
			out.Title, out.Description = permissionTitle(p), permissionDescription(p)
			out.Action = out.Title
		}
		var legacyInput map[string]any
		knownLegacy := json.Unmarshal(p.ToolCall.RawInput, &legacyInput) == nil && legacyInput != nil && len(p.Options) != 0
		if knownLegacy && !sensitive && !opaque {
			// Preserve the already supported, unmarked legacy tool display. It
			// does not make opaque requests eligible for Agent decisions.
			details, _ := json.MarshalIndent(p.ToolCall.RawInput, "", "  ")
			out.Details = string(details)
		} else {
			// A future permission shape is not a private Bot protocol. Retain
			// its presence and original native choices without rendering an
			// arbitrary body that can contain sensitive endpoint evidence.
			details, _ := json.MarshalIndent(struct {
				State          string `json:"state"`
				SensitiveInput bool   `json:"sensitive_input,omitempty"`
				OpaqueInput    bool   `json:"opaque_input,omitempty"`
			}{"opaque", sensitive, opaque}, "", "  ")
			out.Details = string(details)
		}
	}
	if out.Title == "" {
		out.TitleKey = "chat.caelisApproval"
	}
	return out
}

func permissionTitle(p nativePermission) string {
	if p.Title != "" {
		return p.Title
	}
	if p.Context != nil && p.Context.Title != "" {
		return p.Context.Title
	}
	return p.ToolCall.Title
}

func permissionDescription(p nativePermission) string {
	if p.Description != "" {
		return p.Description
	}
	if p.Context != nil {
		return p.Context.Description
	}
	return ""
}

func approvalFieldText(field *wire.ApprovalField) string {
	if field == nil || value(field.Sensitive) {
		return ""
	}
	if field.State != wire.ApprovalFieldStateKnown && field.State != wire.ApprovalFieldStateTruncated {
		return ""
	}
	return value(field.Text)
}

func approvalDisplayField(field wire.ApprovalField) wire.ApprovalField {
	if value(field.Sensitive) {
		field.State = wire.ApprovalFieldStateOpaque
	}
	if field.State != wire.ApprovalFieldStateKnown && field.State != wire.ApprovalFieldStateTruncated {
		field.Text = nil
	}
	return field
}

func approvalDisplaySubject(in *wire.ApprovalSubject) *wire.ApprovalSubject {
	out := *in
	displayField := approvalDisplayField
	if value(in.Reported) && (value(in.SensitiveInput) || value(in.OpaqueInput)) {
		// Core's public display contract withdraws remote-derived text as a
		// whole when it may repeat private evidence. Keep presence and option
		// joins, including when an older owner omitted that final redaction.
		displayField = func(field wire.ApprovalField) wire.ApprovalField {
			field = approvalDisplayField(field)
			if field.State != wire.ApprovalFieldStateUnavailable {
				field.State = wire.ApprovalFieldStateOpaque
			}
			field.Text = nil
			return field
		}
	}
	if in.Title != nil {
		out.Title = pointer(displayField(*in.Title))
	}
	if in.Description != nil {
		out.Description = pointer(displayField(*in.Description))
	}
	out.Tool, out.Operation = displayField(in.Tool), displayField(in.Operation)
	out.Effect, out.Scope = displayField(in.Effect), displayField(in.Scope)
	out.Targets = make([]wire.ApprovalField, len(in.Targets))
	for i, target := range in.Targets {
		out.Targets[i] = displayField(target)
	}
	out.Grants = make([]wire.ApprovalGrant, len(in.Grants))
	for i, grant := range in.Grants {
		out.Grants[i] = wire.ApprovalGrant{OptionId: grant.OptionId, Scope: displayField(grant.Scope)}
	}
	return &out
}

// Explicit presence markers only restrict display. They never identify a new
// request type or confer decision authority on an unfamiliar native payload.
func approvalInputPresence(input any) (sensitive, opaque bool) {
	switch v := input.(type) {
	case wire.JSONObject:
		return approvalInputPresence(map[string]any(v))
	case map[string]any:
		for key, field := range v {
			if present, ok := field.(bool); ok && present {
				sensitive = sensitive || key == "sensitive" || key == "sensitive_input"
				opaque = opaque || key == "opaque_input"
			}
			s, o := approvalInputPresence(field)
			sensitive, opaque = sensitive || s, opaque || o
		}
	case []any:
		for _, field := range v {
			s, o := approvalInputPresence(field)
			sensitive, opaque = sensitive || s, opaque || o
		}
	}
	return
}
