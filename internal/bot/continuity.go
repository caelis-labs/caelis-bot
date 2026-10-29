package bot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/continuity"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type reviewState struct {
	Version         int       `json:"version"`
	Session         string    `json:"session,omitempty"`
	Evaluated       uint64    `json:"evaluated"`
	BaseTurns       uint64    `json:"baseTurns"`
	BaseCompactions uint64    `json:"baseCompactions"`
	NotBefore       time.Time `json:"notBefore"`
	LastNow         time.Time `json:"lastNow"`
	// Only the producer's frozen message. Delivery outcomes belong exclusively
	// to SystemReceipt; an unknown message keeps this original ID and owner.
	Pending *api.SystemMessage `json:"pending,omitempty"`
	LastID  string             `json:"lastId,omitempty"`
}

type reviewController struct {
	path   string
	state  reviewState
	err    error
	warned bool
}

func openReview(root, provider string) *reviewController {
	r := &reviewController{path: filepath.Join(root, "continuity-"+provider+".json"), state: reviewState{Version: 1}}
	data, err := os.ReadFile(r.path)
	if err == nil {
		if json.Unmarshal(data, &r.state) != nil || r.state.Version != 1 || r.state.Pending != nil && (!r.state.Pending.Valid() || r.state.Pending.Source != api.SystemContinuityReview) {
			r.err = errors.New("invalid continuity observations")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		r.err = err
	}
	return r
}

func (r *reviewController) save(next reviewState) error {
	if err := localstate.Write(r.path, next); err != nil {
		return err
	}
	r.state = next
	return nil
}

// ReviewContinuity is sampled by Tick after normal producers. A new completed
// native turn is the only evaluation edge; elapsed idle time cannot wake a model.
func (r *Runtime) ReviewContinuity(ctx context.Context) error {
	r.step.Lock()
	defer r.step.Unlock()
	c := r.review
	if c == nil {
		return nil
	}
	if c.err != nil {
		if !c.warned && r.diagnostics != nil {
			r.diagnostics.Write(diagnosticlog.Record{Component: "continuity", Code: "storage_unavailable"})
			c.warned = true
		}
		return nil
	}
	if r.paused || r.engine == nil {
		return nil
	}
	observer, ok := r.engine.(continuity.Observer)
	if !ok {
		return nil
	}
	native, ok := r.engine.(interface{ ConversationState() api.ConversationState })
	if !ok {
		return nil
	}
	o, current, now := observer.ContinuityObservation(), native.ConversationState(), r.now()
	next := c.state
	if now.Before(next.LastNow) {
		return nil
	}
	if next.Pending != nil {
		// SubmitSystem reconciles its durable original receipt before admission.
		// Unknown never crosses the native submission boundary twice.
		receipt, err := r.submitSystemLocked(ctx, *next.Pending)
		if receipt.Deferred && receipt.ID == next.Pending.ID {
			// Definitive non-dispatch is the only permission to retire a stale
			// candidate. Never discard an unknown operation on binding change.
			if next.Session != o.Session || now.Sub(next.LastNow) > time.Minute ||
				o.Turns > next.Evaluated && continuity.Confidence(o, next.BaseTurns, next.BaseCompactions, now).Total() < continuity.Threshold {
				next.Pending = nil
				if next.Session == o.Session {
					next.Evaluated = o.Turns
				}
				return c.save(next)
			}
			return err
		}
		if receipt.Outcome == "unknown" || receipt.ID != next.Pending.ID {
			return err
		}
		if receipt.Outcome != "accepted" && receipt.Outcome != "rejected" {
			return err
		}
		next.LastID, next.Pending = next.Pending.ID, nil
		next.NotBefore, next.LastNow = now.Add(continuity.Cooldown), now
		if next.Session == o.Session {
			next.BaseTurns, next.BaseCompactions, next.Evaluated = o.Turns, o.Compactions, o.Turns
		}
		if save := c.save(next); save != nil {
			return save
		}
		r.reviewDiagnostic("invitation_"+receipt.Outcome, next.LastID, "")
		return err
	}
	if !current.Observed || !current.Idle || current.Status != "completed" || o.Session == "" || o.Session != current.Session || o.Turn != current.Turn {
		return nil
	}
	if next.Session != o.Session {
		next.Session, next.Evaluated, next.BaseTurns, next.BaseCompactions = o.Session, 0, 0, 0
	}
	if o.Turns <= next.Evaluated {
		return nil
	}
	next.Evaluated, next.LastNow = o.Turns, now
	// Fresh completion is an event boundary, not an age/usage policy gate.
	if o.CompletedAt.IsZero() || o.CompletedAt.After(now) || now.Sub(o.CompletedAt) > time.Minute || now.Before(next.NotBefore) {
		return c.save(next)
	}
	score := continuity.Confidence(o, next.BaseTurns, next.BaseCompactions, now)
	r.reviewDiagnostic("review_evaluated", "", fmt.Sprintf("age=%.2f turns=%.2f compactions=%.2f input=%.2f threshold=3 input_known=%t", score.Age, score.Turns, score.Compactions, score.Input, o.InputKnown))
	if score.Total() < continuity.Threshold {
		return c.save(next)
	}
	text := fmt.Sprintf("Optional memory review. Observed session age lower bound: %d days; since the last invitation: %d observed completed turns, %d observed successful compactions. Request input: %s. These are limited observations, not stale-content or billing evidence. Current user instructions take priority. Keep working or defer if context is useful. For review, read bot-core references/memory.md. Consolidation and choosing whether/when to call bot_dream are separate decisions. No maintenance announcement is needed; if there is nothing useful to deliver, return exactly %s.", int(max(0, now.Sub(o.Since).Hours()/24)), max(o.Turns, next.BaseTurns)-next.BaseTurns, max(o.Compactions, next.BaseCompactions)-next.BaseCompactions, reviewInput(o), api.SilentReminder)
	next.Pending = &api.SystemMessage{ID: "continuity-" + rand.Text(), Source: api.SystemContinuityReview, Text: text, RuntimeOwner: r.engine.Snapshot().RuntimeOwner}
	if next.Pending.RuntimeOwner == "" {
		next.Pending = nil
		return c.save(next)
	}
	if err := c.save(next); err != nil {
		return err
	}
	r.reviewDiagnostic("invitation_ready", next.Pending.ID, fmt.Sprintf("age=%.2f turns=%.2f compactions=%.2f input=%.2f threshold=3 input_known=%t", score.Age, score.Turns, score.Compactions, score.Input, o.InputKnown))
	// The following Tick uses the same frozen identity, giving queued human
	// input another admission opportunity without holding an adapter lock.
	return nil
}

func reviewInput(o continuity.Observation) string {
	if !o.InputKnown || o.InputCount < 1 || o.InputCount > len(o.Inputs) {
		return "unavailable for the latest turn"
	}
	return fmt.Sprintf("latest ordinary-turn minimum %d tokens; %d comparable turn samples, not retained-context size", o.Inputs[o.InputCount-1], o.InputCount)
}
func (r *Runtime) reviewDiagnostic(code, id, reason string) {
	if r.diagnostics != nil {
		r.diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "continuity", Code: code, RequestID: id, Reason: reason})
	}
}
