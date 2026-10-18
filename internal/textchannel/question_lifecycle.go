package textchannel

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const asyncQuestionMaxAge = 24 * time.Hour

func asyncKey(q asyncQuestion) string { return fmt.Sprintf("%s:%d", q.Fingerprint, q.Index) }
func asyncExpired(q asyncQuestion) bool {
	deadline := q.ExpiresAt
	if deadline == 0 {
		deadline = q.IssuedAt + int64(asyncQuestionMaxAge/time.Second)
	}
	return deadline > 0 && time.Now().Unix() >= deadline
}
func closedAsync(state string) bool {
	return state == "dismissed" || state == "expired" || state == "superseded"
}
func questionCard(q AsyncButtonQuestion) api.QuestionCard {
	card := api.QuestionCard{ShortID: q.ShortID, Binding: q.Fingerprint, Title: q.Title, Options: q.Options, State: q.State, TaskTitle: q.TaskTitle, ExpiresAt: q.ExpiresAt, Reason: q.Reason}
	if q.Presentation != nil {
		card.Title, card.Context, card.Options = q.Presentation.Title, q.Presentation.Context, append([]string(nil), q.Presentation.Options...)
	}
	return card
}
func (s *Store) closeAsyncLocked(id string, q asyncQuestion, state, reason string) {
	q.State, q.Reason, q.ClosedAt = state, reason, time.Now().Unix()
	s.state.AsyncQuestions[id] = q
	s.state.AsyncClosed[asyncKey(q)] = q
}

// ReconcileAsync also runs on the host clock: offline and idle questions expire.
// Missing bounded history and an unobserved Worker are never terminal evidence.
func (s *Store) ReconcileAsync(snapshot api.Snapshot, work []api.WorkState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, closed := maps.Clone(s.state.AsyncQuestions), maps.Clone(s.state.AsyncClosed)
	changed := false
	for id, q := range s.state.AsyncQuestions {
		if q.State != "" {
			continue
		}
		state, reason := "", ""
		if asyncExpired(q) {
			state, reason = "expired", "deadline"
		}
		if q.WorkID == "" && snapshot.RuntimeOwner != "" && snapshot.RuntimeOwner != q.Owner && (snapshot.Connection == "ready" || snapshot.Connection == "connected") {
			state, reason = "superseded", "owner_replaced"
		}
		for _, w := range work {
			if q.WorkID == "" || w.Task.ID != q.WorkID || w.Runtime != q.Owner {
				continue
			}
			if q.TaskTitle != w.Task.Title {
				q.TaskTitle = w.Task.Title
				s.state.AsyncQuestions[id] = q
				changed = true
			}
			switch {
			case w.StopRequested || w.Task.Status == "interrupted" || w.Task.Status == "cancelled" || w.Task.Status == "canceled":
				state, reason = "expired", "task_cancelled"
			case w.Task.Status == "failed" || w.Task.Status == "unavailable":
				state, reason = "expired", "task_ended"
			case w.Task.Status == "completed" && w.Task.Result != "" && w.TurnKey != q.TurnKey:
				state, reason = "expired", "task_completed"
			}
		}
		if state != "" {
			s.closeAsyncLocked(id, q, state, reason)
			changed = true
		}
	}
	if changed {
		if err := s.save(); err != nil {
			s.state.AsyncQuestions, s.state.AsyncClosed = previous, closed
			return err
		}
	}
	return nil
}

// ProjectQuestions always resolves cached IM cards against the durable ledger.
// An old cache or reused Q number cannot revive a pending control.
func (s *Store) ProjectQuestions(items []api.Item) ([]api.Item, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := append([]api.Item(nil), items...)
	for i, item := range result {
		if len(item.QuestionCards) == 0 {
			continue
		}
		item.QuestionCards = append([]api.QuestionCard(nil), item.QuestionCards...)
		for n, card := range item.QuestionCards {
			q, ok := s.state.AsyncQuestions[card.ShortID]
			if !ok || q.Fingerprint != card.Binding || q.ItemID != item.ID {
				ok = false
				for _, old := range s.state.AsyncClosed {
					if old.ItemID == item.ID && old.Fingerprint == card.Binding && old.ShortID == card.ShortID {
						q, ok = old, true
						break
					}
				}
			}
			if ok {
				card.State, card.Reason, card.ExpiresAt, card.TaskTitle = q.State, q.Reason, q.ExpiresAt, q.TaskTitle
				if q.Presentation != nil {
					card.Title, card.Context, card.Options = q.Presentation.Title, q.Presentation.Context, append([]string(nil), q.Presentation.Options...)
				}
			} else {
				card.State, card.Reason = "expired", "reference_retired"
			}
			item.QuestionCards[n] = card
		}
		result[i] = item
	}
	return result, s.revision + uint64(len(s.state.ReplyOrder))
}

