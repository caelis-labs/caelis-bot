// Package textchannel owns the text-only contract shared by companion channels.
// A transport authenticates its owner and persists its ingress before calling it.
package textchannel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Inbound and Outbound describe the minimum text capability. Native buttons are
// optional transport presentation and must submit the same api.Decision.
type Reference struct{ Conversation, MessageID string }
type Inbound struct {
	Channel, Conversation, ID, Text string
	Reference                       *Reference // transport supplied; quoted text is never authority
	observed                        bool
}
type Outbound struct{ Channel, Conversation, ID, Text string }
type Notice struct {
	ID, Text string
	Origin   Origin
	SeenAt   int64
}

type Origin struct{ Channel, Conversation string }
type option struct{ ID, Label, Scope, Details string }
type prompt struct {
	NativeID, TurnKey, Owner, QuestionID, Fingerprint, Schema, Type, SubmitChoice string
	Options                                                                       []option
	Free, Required, Multiple, Secret                                              bool
}
type claim struct{ State, Feedback string }
type cardBinding struct {
	NativeID, Schema, ShortID string
	Ambiguous, Secret         bool
}
type document struct {
	Version                    int
	NextApproval, NextQuestion uint64
	Origins                    map[string]Origin              // exact accepted submission ID, saved before dispatch
	Prompts                    map[string]prompt              // immutable short numbers; never recycled
	Claims                     map[string]claim               // native request ID, shared across transports
	Answers                    map[string]map[string][]string // non-secret answers waiting on one native request
	Replies                    map[string]string              // original channel message ID -> immutable feedback
	ReplyTimes                 map[string]int64               // Unix microseconds of the original feedback
	Cards                      map[string]cardBinding         // confirmed transport message ID -> native prompt
	ReplyOrder                 []string
	AsyncQuestions             map[string]asyncQuestion `json:"asyncQuestions,omitempty"`
	AsyncActive                map[string]string        `json:"asyncActive,omitempty"` // item ID -> exact question fingerprint
}
type Store struct {
	mu            sync.Mutex
	path          string
	state         document
	secretAnswers map[string]map[string][]string // intentionally memory-only; lost on restart
	decide        func(context.Context, api.Decision) error
	onNotice      func(Notice)
	onInbound     func(Inbound)
	onAsyncUpdate func(api.Item)
	answerAsync   func(context.Context, Inbound, string) (api.Receipt, error)
}

func Open(root string, decide func(context.Context, api.Decision) error) (*Store, error) {
	s := &Store{path: filepath.Join(root, "text-channel-control.json"), decide: decide, secretAnswers: map[string]map[string][]string{}}
	s.state = document{Version: 1, Origins: map[string]Origin{}, Prompts: map[string]prompt{}, Claims: map[string]claim{}, Answers: map[string]map[string][]string{}, Replies: map[string]string{}, ReplyTimes: map[string]int64{}, Cards: map[string]cardBinding{}, AsyncQuestions: map[string]asyncQuestion{}, AsyncActive: map[string]string{}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &s.state) != nil || s.state.Version != 1 || s.state.Origins == nil || s.state.Prompts == nil || s.state.Claims == nil || s.state.Replies == nil {
		return nil, errors.New("text channel control state unavailable")
	}
	if s.state.Answers == nil {
		s.state.Answers = map[string]map[string][]string{}
	}
	if s.state.Cards == nil {
		s.state.Cards = map[string]cardBinding{}
	}
	if s.state.ReplyTimes == nil {
		s.state.ReplyTimes = map[string]int64{}
	}
	if s.state.AsyncQuestions == nil {
		s.state.AsyncQuestions = map[string]asyncQuestion{}
	}
	if s.state.AsyncActive == nil {
		s.state.AsyncActive = map[string]string{}
	}
	if s.state.ReplyOrder == nil && len(s.state.Replies) > 0 {
		for key := range s.state.Replies {
			s.state.ReplyOrder = append(s.state.ReplyOrder, key)
		}
		slices.Sort(s.state.ReplyOrder)
	}
	return s, nil
}
func (s *Store) save() error { return localstate.Write(s.path, s.state) }

