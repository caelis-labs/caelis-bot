package textchannel

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

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
	if q.Presentation != nil && validateQuestionPresentation(*q.Presentation, q.Title, q.Options) == nil {
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
	var replies []struct{ request, binding, outcome string }
	observer := s.onQuestionReplyOutcome
	committed := false
	defer func() {
		s.mu.Unlock()
		if committed && observer != nil {
			for _, reply := range replies {
				observer(reply.request, reply.binding, reply.outcome)
			}
		}
	}()
	previous := s.state
	s.state = cloneMaintenanceState(s.state)
	changed := s.compactClosedQuestionCopies()
	for id, q := range s.state.AsyncQuestions {
		if q.State == "unknown" && q.WorkID == "" && snapshot.RuntimeOwner == q.Owner && snapshot.LastReceipt.ID == q.ReplyID {
			if snapshot.LastReceipt.Outcome == "accepted" || snapshot.LastReceipt.Outcome == "rejected" {
				replies = append(replies, struct{ request, binding, outcome string }{q.ReplyID, q.Fingerprint, snapshot.LastReceipt.Outcome})
			}
			if snapshot.LastReceipt.Outcome == "accepted" {
				q.State = "answered"
				q.ClosedAt = time.Now().Unix()
				s.state.AsyncClosed[asyncKey(q)] = q
				changed = true
			} else if snapshot.LastReceipt.Outcome == "rejected" {
				q = s.rejectedAsyncLocked(q)
				if q.State != "" {
					s.state.AsyncClosed[asyncKey(q)] = q
				}
				changed = true
			}
			s.state.AsyncQuestions[id] = q
		}
		if q.State != "" && q.State != "unknown" && q.State != "dispatching" {
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
			if q.State == "" && q.TaskTitle != w.Task.Title {
				q.TaskTitle = w.Task.Title
				s.state.AsyncQuestions[id] = q
				changed = true
			}
			switch {
			case w.StopRequested || w.Task.Status == "cancelled" || w.Task.Status == "canceled":
				state, reason = "expired", "task_cancelled"
			case w.Task.Status == "failed":
				state, reason = "expired", "task_ended"
			case w.TaskEnded:
				state, reason = "expired", "task_completed"
			}
		}
		if state != "" {
			if q.State == "unknown" || q.State == "dispatching" {
				if q.ClosedAt == 0 {
					q.Reason, q.ClosedAt = reason, time.Now().Unix()
					s.state.AsyncQuestions[id] = q
					changed = true
				}
			} else {
				s.closeAsyncLocked(id, q, state, reason)
				changed = true
			}
		}
	}
	if changed {
		if err := s.save(); err != nil {
			s.state = previous
			return err
		}
	}
	committed = true
	return nil
}