// Dismiss never dispatches an answer, approval, or a new Worker turn.
func (s *Store) DismissQuestion(_ context.Context, requestID, id, binding string) string {
	if requestID == "" || binding == "" {
		return "问题暂不可用。"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.state.AsyncQuestions[id]
	if !ok || q.Fingerprint != binding {
		return "这条问题卡片已失效，请使用最新卡片。"
	}
	if q.State == "answered" {
		return "回答已提交，不会撤回答案。"
	}
	if q.State == "unknown" || q.State == "dispatching" {
		return "回答投递结果待核对，不能撤回或重复提交。"
	}
	if closedAsync(q.State) {
		return "问题已关闭，未提供答案。"
	}
	state, reason := "dismissed", "user_dismissed"
	if asyncExpired(q) {
		state, reason = "expired", "deadline"
	}
	s.closeAsyncLocked(id, q, state, reason)
	if err := s.save(); err != nil {
		s.state.AsyncQuestions[id] = q
		delete(s.state.AsyncClosed, asyncKey(q))
		return "本地记录不可用，问题未关闭；请重试。"
	}
	return "问题已关闭，未提供答案。"
}

func (s *Store) PresentWorkerQuestion(id string, presentation *api.QuestionPresentation) (api.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.state.AsyncQuestions[id]
	if !ok || q.WorkID == "" || q.State != "" || asyncExpired(q) || s.state.AsyncActive[q.ItemID] != q.Fingerprint {
		return api.Item{}, fmt.Errorf("original Worker question is no longer pending")
	}
	if presentation != nil {
		if err := validateQuestionPresentation(*presentation, len(q.Options)); err != nil {
			return api.Item{}, err
		}
		// Re-presentation cannot silently relabel choices a user may already be reading.
		if q.Presented && (q.Presentation == nil || q.Presentation.Title != presentation.Title || q.Presentation.Context != presentation.Context || !slices.Equal(q.Presentation.Options, presentation.Options)) {
			return api.Item{}, fmt.Errorf("question already presented with different wording")
		}
	}
	old := q
	if presentation != nil {
		value := *presentation
		value.Options = append([]string(nil), presentation.Options...)
		q.Presentation = &value
	}
	q.Presented = true
	s.state.AsyncQuestions[id] = q
	if err := s.save(); err != nil {
		s.state.AsyncQuestions[id] = old
		return api.Item{}, err
	}
	item := api.Item{ID: q.ItemID, TurnKey: q.TurnKey, Kind: "assistant", Status: "completed", Text: s.asyncCardTextForLocked(q.ItemID, q.Fingerprint, true)}
	for _, entry := range s.asyncQuestionsLocked(q.ItemID, q.Fingerprint) {
		field := entry.Question
		if field.Presented {
			item.QuestionCards = append(item.QuestionCards, questionCard(AsyncButtonQuestion{ShortID: entry.ID, Fingerprint: field.Fingerprint, Title: field.Title, Options: append([]string(nil), field.Options...), State: field.State, TaskTitle: field.TaskTitle, Reason: field.Reason, ExpiresAt: field.ExpiresAt, Presentation: field.Presentation}))
		}
	}
	return item, nil
}
func (s *Store) PresentedQuestion(itemID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.state.AsyncQuestions {
		if q.ItemID == itemID && q.Presented {
			return true
		}
	}
	return false
}

func (s *Store) PendingQuestionClosures() []api.QuestionClosure {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []api.QuestionClosure
	for key, q := range s.state.AsyncClosed {
		if !closedAsync(q.State) || q.Notified {
			continue
		}
		out = append(out, api.QuestionClosure{ID: key, ItemID: q.ItemID, CallID: q.CallID, Owner: q.Owner, WorkID: q.WorkID, State: q.State, Reason: q.Reason, Index: q.Index})
	}
	slices.SortFunc(out, func(a, b api.QuestionClosure) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out
}
func (s *Store) QuestionClosureRecorded(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.state.AsyncClosed[id]
	if !ok || q.Notified {
		return nil
	}
	old := q
	q.Notified = true
	s.state.AsyncClosed[id] = q
	if err := s.save(); err != nil {
		s.state.AsyncClosed[id] = old
		return err
	}
	return nil
}

// Reject oversized authored summaries rather than truncating meaning or scope.
// CJK glyphs count as two units so both Latin and CJK copy fit narrow panels.
func validateQuestionPresentation(p api.QuestionPresentation, count int) error {
	units := func(text string) int {
		n := 0
		for _, r := range text {
			n++
			if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
				n++
			}
		}
		return n
	}
	if strings.TrimSpace(p.Title) == "" || units(p.Title) > 80 || units(p.Context) > 160 || strings.ContainsAny(p.Title+p.Context, "\r\n") {
		return fmt.Errorf("provide one complete short question (80 display units) and at most one context sentence (160); preserve all decision scope")
	}
	if len(p.Options) != count {
		return fmt.Errorf("display options must retain the original count and order")
	}
	seen := map[string]bool{}
	for _, option := range p.Options {
		key := strings.ToLower(strings.TrimSpace(option))
		if seen[key] {
			return fmt.Errorf("display option labels must be distinct")
		}
		seen[key] = true
		if strings.TrimSpace(option) == "" || units(option) > 48 || strings.ContainsAny(option, "\r\n") {
			return fmt.Errorf("each option needs a distinct short label (48 display units), preserving its original meaning")
		}
	}
	return nil
}
