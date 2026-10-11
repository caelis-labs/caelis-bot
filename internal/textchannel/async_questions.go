package textchannel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Codex async questions are completed agent messages, not pending native
// server requests. Their replies enter the same Bot conversation as a new user
// submission, with the exact question item identity supplied to the model.
type asyncQuestion struct {
	ItemID, TurnKey, CallID, Owner, Fingerprint, Title string
	WorkID                                             string
	Options                                            []string
	Index                                              int
	State, ReplyID                                     string // dispatching/accepted/unknown/expired; never replay an attempted send
	IssuedAt                                           int64  `json:"issuedAt,omitempty"`
}

// AsyncButtonQuestion is the provider-facing presentation of one exact async
// question. The short ID and fingerprint are stable for this item generation;
// transports still submit through Handle for owner and receipt checks.
type AsyncButtonQuestion struct {
	ShortID, Title, Fingerprint, State string
	Options                            []string
}

type WorkerAsyncQuestion struct {
	ShortID, ItemID, WorkID, Title, State string
	Options                               []string
}

func (s *Store) WorkerAsyncQuestions() []WorkerAsyncQuestion {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []WorkerAsyncQuestion{}
	for id, q := range s.state.AsyncQuestions {
		if q.WorkID == "" || s.state.AsyncActive[q.ItemID] != q.Fingerprint {
			continue
		}
		out = append(out, WorkerAsyncQuestion{ShortID: id, ItemID: q.ItemID, WorkID: q.WorkID, Title: q.Title, State: q.State, Options: append([]string(nil), q.Options...)})
	}
	slices.SortFunc(out, func(a, b WorkerAsyncQuestion) int { return strings.Compare(a.ShortID, b.ShortID) })
	return out
}

// Bot tools enter through the same durable controller without posing as an
// inbound owner message. The native Worker and original item still gate dispatch.
func (s *Store) HandleBotWorkAnswer(ctx context.Context, requestID, shortID, value string) api.Receipt {
	s.mu.Lock()
	q := s.state.AsyncQuestions[shortID]
	s.mu.Unlock()
	if q.WorkID == "" {
		return api.Receipt{ID: requestID, Outcome: "rejected", Message: "Worker question unavailable"}
	}
	in := Inbound{Channel: "bot", Conversation: "resident", ID: requestID, Text: "/answer " + shortID + " " + value, observed: true}
	feedback := s.Handle(ctx, in, api.Snapshot{})
	s.mu.Lock()
	state := s.state.AsyncQuestions[shortID].State
	s.mu.Unlock()
	outcome := "rejected"
	if state == "accepted" {
		outcome = "accepted"
	} else if state == "dispatching" || state == "unknown" {
		outcome = "unknown"
	}
	return api.Receipt{ID: requestID, Outcome: outcome, Message: feedback}
}

// HandleDesktopQuestion requires the generation carried by the clicked card.
// A recycled short number alone can never authorize an older desktop button.
func (s *Store) HandleDesktopQuestion(ctx context.Context, requestID, shortID, binding, value string, snapshot api.Snapshot) string {
	if binding == "" {
		return "这条问题卡片已失效，请使用最新卡片。"
	}
	return s.Handle(ctx, Inbound{Channel: "desktop", Conversation: "local", ID: requestID, Text: "/answer " + shortID + " " + value, expectedQuestionBinding: binding}, snapshot)
}