// RecordOrigin runs before native Submit. It prevents echoing the original
// user item to its own transport and retains the Weixin context token.
func (s *Store) RecordOrigin(id string, origin Origin) error {
	if id == "" || origin.Channel == "" || origin.Conversation == "" {
		return errors.New("invalid message origin")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.state.Origins[id]; ok {
		if previous != origin {
			return errors.New("message origin changed")
		}
		return nil
	}
	s.state.Origins[id] = origin
	if err := s.save(); err != nil {
		delete(s.state.Origins, id)
		return err
	}
	return nil
}
func (s *Store) OriginOf(id string) (Origin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.state.Origins[id]
	return value, ok
}

// Route chooses a Weixin context token for delivery. Ambiguous steering falls
// back to that transport's current token; it never filters conversation output.
func (s *Store) Route(snapshot api.Snapshot, requestID, turnKey string) (Origin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if origin, ok := s.state.Origins[requestID]; ok {
		return origin, true
	}
	var result Origin
	found := false
	for _, item := range snapshot.Items {
		if (item.Kind != "user" && item.Kind != "hostNotice") || item.TurnKey == "" || item.TurnKey != turnKey {
			continue
		}
		origin := s.state.Origins[item.RequestID] // empty means desktop
		if found && result != origin {
			return Origin{}, false
		}
		result, found = origin, true
	}
	return result, found
}

// Card freezes every field and offered decision against one native request.
// The final command is an exact copyable example; option labels never share it.
func (s *Store) Card(a api.Approval) (string, string, error) {
	if a.Status != "pending" || a.ID == "" {
		return "", "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	secret := false
	for _, q := range a.Questions {
		secret = secret || q.Secret
	}
	var lines []string
	for _, value := range []string{a.Title, a.TaskTitle, a.Action} {
		if value != "" {
			lines = append(lines, value)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "需要处理的请求")
	}
	if a.Target != "" {
		lines = append(lines, "目标："+a.Target)
	}
	// Unstructured details cannot be reliably scrubbed. Suppress them for a
	// request with a Runtime-marked secret field, keeping operation and target.
	if !secret {
		for _, section := range a.Sections {
			if section.Text != "" {
				lines = append(lines, section.Text)
			}
		}
		for _, value := range []string{a.Description, a.Details} {
			if value != "" {
				lines = append(lines, value)
			}
		}
	}
	if a.URL != "" {
		if u, ok := webURL(a.URL); ok {
			lines = append(lines, "授权链接："+a.URL, "请在浏览器完成服务要求的步骤，再选择 Runtime 提供的确认选项；打开链接本身不代表成功。")
			if localURL(u) {
				lines = append(lines, "此链接指向本机或私网地址，手机可能无法直连；请核对服务是否只允许本机回调。")
			}
		} else {
			lines = append(lines, "Runtime 提供的链接不是可展示的 HTTP(S) 地址；请核对原请求。")
		}
	}
	schema := catalogFingerprint(a)
	add := func(kind string, p prompt) (string, error) {
		encoded, _ := json.Marshal(p)
		h := sha256.Sum256(encoded)
		p.Fingerprint = hex.EncodeToString(h[:])
		for id, old := range s.state.Prompts {
			if old.NativeID == p.NativeID && old.QuestionID == p.QuestionID && old.Fingerprint == p.Fingerprint {
				return id, nil
			}
		}
		var id string
		if kind == "A" {
			s.state.NextApproval++
			id = fmt.Sprintf("A%d", s.state.NextApproval)
		} else {
			s.state.NextQuestion++
			id = fmt.Sprintf("Q%d", s.state.NextQuestion)
		}
		s.state.Prompts[id] = p
		if err := s.save(); err != nil {
			delete(s.state.Prompts, id)
			return "", err
		}
		return id, nil
	}
	var first string
	if len(a.Questions) > 0 {
		if hasRequired(a.Questions) && len(a.Questions) > 1 {
			lines = append(lines, "如需填写可选字段，请先填写；所有必填字段填齐后会一次提交原请求。")
		}
		seenQuestions := map[string]bool{}
		for _, q := range a.Questions {
			if q.ID == "" || seenQuestions[q.ID] {
				return strings.Join(append(lines, "Runtime 问题标识缺失或重复，无法安全提交；请核对原请求。"), "\n"), "", nil
			}
			if !uniqueOptions(questionOptions(q)) {
				return strings.Join(append(lines, "Runtime 问题选项标识缺失或重复，无法安全提交；请核对原请求。"), "\n"), "", nil
			}
			seenQuestions[q.ID] = true
		}
		for _, q := range a.Questions {
			p := prompt{NativeID: a.ID, TurnKey: a.TurnKey, Owner: a.Owner, QuestionID: q.ID, Schema: schema, Type: q.Type, SubmitChoice: submitChoice(a), Free: freeQuestion(q), Required: q.Required, Multiple: q.Multiple, Secret: q.Secret, Options: questionOptions(q)}
			id, err := add("Q", p)
			if err != nil {
				return "", "", err
			}
			if first == "" {
				first = id
			}
			title := q.Title
			if title == "" {
				title = q.ID
			}
			need := "可选"
			if q.Required {
				need = "必填"
			}
			lines = append(lines, "", fmt.Sprintf("[%s] %s（%s）", id, title, need))
			if _, filled := s.answerLocked(answerKey(a.ID, schema), q.ID); filled {
				lines = append(lines, "状态：已填写；再次回答可在提交前修改。")
			} else {
				lines = append(lines, "状态：待填写。")
			}
			if q.Secret {
				lines = append(lines, "敏感字段：其他入口只显示填写状态；答案只用于原生请求，不进入 Bot 普通对话。")
			}
			if s.state.Claims[a.ID].State == "" {
				for number, o := range p.Options {
					lines = append(lines, fmt.Sprintf("[%d] %s", number+1, meaning(o)))
				}
				if q.Multiple && len(p.Options) > 0 {
					example := "1"
					if len(p.Options) > 1 {
						example = "1,2"
					}
					lines = append(lines, "多选用逗号分隔索引，例如：", "/answer "+id+" "+example)
				} else if p.Free {
					lines = append(lines, "回答请输入 /answer "+id+" 后接完整回答。")
				} else if len(p.Options) == 0 {
					lines = append(lines, "Runtime 没有为此字段提供可用选项；请核对原请求。")
				}
				if len(p.Options) > 0 && !q.Multiple {
					lines = append(lines, "", "回答请输入", fmt.Sprintf("/answer %s 1", id))
				}
			}
		}
	}
	p := prompt{NativeID: a.ID, TurnKey: a.TurnKey, Owner: a.Owner, Schema: schema}
	for _, o := range a.Choices {
		if len(a.Questions) > 0 && (o.ID == "answer" || o.ID == "accept") && hasRequired(a.Questions) {
			continue // a required form submits once its fields are complete
		}
		p.Options = append(p.Options, option{o.ID, label(o), o.Scope, o.Details})
	}
	if !uniqueOptions(p.Options) {
		return strings.Join(append(lines, "Runtime 决策选项标识缺失或重复，无法安全提交；请核对原请求。"), "\n"), "", nil
	}
	if len(p.Options) > 0 {
		id, err := add("A", p)
		if err != nil {
			return "", "", err
		}
		if first == "" {
			first = id
		}
		if len(a.Questions) > 0 {
			lines = append(lines, "", "处理整个请求：")
		}
		if s.state.Claims[a.ID].State == "" {
			for number, o := range p.Options {
				lines = append(lines, fmt.Sprintf("[%d] %s", number+1, meaning(o)))
			}
			lines = append(lines, "", "执行审批请输入", fmt.Sprintf("/approve %s 1", id))
		}
	} else if len(a.Questions) == 0 {
		lines = append(lines, "Runtime 没有提供可执行选项；请核对原请求。")
	}
	if decision := s.state.Claims[a.ID]; decision.State != "" {
		lines = append(lines, decision.Feedback)
	}
	if len(lines) == 0 {
		lines = append(lines, "需要处理的请求")
	}
	if first != "" {
		lines[0] = "[" + first + "] " + lines[0]
	}
	return strings.Join(lines, "\n"), first, nil
}

func catalogFingerprint(a api.Approval) string {
	a.Status, a.Resolution, a.TitleKey, a.NoticeKey = "", nil, "", ""
	b, _ := json.Marshal(a)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func questionOptions(q api.Question) []option {
	var result []option
	for _, o := range q.Options {
		result = append(result, option{o.ID, label(o), o.Scope, o.Details})
	}
	if q.Type == "boolean" && len(result) == 0 {
		result = []option{{ID: "true", Label: "是"}, {ID: "false", Label: "否"}}
	}
	return result
}
func uniqueOptions(options []option) bool {
	seen := map[string]bool{}
	for _, o := range options {
		if o.ID == "" || seen[o.ID] {
			return false
		}
		seen[o.ID] = true
	}
	return true
}
func freeQuestion(q api.Question) bool {
	return !q.Multiple && (q.Type == "text" || q.Type == "string" || q.Type == "number" || q.Type == "integer")
}
func hasRequired(questions []api.Question) bool {
	for _, q := range questions {
		if q.Required {
			return true
		}
	}
	return false
}
func submitChoice(a api.Approval) string {
	for _, preferred := range []string{"answer", "accept"} {
		for _, c := range a.Choices {
			if c.ID == preferred {
				return preferred
			}
		}
	}
	if len(a.Choices) == 0 { // old fixture projections; Runtime still validates
		return "answer"
	}
	return ""
}
func webURL(value string) (*url.URL, bool) {
	u, err := url.Parse(value)
	return u, err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}
func localURL(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}
func answerKey(native, schema string) string { return native + "\x00" + schema }
func (s *Store) answerLocked(key, question string) ([]string, bool) {
	if v, ok := s.secretAnswers[key][question]; ok {
		return slices.Clone(v), true
	}
	v, ok := s.state.Answers[key][question]
	return slices.Clone(v), ok
}
func (s *Store) Claimed(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	decision, ok := s.state.Claims[id]
	return decision.Feedback, ok && decision.State != ""
}
func label(c api.Choice) string {
	if c.Label != "" {
		return c.Label
	}
	if c.LabelKey != "" {
		if value, ok := i18n.Lookup(i18n.Chinese, c.LabelKey, nil); ok {
			return value
		}
	}
	return "Runtime 选项 " + c.ID
}
func meaning(o option) string {
	value := o.Label
	if o.Scope != "" {
		scope := map[string]string{"once": "仅本次", "turn": "本轮", "session": "本会话", "conversation": "本次对话", "always": "始终", "rule": "规则"}[o.Scope]
		if scope == "" {
			scope = o.Scope
		}
		value += "（范围：" + scope + "）"
	}
	if o.Details != "" {
		value += "；" + o.Details
	}
	return value
}

func IsCommand(text string) bool { return strings.HasPrefix(strings.TrimSpace(text), "/") }

func cardKey(channel, conversation, messageID string) string {
	return channel + "\x00" + conversation + "\x00" + messageID
}

// BindCard is called only after a transport confirms the real sent message ID.
// A card with several actionable fields cannot authorize a bare reply.
func (s *Store) BindCard(channel, conversation, messageID string, a api.Approval) error {
	if channel == "" || conversation == "" || messageID == "" || a.ID == "" || a.Status != "pending" {
		return errors.New("invalid card delivery")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding := cardBinding{NativeID: a.ID, Schema: catalogFingerprint(a)}
	for _, q := range a.Questions {
		binding.Secret = binding.Secret || q.Secret
	}
	for id, p := range s.state.Prompts {
		if p.NativeID != a.ID || p.Schema != binding.Schema {
			continue
		}
		if binding.ShortID != "" {
			binding.Ambiguous = true
		}
		binding.ShortID = id
	}
	if binding.ShortID == "" {
		return errors.New("card has no text prompt")
	}
	key := cardKey(channel, conversation, messageID)
	old, existed := s.state.Cards[key]
	if existed && old == binding {
		return nil
	}
	s.state.Cards[key] = binding
	if err := s.save(); err != nil {
		if existed {
			s.state.Cards[key] = old
		} else {
			delete(s.state.Cards, key)
		}
		return err
	}
	return nil
}

func (s *Store) referencedPrompt(in Inbound) (cardBinding, prompt, bool) {
	if in.Reference == nil || in.Reference.MessageID == "" || in.Reference.Conversation == "" {
		return cardBinding{}, prompt{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Cards[cardKey(in.Channel, in.Reference.Conversation, in.Reference.MessageID)]
	if !ok {
		return cardBinding{}, prompt{}, false
	}
	p := s.state.Prompts[b.ShortID]
	return b, p, true
}

func (s *Store) HasCardReference(in Inbound) bool {
	_, _, ok := s.referencedPrompt(in)
	return ok
}

// SecretPrompt identifies only Runtime-marked fields, including a real reply
// to a confirmed card. Transports use it before persisting inbound text.
func (s *Store) SecretPrompt(in Inbound) string {
	parts := strings.Fields(in.Text)
	if len(parts) >= 3 && parts[0] == "/answer" {
		s.mu.Lock()
		p := s.state.Prompts[parts[1]]
		s.mu.Unlock()
		if p.Secret && p.QuestionID != "" {
			return parts[1]
		}
	}
	if IsCommand(in.Text) {
		return ""
	}
	b, p, ok := s.referencedPrompt(in)
	if ok && b.Secret && (b.Ambiguous || p.Secret && p.QuestionID != "") {
		return b.ShortID
	}
	return ""
}

// HandleReference handles only replies to a confirmed Bot card. Unknown
// references are ordinary chat input; ambiguous cards return text guidance.
func (s *Store) HandleReference(ctx context.Context, in Inbound, snapshot api.Snapshot) (string, bool) {
	if IsCommand(in.Text) {
		return "", false
	}
	b, p, ok := s.referencedPrompt(in)
	if !ok {
		return "", false
	}
	s.observeInbound(in)
	in.observed = true
	key := cardKey(in.Channel, in.Conversation, in.ID)
	var current *api.Approval
	for i := range snapshot.Approvals {
		if snapshot.Approvals[i].ID == b.NativeID {
			current = &snapshot.Approvals[i]
			break
		}
	}
	if current != nil && current.Status == "resolved" {
		return s.reply(key, "原请求已处理。"+resolutionText(current.Resolution)), true
	}
	s.mu.Lock()
	claimed := s.state.Claims[b.NativeID]
	s.mu.Unlock()
	if claimed.State != "" {
		return s.reply(key, claimed.Feedback), true
	}
	if current == nil || current.Status != "pending" {
		return s.reply(key, "原请求已失效或已处理，请查看最新请求。"), true
	}
	if catalogFingerprint(*current) != b.Schema {
		return s.reply(key, "引用的卡片已变化，请使用最新卡片中的完整命令。"), true
	}
	if b.Ambiguous {
		return s.reply(key, "此卡片有多个可操作字段或决定，直接回复会有歧义；请发送对应的完整 /answer 或 /approve 命令。"), true
	}
	if p.NativeID != b.NativeID || p.Schema != b.Schema {
		return s.reply(key, "引用的卡片已变化，请使用最新卡片中的完整命令。"), true
	}
	value := strings.TrimSpace(in.Text)
	if value == "" {
		return s.reply(key, "回答不能为空。"), true
	}
	command := "/approve"
	if p.QuestionID != "" {
		command = "/answer"
	}
	in.Text = command + " " + b.ShortID + " " + value
	return s.Handle(ctx, in, snapshot), true
}

// IsSecretCommand lets a transport keep a Runtime-marked secret answer out of
// its ordinary persisted ingress ledger. It does not inspect arbitrary content.
func (s *Store) IsSecretCommand(value string) bool {
	return s.SecretPrompt(Inbound{Text: value}) != ""
}

// RecoverSecretInput is used only when a transport lost an in-memory secret
// between durable ingress and its ordinary receipt. It observes the original
// claim; it never reconstructs or redispatches a secret value.
func (s *Store) RecoverSecretInput(in Inbound, shortID string) string {
	key := in.Channel + "\x00" + in.Conversation + "\x00" + in.ID
	s.mu.Lock()
	if saved, ok := s.state.Replies[key]; ok {
		s.mu.Unlock()
		return saved
	}
	p := s.state.Prompts[shortID]
	claimed := s.state.Claims[p.NativeID]
	s.mu.Unlock()
	if claimed.State != "" {
		return s.reply(key, claimed.Feedback)
	}
	return s.reply(key, "敏感回答未在本机重启后保留，请重新发送原问题的回答；原请求尚未因此提交。")
}

// Handle returns user-visible feedback for every command. A durable claim is
// written before Decide. Any uncertain result is terminal until the original
// Runtime receipt resolves; a duplicate message only returns saved feedback.
func (s *Store) Handle(ctx context.Context, in Inbound, snapshot api.Snapshot) string {
	key := in.Channel + "\x00" + in.Conversation + "\x00" + in.ID
	s.mu.Lock()
	if previous, ok := s.state.Replies[key]; ok {
		s.mu.Unlock()
		return previous
	}
	s.mu.Unlock()
	if !in.observed {
		s.observeInbound(in)
	}
	parts := strings.Fields(strings.TrimSpace(in.Text))
	if len(parts) == 0 {
		return "空命令。"
	}
	if parts[0] != "/approve" && parts[0] != "/answer" {
		return s.reply(key, "未知命令。请复制当前请求中的完整 /approve 或 /answer 命令。")
	}
	if len(parts) < 3 {
		return s.reply(key, "命令不完整。请复制当前请求中的完整命令。")
	}
	id := parts[1]
	if parts[0] == "/approve" && !strings.HasPrefix(id, "A") || parts[0] == "/answer" && !strings.HasPrefix(id, "Q") {
		return s.reply(key, "请求编号无效。")
	}
	s.mu.Lock()
	p, ok := s.state.Prompts[id]
	_, async := s.state.AsyncQuestions[id]
	prior := s.state.Claims[p.NativeID]
	s.mu.Unlock()
	if async && parts[0] == "/answer" {
		return s.handleAsync(ctx, in, snapshot, id, key)
	}
	if !ok {
		return s.reply(key, "请求编号不存在或已经失效。")
	}
	if prior.State != "" {
		for _, a := range snapshot.Approvals {
			if a.ID == p.NativeID && a.Status == "resolved" {
				return s.reply(key, "原请求已处理。"+resolutionText(a.Resolution))
			}
		}
		return s.reply(key, prior.Feedback)
	}
	var current *api.Approval
	for i := range snapshot.Approvals {
		if snapshot.Approvals[i].ID == p.NativeID {
			current = &snapshot.Approvals[i]
			break
		}
	}
	if current == nil || current.Status != "pending" || current.TurnKey != p.TurnKey || current.Owner != p.Owner {
		return s.reply(key, "原请求已失效或已处理，请查看最新请求。")
	}
	if p.Schema == "" || catalogFingerprint(*current) != p.Schema {
		return s.reply(key, "原请求内容已变化，请使用最新卡片中的命令。")
	}
	// The native request, all sibling fields and offered choices are frozen by
	// Schema. Also compare this field's exact ordered option IDs before parsing.
	var actual []option
	if p.QuestionID != "" {
		var q *api.Question
		for i := range current.Questions {
			if current.Questions[i].ID == p.QuestionID {
				q = &current.Questions[i]
				break
			}
		}
		if q == nil || q.Secret != p.Secret || q.Multiple != p.Multiple || q.Type != p.Type || q.Required != p.Required || submitChoice(*current) != p.SubmitChoice {
			return s.reply(key, "问题已变化，请查看最新请求。")
		}
		actual = questionOptions(*q)
	} else {
		for _, o := range current.Choices {
			if len(current.Questions) > 0 && (o.ID == "answer" || o.ID == "accept") && hasRequired(current.Questions) {
				continue
			}
			actual = append(actual, option{o.ID, label(o), o.Scope, o.Details})
		}
	}
	if encodedA, _ := json.Marshal(actual); string(encodedA) != marshalOptions(p.Options) {
		return s.reply(key, "选项已变化，请使用新请求中的命令。")
	}
	// Keep the whole tail after the short ID. Only an all-digit tail is an index.
	tail := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.Text), parts[0]))
	tail = strings.TrimSpace(strings.TrimPrefix(tail, id))
	decision := api.Decision{ID: p.NativeID}
	var answer []string
	if p.QuestionID != "" {
		var problem string
		answer, problem = parseAnswer(p, tail)
		if problem != "" {
			return s.reply(key, problem)
		}
	} else {
		if !allDigits(tail) {
			return s.reply(key, "自定义意见不会授权，也不会转交 Bot。请使用当前请求列出的拒绝命令；如需改计划，再单独告诉 Bot。")
		}
		n, err := strconv.Atoi(tail)
		if err != nil || n < 1 || n > len(p.Options) {
			return s.reply(key, "选项序号无效，请复制此请求展示的完整命令。")
		}
		decision.Choice = p.Options[n-1].ID
	}
	s.mu.Lock()
	if prior = s.state.Claims[p.NativeID]; prior.State != "" {
		s.mu.Unlock()
		return s.reply(key, prior.Feedback)
	}
	answerStoreKey := answerKey(p.NativeID, p.Schema)
	if p.QuestionID != "" {
		if p.Secret {
			if s.secretAnswers[answerStoreKey] == nil {
				s.secretAnswers[answerStoreKey] = map[string][]string{}
			}
			s.secretAnswers[answerStoreKey][p.QuestionID] = slices.Clone(answer)
		} else {
			if s.state.Answers[answerStoreKey] == nil {
				s.state.Answers[answerStoreKey] = map[string][]string{}
			}
			previous, hadPrevious := s.state.Answers[answerStoreKey][p.QuestionID]
			s.state.Answers[answerStoreKey][p.QuestionID] = slices.Clone(answer)
			if err := s.save(); err != nil {
				if hadPrevious {
					s.state.Answers[answerStoreKey][p.QuestionID] = previous
				} else {
					delete(s.state.Answers[answerStoreKey], p.QuestionID)
				}
				s.mu.Unlock()
				return s.reply(key, "本地记录不可用，回答未提交；请重试。")
			}
		}
		if !hasRequired(current.Questions) {
			s.mu.Unlock()
			return s.reply(key, id+" 已填写；此请求没有必填字段，可继续填写或使用卡片中的提交命令。")
		}
	}
	collecting := len(current.Questions) > 0 && (p.QuestionID != "" || decision.Choice == "accept" || decision.Choice == "answer")
	if collecting {
		missing := s.missingLocked(current.Questions, answerStoreKey, p.Schema)
		if len(missing) > 0 {
			s.mu.Unlock()
			return s.reply(key, id+" 已填写；仍需回答 "+strings.Join(missing, "、")+"。提交前可再次回答已有字段以修改。")
		}
		decision.Choice = submitChoice(*current)
		if decision.Choice == "" {
			s.mu.Unlock()
			return s.reply(key, "Runtime 未提供提交回答的选项；原请求尚未提交。")
		}
		decision.Answers = s.answersLocked(current.Questions, answerStoreKey)
	}
	s.state.Claims[p.NativeID] = claim{State: "unknown", Feedback: "决定已收到，原请求结果待核对；不会重复提交。"}
	if err := s.save(); err != nil {
		delete(s.state.Claims, p.NativeID)
		s.mu.Unlock()
		return s.reply(key, "本地记录不可用，决定未提交。")
	}
	s.mu.Unlock()
	err := s.decide(ctx, decision)
	var validation api.DecisionValidationError
	if errors.As(err, &validation) {
		s.mu.Lock()
		old := s.state.Claims[p.NativeID]
		delete(s.state.Claims, p.NativeID) // adapter guarantees no native write
		if saveErr := s.save(); saveErr != nil {
			s.state.Claims[p.NativeID] = old
			s.mu.Unlock()
			return s.reply(key, "Runtime 未接受这些字段，但本地状态未能更新；请先核对原请求。")
		}
		s.mu.Unlock()
		return s.reply(key, "Runtime 未接受这些字段："+validation.Message+"。原请求仍待处理，可修改回答后重试。")
	}
	feedback := "决定已提交，等待 Runtime 确认。"
	state := "submitted"
	if err != nil {
		feedback = "决定结果暂不确定，请核对原请求；不会重复提交。"
		state = "unknown"
	}
	s.mu.Lock()
	s.state.Claims[p.NativeID] = claim{State: state, Feedback: feedback}
	delete(s.state.Answers, answerStoreKey)
	delete(s.secretAnswers, answerStoreKey)
	_ = s.save()
	s.mu.Unlock()
	return s.reply(key, feedback)
}

func parseAnswer(p prompt, tail string) ([]string, string) {
	if tail == "" {
		return nil, "回答不能为空。"
	}
	if p.Multiple {
		if len(p.Options) == 0 {
			return nil, "Runtime 未提供多选选项，请核对原请求。"
		}
		var result []string
		seen := map[int]bool{}
		for _, part := range strings.Split(tail, ",") {
			part = strings.TrimSpace(part)
			if !allDigits(part) {
				return nil, "多选请使用逗号分隔的数字索引，例如 1,3；不会当作自由文本。"
			}
			n, err := strconv.Atoi(part)
			if err != nil || n < 1 || n > len(p.Options) || seen[n] {
				return nil, "多选索引无效或重复，请按当前问题的选项编号填写。"
			}
			seen[n] = true
			result = append(result, p.Options[n-1].ID)
		}
		return result, ""
	}
	if len(p.Options) > 0 && allDigits(tail) {
		n, err := strconv.Atoi(tail)
		if err != nil || n < 1 || n > len(p.Options) {
			return nil, "选项序号无效，请复制当前问题展示的完整命令。"
		}
		return []string{p.Options[n-1].ID}, ""
	}
	if !p.Free {
		return nil, "此问题只接受列出的选项，请复制对应命令。"
	}
	if len(p.Options) > 0 && numericList(tail) {
		return nil, "此字段是单选；请选择一个编号，或输入非数字列表的自定义回答。"
	}
	if p.Type == "integer" || p.Type == "number" {
		n, err := strconv.ParseFloat(tail, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || p.Type == "integer" && (math.Trunc(n) != n || n != float64(int64(n))) {
			return nil, "数字格式无效，请输入有效的" + map[bool]string{true: "整数。", false: "数字。"}[p.Type == "integer"]
		}
	}
	return []string{tail}, ""
}
func numericList(v string) bool {
	parts := strings.Split(v, ",")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !allDigits(strings.TrimSpace(part)) {
			return false
		}
	}
	return true
}
func (s *Store) missingLocked(questions []api.Question, key, schema string) []string {
	var missing []string
	for _, q := range questions {
		if !q.Required {
			continue
		}
		if answer, ok := s.answerLocked(key, q.ID); !ok || len(answer) == 0 {
			for id, p := range s.state.Prompts {
				if p.QuestionID == q.ID && p.Schema == schema {
					missing = append(missing, id)
					break
				}
			}
		}
	}
	return missing
}
func (s *Store) answersLocked(questions []api.Question, key string) map[string][]string {
	result := map[string][]string{}
	for _, q := range questions {
		if answer, ok := s.answerLocked(key, q.ID); ok {
			result[q.ID] = answer
		}
	}
	return result
}
func marshalOptions(v []option) string { b, _ := json.Marshal(v); return string(b) }
func resolutionText(r *api.ApprovalResolution) string {
	if r == nil {
		return ""
	}
	switch r.Outcome {
	case "allowed":
		return "结果：已允许。"
	case "declined":
		return "结果：已拒绝。"
	case "cancelled":
		return "结果：已取消。"
	default:
		return ""
	}
}
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func (s *Store) SetNoticeObserver(observer func(Notice)) {
	s.mu.Lock()
	s.onNotice = observer
	s.mu.Unlock()
}

func (s *Store) SetInboundObserver(observer func(Inbound)) {
	s.mu.Lock()
	s.onInbound = observer
	s.mu.Unlock()
}

func (s *Store) observeInbound(in Inbound) {
	s.mu.Lock()
	observer := s.onInbound
	s.mu.Unlock()
	if observer != nil && in.ID != "" {
		observer(in)
	}
}

func noticeFor(key, body string, seenAt int64) Notice {
	h := sha256.Sum256([]byte(key))
	parts := strings.SplitN(key, "\x00", 3)
	origin := Origin{}
	if len(parts) == 3 {
		origin = Origin{Channel: parts[0], Conversation: parts[1]}
	}
	return Notice{ID: hex.EncodeToString(h[:]), Text: body, Origin: origin, SeenAt: seenAt}
}

func (s *Store) reply(key, body string) string {
	s.mu.Lock()
	if existing, exists := s.state.Replies[key]; exists {
		s.mu.Unlock()
		return existing
	}
	s.state.ReplyOrder = append(s.state.ReplyOrder, key)
	s.state.Replies[key] = body
	seenAt := time.Now().UnixMicro()
	s.state.ReplyTimes[key] = seenAt
	_ = s.save()
	observer := s.onNotice
	s.mu.Unlock()
	if observer != nil {
		observer(noticeFor(key, body, seenAt))
	}
	return body
}
func (s *Store) Publish(in Inbound, body string) string {
	if in.Channel == "" || in.ID == "" {
		return ""
	}
	return s.reply(in.Channel+"\x00"+in.Conversation+"\x00"+in.ID, body)
}
func (s *Store) Notices() []Notice {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Notice, 0, len(s.state.Replies))
	for _, key := range s.state.ReplyOrder {
		body, exists := s.state.Replies[key]
		if !exists {
			continue
		}
		result = append(result, noticeFor(key, body, s.state.ReplyTimes[key]))
	}
	return result
}
func (s *Store) Revision() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(len(s.state.ReplyOrder))
}
