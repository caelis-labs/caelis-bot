package caelis

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// OwnedReadiness exposes only public metadata from the designated target store.
// CurrentModel is its actual default; checking another desired model never
// changes that default. AuthenticatedModels reports configured native credential
// availability; it does not validate a provider request. Ready never survives
// an unconfirmed owned shutdown.
type OwnedReadiness struct {
	Ready                bool     `json:"ready"`
	CurrentModel         string   `json:"currentModel,omitempty"`
	Reason               string   `json:"reason,omitempty"`
	NativeConfigRevision string   `json:"nativeConfigRevision,omitempty"`
	AuthenticatedModels  []string `json:"authenticatedModels,omitempty"`
}

type ownedReadinessError struct {
	reason string
	cause  error
}

func (e *ownedReadinessError) Error() string { return e.reason }
func (e *ownedReadinessError) Unwrap() error { return e.cause }
func readinessFailure(reason string, cause error) error {
	return &ownedReadinessError{reason: reason, cause: cause}
}

// ProbeOwnedReadiness performs an explicitly requested, bounded target-native
// readiness check. It requires an existing private ownership marker before any
// launch; it never initializes, adopts, enrolls, authenticates, selects a model,
// or dispatches work. Public model completion is read-only despite using POST
// in the native protocol; initialization/status use GET. No secrets or paths are
// projected, and every launched owned foreground is stopped with native proof.
func ProbeOwnedReadiness(ctx context.Context, opts OwnedHostOptions, desiredModel string) (out OwnedReadiness, err error) {
	if err = ctx.Err(); err != nil {
		out.Reason = "owned-readiness-cancelled"
		return out, readinessFailure(out.Reason, err)
	}
	eligible, reason := ProbeOwnedStore(opts.NodeID, opts.Store)
	if !eligible {
		out.Reason = reason
		return out, nil
	}
	if desiredModel != "" && !publicReadinessModel(desiredModel) {
		out.Reason = "owned-model-invalid"
		return out, readinessFailure(out.Reason, errors.New("invalid public model identifier"))
	}
	bounded, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	host, err := startOwnedHostWithStore(bounded, opts, true)
	if err != nil {
		out.Reason = "owned-host-unavailable"
		return out, readinessFailure(out.Reason, err)
	}
	defer func() {
		stop, finish := context.WithTimeout(context.Background(), 4*time.Second)
		defer finish()
		if stopErr := host.stop(stop); stopErr != nil {
			out.Ready = false
			out.Reason = "owned-host-stop-unconfirmed"
			err = readinessFailure(out.Reason, errors.Join(err, stopErr))
		} else if err == nil && bounded.Err() != nil {
			out.Ready = false
			out.Reason = "owned-readiness-cancelled"
			err = readinessFailure(out.Reason, bounded.Err())
		}
	}()
	return inspectOwnedReadiness(bounded, host, desiredModel)
}

func inspectOwnedReadiness(ctx context.Context, host *ownedHost, desiredModel string) (out OwnedReadiness, err error) {
	if err = host.check(ctx); err != nil {
		out.Reason = "owned-host-unavailable"
		return out, readinessFailure(out.Reason, errors.Join(err, ctx.Err()))
	}
	c, err := setupClient(ctx, host.settings)
	if err != nil {
		out.Reason = "owned-metadata-unavailable"
		return out, readinessFailure(out.Reason, errors.Join(err, ctx.Err()))
	}
	defer c.http.CloseIdleConnections()
	out, err = inspectPublicReadiness(ctx, c, desiredModel, nil)
	if err == nil && out.Ready {
		if err = host.check(ctx); err != nil {
			out.Ready = false
			out.Reason = "owned-host-unavailable"
			return out, readinessFailure(out.Reason, err)
		}
	}
	return out, err
}

type publicCurrentAuthentication struct{ known, authenticated bool }