func (s *Store) AsyncButtonItemIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.state.AsyncActive))
	for id := range s.state.AsyncActive {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (s *Store) AsyncButtonQuestions(itemID string) []AsyncButtonQuestion {
	s.mu.Lock()
	defer s.mu.Unlock()
	fingerprint := s.state.AsyncActive[itemID]
	if fingerprint == "" {
		return nil
	}
	return s.asyncButtonQuestionsLocked(itemID, fingerprint)
}

func (s *Store) SetAsyncAnswerer(answer func(context.Context, Inbound, string) (api.Receipt, error)) {
	s.mu.Lock()
	s.answerAsync = answer
	s.mu.Unlock()
}

func (s *Store) SetAsyncWorkAnswerer(answer func(context.Context, Inbound, string, string, string, string) (api.Receipt, error)) {
	s.mu.Lock()
	s.answerAsyncWork = answer
	s.mu.Unlock()
}

func (s *Store) SetAsyncUpdateObserver(observer func(api.Item)) {
	s.mu.Lock()
	s.onAsyncUpdate = observer
	s.mu.Unlock()
}

func asyncFingerprint(item api.Item, owner, workID string) string {
	b, _ := json.Marshal(struct {
		ID, Turn, Call, Owner, WorkID string
		Questions                     []api.AsyncQuestion
	}{item.ID, item.TurnKey, item.AsyncCallID, owner, workID, item.AsyncQuestions})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Match Codex's bounded AnsweredQuestion title without splitting UTF-8.
func asyncReplyTitle(title string) string {
	if len(title) > 512 {
		cut := 0
		for _, r := range title {
			size := utf8.RuneLen(r)
			if cut+size > 512 {
				break
			}
			cut += size
		}
		title = title[:cut]
	}
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(title)
}

// AsyncCard replaces only the display text of a real async agent item. Short
// references and their ordered options are durable before the card is shown.
func (s *Store) AsyncCard(item api.Item, owner string) (api.Item, error) {
	return s.asyncCard(item, owner, "")
}

// AsyncWorkCard binds a native question to its original Worker. Only its
// completed question item is shown; Worker transcript text stays private.
func (s *Store) AsyncWorkCard(item api.Item, owner, workID string) (api.Item, error) {
	if workID == "" {
		return item, fmt.Errorf("async question work owner missing")
	}
	return s.asyncCard(item, owner, workID)
}

func (s *Store) asyncCard(item api.Item, owner, workID string) (api.Item, error) {
	if item.Kind != "assistant" || item.ID == "" || item.TurnKey == "" || item.AsyncCallID == "" || len(item.AsyncQuestions) == 0 {
		return item, nil
	}
	if owner == "" {
		return item, fmt.Errorf("async question runtime owner missing")
	}
	for _, q := range item.AsyncQuestions {
		if strings.TrimSpace(q.Title) == "" {
			return item, fmt.Errorf("async question title missing")
		}
	}
	fingerprint := asyncFingerprint(item, owner, workID)
	s.mu.Lock()
	created := []string{}
	var undoIDs []func()
	previousActive, hadActive := s.state.AsyncActive[item.ID]
	for index, q := range item.AsyncQuestions {
		found := false
		for _, old := range s.state.AsyncQuestions {
			if old.ItemID == item.ID && old.Index == index && old.Fingerprint == fingerprint {
				found = true
				break
			}
		}
		if found || hadActive && previousActive == fingerprint {
			continue
		}
		id, undo, err := s.nextShortIDLocked("Q")
		if err != nil {
			for i := len(undoIDs) - 1; i >= 0; i-- {
				delete(s.state.AsyncQuestions, created[i])
				undoIDs[i]()
			}
			s.mu.Unlock()
			return item, err
		}
		s.state.AsyncQuestions[id] = asyncQuestion{ItemID: item.ID, TurnKey: item.TurnKey, CallID: item.AsyncCallID, Owner: owner, WorkID: workID, Fingerprint: fingerprint, Title: q.Title, Options: append([]string(nil), q.Options...), Index: index, IssuedAt: time.Now().Unix()}
		created = append(created, id)
		undoIDs = append(undoIDs, undo)
	}
	s.state.AsyncActive[item.ID] = fingerprint
	if len(created) > 0 || !hadActive || previousActive != fingerprint {
		if err := s.save(); err != nil {
			for i := len(undoIDs) - 1; i >= 0; i-- {
				delete(s.state.AsyncQuestions, created[i])
				undoIDs[i]()
			}
			if hadActive {
				s.state.AsyncActive[item.ID] = previousActive
			} else {
				delete(s.state.AsyncActive, item.ID)
			}
			s.mu.Unlock()
			return item, err
		}
	}
	item.Text = s.asyncCardTextLocked(item.ID, fingerprint)
	item.QuestionCards = nil
	for _, q := range s.asyncButtonQuestionsLocked(item.ID, fingerprint) {
		item.QuestionCards = append(item.QuestionCards, api.QuestionCard{ShortID: q.ShortID, Binding: q.Fingerprint, Title: q.Title, Options: q.Options, State: q.State})
	}
	s.mu.Unlock()
	item.AsyncCallID, item.AsyncQuestions = "", nil
	return item, nil
}

func (s *Store) asyncButtonQuestionsLocked(itemID, fingerprint string) []AsyncButtonQuestion {
	var result []AsyncButtonQuestion
	for _, entry := range s.asyncQuestionsLocked(itemID, fingerprint) {
		q := entry.Question
		result = append(result, AsyncButtonQuestion{ShortID: entry.ID, Title: q.Title, Fingerprint: q.Fingerprint, State: q.State, Options: append([]string(nil), q.Options...)})
	}
	return result
}

type indexedAsyncQuestion struct {
	ID       string
	Question asyncQuestion
}

func (s *Store) asyncQuestionsLocked(itemID, fingerprint string) []indexedAsyncQuestion {
	var out []indexedAsyncQuestion
	for id, q := range s.state.AsyncQuestions {
		if q.ItemID == itemID && q.Fingerprint == fingerprint {
			out = append(out, indexedAsyncQuestion{ID: id, Question: q})
		}
	}
	slices.SortFunc(out, func(a, b indexedAsyncQuestion) int { return a.Question.Index - b.Question.Index })
	return out
}

func (s *Store) asyncCardTextLocked(itemID, fingerprint string) string {
	var lines []string
	for _, entry := range s.asyncQuestionsLocked(itemID, fingerprint) {
		id, q := entry.ID, entry.Question
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "["+id+"] "+q.Title)
		for number, option := range q.Options {
			lines = append(lines, fmt.Sprintf("[%d] %s", number+1, option))
		}
		switch q.State {
		case "accepted":
			lines = append(lines, "回答已提交。")
		case "expired":
			lines = append(lines, "问题编号已过期。")
		case "dispatching", "unknown":
			lines = append(lines, "回答投递结果待核对；不会自动重发。")
		default:
			if len(q.Options) > 0 {
				lines = append(lines, "", "提示：发送 /answer "+id+" + 选项标号")
			} else {
				lines = append(lines, "", "提示：发送 /answer "+id+" + 完整回答")
			}
		}
	}
	if len(lines) == 0 {
		return "问题已处理或过期。"
	}
	return strings.Join(lines, "\n")
}

func (s *Store) handleAsync(ctx context.Context, in Inbound, snapshot api.Snapshot, id, key string) string {
	s.mu.Lock()
	q := s.state.AsyncQuestions[id]
	if in.expectedQuestionBinding != "" && in.expectedQuestionBinding != q.Fingerprint {
		s.mu.Unlock()
		return s.reply(key, "这条问题卡片已失效，请使用最新卡片。")
	}
	if q.State == "expired" || q.State == "" && shortIDExpired(q.IssuedAt) {
		s.mu.Unlock()
		return s.reply(key, "问题编号已过期，请查看最新问题。")
	}
	active := s.state.AsyncActive[q.ItemID]
	answer := s.answerAsync
	answerWork := s.answerAsyncWork
	if active != q.Fingerprint || q.WorkID == "" && (snapshot.RuntimeOwner != q.Owner || snapshot.Connection != "ready" && snapshot.Connection != "connected") {
		s.mu.Unlock()
		return s.reply(key, "问题已失效或 Runtime 尚未就绪，请查看最新消息。")
	}
	if q.State != "" {
		feedback := "回答已提交，不会重复发送。"
		if q.State != "accepted" {
			feedback = "回答投递结果待核对，不会重复发送。"
		}
		s.mu.Unlock()
		return s.reply(key, feedback)
	}
	if q.WorkID == "" && answer == nil || q.WorkID != "" && answerWork == nil {
		s.mu.Unlock()
		return s.reply(key, "当前入口暂不能提交这条回答。")
	}
	s.mu.Unlock()

	parts := strings.Fields(strings.TrimSpace(in.Text))
	tail := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.Text), parts[0]))
	tail = strings.TrimSpace(strings.TrimPrefix(tail, id))
	if tail == "" {
		return s.reply(key, "请在问题编号后填写选项序号或完整回答。")
	}
	chosen := tail
	if allDigits(tail) && len(q.Options) > 0 {
		index, err := strconv.Atoi(tail)
		if err != nil || index < 1 || index > len(q.Options) {
			return s.reply(key, "选项序号无效，请使用卡片列出的序号。")
		}
		chosen = q.Options[index-1]
	}
	if len(chosen) > 16<<10 {
		return s.reply(key, "回答过长，请缩短后重试。")
	}
	questionItemID, _ := json.Marshal([]any{"request_user_input_async", q.CallID, q.Index})
	payload, _ := json.Marshal([]struct {
		Answer         string `json:"answer"`
		Question       string `json:"question"`
		QuestionItemID string `json:"questionItemId"`
	}{{chosen, asyncReplyTitle(q.Title), string(questionItemID)}})
	modelInput := "<send_user_message_question_reply>\n" + string(payload) + "\n</send_user_message_question_reply>"

	s.mu.Lock()
	current := s.state.AsyncQuestions[id]
	if current.State != "" || s.state.AsyncActive[q.ItemID] != q.Fingerprint {
		s.mu.Unlock()
		return s.reply(key, "问题已处理或已变化，请查看最新消息。")
	}
	current.State, current.ReplyID = "dispatching", in.ID
	s.state.AsyncQuestions[id] = current
	if err := s.save(); err != nil {
		current.State, current.ReplyID = "", ""
		s.state.AsyncQuestions[id] = current
		s.mu.Unlock()
		return s.reply(key, "本地记录不可用，回答未提交；请重试。")
	}
	s.mu.Unlock()
	var receipt api.Receipt
	var err error
	if q.WorkID != "" {
		receipt, err = answerWork(ctx, in, q.WorkID, q.Owner, q.ItemID, modelInput)
	} else {
		receipt, err = answer(ctx, in, modelInput)
	}

	s.mu.Lock()
	current = s.state.AsyncQuestions[id]
	feedback := "回答投递结果待核对，不会自动重发。"
	if err == nil && receipt.Outcome == "accepted" {
		current.State = "accepted"
		feedback = "回答已提交，等待后续回复。"
	} else if receipt.Outcome == "rejected" || errors.Is(err, api.ErrRecoveryPending) {
		current.State, current.ReplyID = "", ""
		feedback = "回答未提交。" + receipt.Message + " 可用同一问题编号重新回答。"
	} else {
		current.State = "unknown"
	}
	s.state.AsyncQuestions[id] = current
	if saveErr := s.save(); saveErr != nil {
		feedback = "回答投递后本地状态保存失败；请先核对 Bot 回复，不会自动重发。"
	}
	updated := api.Item{ID: q.ItemID, TurnKey: q.TurnKey, Kind: "assistant", Status: "completed", Text: s.asyncCardTextLocked(q.ItemID, q.Fingerprint)}
	for _, question := range s.asyncButtonQuestionsLocked(q.ItemID, q.Fingerprint) {
		updated.QuestionCards = append(updated.QuestionCards, api.QuestionCard{ShortID: question.ShortID, Binding: question.Fingerprint, Title: question.Title, Options: question.Options, State: question.State})
	}
	observer := s.onAsyncUpdate
	s.mu.Unlock()
	if observer != nil {
		observer(updated)
	}
	return s.reply(key, feedback)
}