// ProjectQuestions always resolves cached IM cards against the durable ledger.
// An old cache or reused Q number cannot revive a pending control.
func (s *Store) ProjectQuestions(items []api.Item) ([]api.Item, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := append([]api.Item(nil), items...)
	for i, item := range result {
		if reply := item.QuestionReply; reply != nil {
			q, ok := s.state.AsyncQuestions[reply.ShortID]
			if ok && q.Fingerprint == reply.Binding && q.ReplyID == item.RequestID {
				copy := *reply
				switch q.State {
				case "answered":
					copy.Outcome = "accepted"
				case "dispatching":
					copy.Outcome = "sending"
				case "unknown":
					copy.Outcome = "unknown"
				}
				item.QuestionReply, item.Status = &copy, copy.Outcome
				result[i] = item
			}
		}
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
				card.State, card.Reason, card.ExpiresAt = q.State, q.Reason, q.ExpiresAt
				if q.TaskTitle != "" {
					card.TaskTitle = q.TaskTitle
				}
				// The IM card already holds its original display. A compact
				// terminal tombstone updates lifecycle only, never erases it.
				if q.Title != "" {
					card.Title, card.Context, card.Options = q.Title, "", append([]string(nil), q.Options...)
				}
				if q.Presentation != nil && validateQuestionPresentation(*q.Presentation, q.Title, q.Options) == nil {
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
		if err := validateQuestionPresentation(*presentation, q.Title, q.Options); err != nil {
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

// PendingQuestionClosures is a retired compatibility read. Closing a temporary
// interaction never sends model input or queues a notification to a Worker.
func (s *Store) PendingQuestionClosures() []api.QuestionClosure { return nil }

// A presentation may split the original title into a heading and following
// context, but it cannot rewrite, remove or reorder a single semantic byte.
// Options are the native answers themselves, never model-authored aliases.
func validateQuestionPresentation(p api.QuestionPresentation, title string, options []string) error {
	if p.Title == "" || !strings.HasPrefix(title, p.Title) || strings.TrimSpace(strings.TrimPrefix(title, p.Title)) != p.Context {
		return fmt.Errorf("presentation must preserve the exact original question: title followed by context; request a new concise question at source instead of paraphrasing")
	}
	if !slices.Equal(p.Options, options) {
		return fmt.Errorf("presentation options must exactly equal the original answers in order; aliases cannot represent user consent")
	}
	return nil
}

// Terminal display data belongs to the IM transcript, not the control ledger.
// Uncertain dispatches retain only exact original bindings/receipts, never prose.
const asyncHistoryLimit = 4096

func compactQuestion(q asyncQuestion) asyncQuestion {
	q.Title, q.Options, q.Presentation, q.TaskTitle = "", nil, nil, ""
	return q
}
func (s *Store) compactClosedQuestionCopies() bool {
	changed := false
	if s.state.AsyncReceipts == nil {
		s.state.AsyncReceipts = map[string]asyncQuestion{}
	}
	for id, q := range s.state.AsyncQuestions {
		if q.State != "unknown" && q.State != "dispatching" {
			if _, ok := s.state.AsyncReceipts[asyncKey(q)]; ok {
				delete(s.state.AsyncReceipts, asyncKey(q))
				changed = true
			}
		}
		if q.State == "" {
			continue
		}
		compact := compactQuestion(q)
		if q.State == "unknown" || q.State == "dispatching" {
			s.state.AsyncReceipts[asyncKey(q)] = compact
		}
		if q.Title != "" || len(q.Options) != 0 || q.Presentation != nil || q.TaskTitle != "" {
			s.state.AsyncQuestions[id] = compact
			changed = true
		}
	}
	for key, q := range s.state.AsyncClosed {
		if q.State == "unknown" || q.State == "dispatching" {
			s.state.AsyncReceipts[key] = compactQuestion(q)
			delete(s.state.AsyncClosed, key)
			changed = true
			continue
		}
		if asyncExpired(q) {
			delete(s.state.AsyncClosed, key)
			changed = true
			continue
		}
		if q.Title != "" || len(q.Options) != 0 || q.Presentation != nil || q.TaskTitle != "" {
			s.state.AsyncClosed[key] = compactQuestion(q)
			changed = true
		}
	}
	referenced := map[string]string{}
	for _, records := range []map[string]asyncQuestion{s.state.AsyncQuestions, s.state.AsyncClosed} {
		for _, q := range records {
			referenced[q.ItemID] = q.Fingerprint
		}
	}
	for id := range s.state.AsyncActive {
		if _, ok := referenced[id]; !ok {
			delete(s.state.AsyncActive, id)
			changed = true
		}
	}
	needed := map[string]bool{}
	for _, records := range []map[string]asyncQuestion{s.state.AsyncQuestions, s.state.AsyncClosed} {
		for _, q := range records {
			if current := s.state.AsyncActive[q.ItemID]; current != "" && current != q.Fingerprint {
				needed[q.Fingerprint] = true
			}
		}
	}
	for fp := range s.state.AsyncRetired {
		if !needed[fp] {
			delete(s.state.AsyncRetired, fp)
			changed = true
		}
	}
	return changed
}
