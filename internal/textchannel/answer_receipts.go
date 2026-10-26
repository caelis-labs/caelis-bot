package textchannel

import (
	"strings"
	"time"
)

type AsyncAnswerReceipt struct{ WorkID, OperationID string }

func (s *Store) AsyncAnswerReceipts() []AsyncAnswerReceipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []AsyncAnswerReceipt{}
	for _, q := range s.state.AsyncReceipts {
		if q.State == "unknown" && q.WorkID != "" && q.OperationID != "" {
			out = append(out, AsyncAnswerReceipt{q.WorkID, q.OperationID})
		}
	}
	return out
}

// Exact original-owner proof settles delivery, never re-sends an answer. A
// definitive rejection restores pending only while the original source is valid;
// its exact text must be re-observed before a subsequent answer can be sent.
func (s *Store) SettleAsyncOperation(work, operation, outcome string) error {
	if outcome != "accepted" && outcome != "rejected" {
		return nil
	}
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
	changed := false
	for id, q := range s.state.AsyncQuestions {
		if q.WorkID != work || q.OperationID != operation || q.State != "unknown" {
			continue
		}
		replies = append(replies, struct{ request, binding, outcome string }{q.ReplyID, q.Fingerprint, outcome})
		if outcome == "accepted" {
			q.State = "answered"
			q.ClosedAt = time.Now().Unix()
		} else {
			q = s.rejectedAsyncLocked(q)
		}
		s.state.AsyncQuestions[id] = q
		if q.State != "" {
			s.state.AsyncClosed[asyncKey(q)] = q
		}
		changed = true
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

func (s *Store) migrateAnswerReceipt(q asyncQuestion) asyncQuestion {
	if q.OperationID != "" || q.WorkID == "" || q.ReplyID == "" {
		return q
	}
	found := ""
	for key := range s.state.Replies {
		parts := strings.SplitN(key, "\x00", 3)
		if len(parts) != 3 || parts[2] != q.ReplyID {
			continue
		}
		candidate := WorkerAnswerRequestID(Inbound{Channel: parts[0], Conversation: parts[1], ID: parts[2]}, q.WorkID, q.ItemID)
		if found != "" && found != candidate {
			return q
		}
		found = candidate
	}
	q.OperationID = found
	return q
}

// Rejection proves no answer effect, but cannot undo a prior local task/owner
// closure. All three receipt paths use the same presentation boundary.
func (s *Store) rejectedAsyncLocked(q asyncQuestion) asyncQuestion {
	q.State, q.ReplyID, q.OperationID = "", "", ""
	switch {
	case q.ClosedAt > 0 && q.Reason != "":
		q.State = "expired"
		if q.Reason == "owner_replaced" || q.Reason == "question_replaced" {
			q.State = "superseded"
		}
	case s.state.AsyncRetired[q.Fingerprint] || s.state.AsyncActive[q.ItemID] != q.Fingerprint:
		q.State, q.Reason, q.ClosedAt = "superseded", "question_replaced", time.Now().Unix()
	case asyncExpired(q):
		q.State, q.Reason, q.ClosedAt = "expired", "deadline", time.Now().Unix()
	}
	return q
}