func inspectPublicReadiness(ctx context.Context, c *client, desiredModel string, current *publicCurrentAuthentication) (out OwnedReadiness, err error) {
	fail := func(reason string, cause error) (OwnedReadiness, error) {
		out.Ready = false
		out.Reason = reason
		return out, readinessFailure(reason, errors.Join(cause, ctx.Err()))
	}
	var before, after wire.StatusSnapshot
	if err = c.json(ctx, "GET", "/status", nil, &before, "", ""); err != nil {
		return fail("owned-metadata-unavailable", err)
	}
	command := "model"
	var models []wire.SlashArgCandidate
	if err = c.json(ctx, "POST", "/completion/slash-arguments", wire.CompletionRequest{Command: &command, Limit: pointer(1000)}, &models, "", ""); err != nil {
		return fail("owned-metadata-unavailable", err)
	}
	if err = c.json(ctx, "GET", "/status", nil, &after, "", ""); err != nil {
		return fail("owned-metadata-unavailable", err)
	}
	if before.Configuration.Revision != after.Configuration.Revision {
		return fail("owned-configuration-changed", errors.New("native configuration changed during readiness"))
	}
	revision := string(after.Configuration.Revision)
	if revision != "" {
		n, e := strconv.ParseUint(revision, 10, 64)
		if e != nil || strconv.FormatUint(n, 10) != revision {
			return fail("owned-metadata-invalid", errors.New("invalid public configuration revision"))
		}
		out.NativeConfigRevision = revision
	}
	currentCount := 0
	alias := value(after.ModelStatus.Alias)
	for _, m := range models {
		flagged := m.ModelSelection != nil && value(m.ModelSelection.Current)
		matched := alias != "" && (m.Value == alias || value(m.ModelConfigId) == alias || value(m.Display) == alias)
		if flagged && alias != "" && !matched {
			return fail("owned-metadata-invalid", errors.New("public current model identities disagree"))
		}
		if flagged || matched {
			currentCount++
			if !publicReadinessModel(m.Value) {
				return fail("owned-metadata-invalid", errors.New("invalid current public model"))
			}
			out.CurrentModel = m.Value
		}
	}
	if currentCount != 1 {
		out.CurrentModel = ""
		out.Reason = "owned-current-model-unavailable"
		return out, nil
	}
	authenticated, knownAuth := map[string]bool{}, map[string]bool{}
	for _, m := range models {
		known, ready := m.NoAuth != nil, m.NoAuth != nil && !value(m.NoAuth)
		// Pinned Core encodes both NoAuth and MissingAPIKey as bool omitempty.
		// A coherent Doctor projection identifies a configured provider candidate;
		// missing_api_key omission then means false, not absent authentication data.
		currentProvider := m.Value == out.CurrentModel && alias != "" && (m.Value == alias || value(m.ModelConfigId) == alias || value(m.Display) == alias) && value(m.ModelConfigId) != "" && value(after.ModelStatus.Provider) != "" && value(after.ModelStatus.Provider) != "acp" && value(after.ModelStatus.Name) != ""
		if currentProvider {
			known = true
			ready = !value(after.ModelStatus.MissingApiKey)
		}
		if m.NoAuth != nil && *m.NoAuth {
			known = true
			ready = false
		}
		if m.Value == out.CurrentModel && value(after.ModelStatus.MissingApiKey) {
			known = true
			ready = false
		}
		if current != nil && m.Value == out.CurrentModel {
			current.known = currentProvider
			current.authenticated = currentProvider && ready
		}
		if known {
			knownAuth[m.Value] = true
		}
		if ready {
			if !publicReadinessModel(m.Value) {
				return fail("owned-metadata-invalid", errors.New("invalid authenticated public model"))
			}
			authenticated[m.Value] = true
			if id := value(m.ModelConfigId); id != "" {
				if !publicReadinessModel(id) {
					return fail("owned-metadata-invalid", errors.New("invalid authenticated public config identity"))
				}
				authenticated[id] = true
			}
		}
	}
	for model := range authenticated {
		out.AuthenticatedModels = append(out.AuthenticatedModels, model)
	}
	sort.Strings(out.AuthenticatedModels)

	selected := desiredModel
	if selected == "" {
		selected = out.CurrentModel
	}
	var selectedModel *wire.SlashArgCandidate
	for i := range models {
		if models[i].Value == selected || value(models[i].ModelConfigId) == selected {
			if selectedModel != nil {
				return fail("owned-metadata-invalid", errors.New("duplicate public model"))
			}
			selectedModel = &models[i]
		}
	}
	if selectedModel == nil {
		out.Reason = "owned-model-unavailable"
		return out, nil
	}
	if !knownAuth[selectedModel.Value] {
		out.Reason = "owned-model-authentication-unconfirmed"
		return out, nil
	}
	if !authenticated[selectedModel.Value] {
		out.Reason = "owned-model-authentication-required"
		return out, nil
	}

	out.Ready = true
	return out, nil
}

func publicReadinessModel(model string) bool {
	return model != "" && len(model) <= 512 && strings.TrimSpace(model) == model && !strings.ContainsAny(model, "\x00\r\n")
}
