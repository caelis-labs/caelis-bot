// Package botmemory embeds the released Memory appliance in one Bot-owned
// scope. No provider credentials, workspace database or private Memory imports.
package botmemory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	owner "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	mem "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/appliance"
	sdk "github.com/caelis-labs/memory/sdk/go/memory"
)

type index struct {
	Version  int      `json:"version"`
	BotID    string   `json:"botId"`
	Receipts []string `json:"receipts"`
}
type Store struct {
	mu                 sync.Mutex
	runtime            *appliance.Runtime
	dir, scope, issuer string
	space              mem.SpaceID
	capability         mem.RuntimeCapability
	index              index
	closed             bool
}

func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:16]) }
func privateDir(path string) error {
	if e := os.MkdirAll(path, 0700); e != nil {
		return e
	}
	i, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return errors.New("个人数据目录不可重定向")
	}
	return os.Chmod(path, 0700)
}
func Open(ctx context.Context, dir, botID string) (s *Store, err error) {
	if botID == "" || !filepath.IsAbs(dir) {
		return nil, errors.New("个人空间需要稳定 Bot 身份")
	}
	if err = privateDir(dir); err != nil {
		return nil, err
	}
	s = &Store{dir: dir, scope: "bot-" + hash(botID), index: index{Version: 1, BotID: botID, Receipts: []string{}}}
	s.space = mem.SpaceID(s.scope)
	if info, e := os.Lstat(filepath.Join(dir, "index.json")); e == nil && !info.Mode().IsRegular() {
		return nil, errors.New("个人空间索引不可重定向")
	}
	if b, e := os.ReadFile(filepath.Join(dir, "index.json")); e == nil {
		if len(b) > 2<<20 || json.Unmarshal(b, &s.index) != nil || s.index.Version != 1 || s.index.BotID != botID {
			return nil, errors.New("个人空间身份不匹配，请保留数据")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	if err = privateDir(filepath.Join(dir, "memory")); err != nil {
		return nil, err
	}
	s.runtime, err = appliance.Open(ctx, appliance.Options{DataDir: filepath.Join(dir, "memory")})
	if err != nil {
		return nil, err
	}
	opened := s
	defer func() {
		if err != nil {
			opened.runtime.Close()
		}
	}()
	info, e := s.runtime.Management().Inspect(ctx)
	if e != nil {
		return nil, e
	}
	if len(info.Spaces) == 0 {
		_, err = s.runtime.Management().Bootstrap(ctx, owner.BootstrapRequest{
			Realms: []owner.Realm{{ID: mem.RealmID(s.scope)}}, Identities: []owner.Identity{{ID: mem.IdentityID(s.scope), RealmID: mem.RealmID(s.scope)}},
			Spaces: []owner.Space{{ID: s.space, RealmID: mem.RealmID(s.scope), IdentityID: mem.IdentityID(s.scope), Class: mem.SpaceClassPrivate}},
			Views:  []owner.ViewDefinition{{ID: mem.ViewID(s.scope), RealmID: mem.RealmID(s.scope), ReadSpaceIDs: []mem.SpaceID{s.space}, WriteSpaceID: s.space, MaxDisclosureClass: mem.SpaceClassPrivate, Version: 1}},
			Grants: []owner.Grant{{ID: mem.GrantID(s.scope), PrincipalRef: s.scope, ActorRef: s.scope, ViewRef: mem.ViewID(s.scope), AllowedOperations: []mem.Operation{mem.OperationRemember, mem.OperationRecall}, AllowedAudiences: []mem.Audience{mem.AudiencePrivate}, ExpiresAt: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), Version: 1}}, IssuerPrincipals: []string{s.scope}})
		if err != nil {
			return nil, err
		}
	} else if len(info.Spaces) != 1 || info.Spaces[0].ID != s.space || info.Spaces[0].IdentityID != mem.IdentityID(s.scope) {
		return nil, errors.New("Memory 不属于当前 Bot，拒绝接管")
	}
	// Rotate only this embedded appliance's issuer at launch. No issuer secret is
	// copied into Bot JSON or exposed to the Runtime/model. Capabilities renew in process.
	issued, e := s.runtime.Management().RotateIssuerCredential(ctx, s.scope)
	if e != nil {
		return nil, e
	}
	s.issuer = issued.Credential
	if err = localstate.Write(filepath.Join(dir, "index.json"), s.index); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.issuer = ""
	s.capability = mem.RuntimeCapability{}
	return s.runtime.Close()
}
func (s *Store) Authorization(ctx context.Context, _ mem.Operation) (mem.CallAuthorization, error) {
	// Called only while Store.mu is held, including through the bound SDK client.
	if s.closed {
		return mem.CallAuthorization{}, errors.New("个人记忆已关闭")
	}
	if !s.capability.ExpiresAt.After(time.Now().Add(time.Minute)) {
		c, e := s.runtime.IssueCapability(ctx, s.issuer, mem.CapabilityIssueRequest{PrincipalRef: s.scope, GrantRef: mem.GrantID(s.scope), ActorRef: s.scope, Audience: mem.AudiencePrivate, Operations: []mem.Operation{mem.OperationRemember, mem.OperationRecall}, TTLSeconds: 600})
		if e != nil {
			return mem.CallAuthorization{}, e
		}
		s.capability = c
	}
	return mem.CallAuthorization{Capability: s.capability.Token, ActorRef: s.scope, Audience: mem.AudiencePrivate}, nil
}
func (s *Store) client(source string) *sdk.Client {
	return sdk.NewClient(s.runtime.DataPlane(), s, mem.SourceContext{ActorRef: s.scope, SourceType: source}, mem.RecallBudget{MaxFragments: 12, MaxBytes: 16 << 10, DeadlineMS: 1000})
}
func mutation(id, text string) error {
	if len(id) < 8 || len(id) > 128 || !utf8.ValidString(id) || len(text) > 16<<10 || !utf8.ValidString(text) {
		return errors.New("请求标识或内容超出限制")
	}
	return nil
}
func (s *Store) rememberIndex(id string) error {
	for _, v := range s.index.Receipts {
		if v == id {
			return nil
		}
	}
	s.index.Receipts = append(s.index.Receipts, id)
	if e := localstate.Write(filepath.Join(s.dir, "index.json"), s.index); e != nil {
		s.index.Receipts = s.index.Receipts[:len(s.index.Receipts)-1]
		return errors.New("记忆已保存，但目录未确认；请用相同请求标识重试")
	}
	return nil
}
func receiptView(r *owner.Receipt) api.MemoryEntry {
	source := "助手保存的线索"
	if strings.HasPrefix(r.SourceContext.SourceType, "user") {
		source = "用户保存"
	}
	if r.CorrectionOf != "" {
		source = "已更正的线索"
	}
	return api.MemoryEntry{ID: string(r.ReceiptID), Text: r.Text, Source: source, Created: r.ReceivedAt.UTC().Format(time.RFC3339)}
}
func (s *Store) Remember(ctx context.Context, id, text, provider string) (api.MemoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := mutation(id, text); e != nil {
		return api.MemoryEntry{}, e
	}
	if s.closed {
		return api.MemoryEntry{}, errors.New("个人记忆已关闭")
	}
	if len(s.index.Receipts) >= 10000 {
		return api.MemoryEntry{}, errors.New("记忆目录已满")
	}
	source := "assistant:" + provider
	if provider == "settings" {
		source = "user:settings"
	}
	r, e := s.client(source).Remember(ctx, text, "remember-"+hash(provider+"\x00"+id), nil)
	if e != nil {
		return api.MemoryEntry{}, e
	}
	if !r.Accepted {
		return api.MemoryEntry{}, errors.New("记忆未被接受")
	}
	trace, e := s.runtime.Management().TraceReceipt(ctx, owner.TraceReceiptRequest{ReceiptID: r.ReceiptID})
	if e != nil {
		return api.MemoryEntry{}, e
	}
	if trace.State != owner.ReceiptStateActive || trace.Receipt == nil {
		return api.MemoryEntry{}, errors.New("原请求对应的记忆已更正或遗忘，不会重新建立")
	}
	if e = s.rememberIndex(string(r.ReceiptID)); e != nil {
		return api.MemoryEntry{}, e
	}
	return receiptView(trace.Receipt), nil
}
func (s *Store) ReadMemory(ctx context.Context, query string) (api.PersonalMemory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := api.PersonalMemory{Evidence: []api.MemoryEntry{}}
	if len(query) > 256 {
		return out, errors.New("搜索词过长")
	}
	if s.closed {
		return out, errors.New("个人记忆已关闭")
	}
	ids := []string{}
	if strings.TrimSpace(query) != "" {
		recalled, e := s.client("recall").Recall(ctx, query, "")
		if e != nil {
			return out, e
		}
		out.Truncated = out.Truncated || recalled.Truncated
		seen := map[string]bool{}
		for _, f := range recalled.Fragments {
			for _, r := range f.EvidenceRefs {
				if !seen[string(r)] {
					seen[string(r)] = true
					ids = append(ids, string(r))
				}
			}
		}
	} else {
		for i := len(s.index.Receipts) - 1; i >= 0; i-- {
			if len(ids) == 50 {
				out.Truncated = true
				break
			}
			ids = append(ids, s.index.Receipts[i])
		}
	}
	used := 0
	for _, id := range ids {
		trace, e := s.runtime.Management().TraceReceipt(ctx, owner.TraceReceiptRequest{ReceiptID: mem.ReceiptID(id)})
		if e != nil {
			return out, e
		}
		if trace.State != owner.ReceiptStateActive || trace.Receipt == nil {
			continue
		}
		r := trace.Receipt
		if r.SourceContext.SourceType == "trusted_evidence" {
			continue
		} // Retired profile evidence is available only to the one-time migration.
		if r.SpaceID != s.space {
			return out, errors.New("记忆范围不匹配")
		}
		if used+len(r.Text) > 16<<10 {
			out.Truncated = true
			continue
		}
		used += len(r.Text)
		out.Evidence = append(out.Evidence, receiptView(r))
	}
	return out, nil
}

func (s *Store) checkReceipt(ctx context.Context, id string) (owner.TraceReceiptResponse, error) {
	if s.closed {
		return owner.TraceReceiptResponse{}, errors.New("个人记忆已关闭")
	}
	r, e := s.runtime.Management().TraceReceipt(ctx, owner.TraceReceiptRequest{ReceiptID: mem.ReceiptID(id)})
	if e != nil {
		return r, e
	}
	if r.Receipt != nil && r.Receipt.SpaceID == s.space || r.Tombstone != nil && r.Tombstone.SpaceID == s.space {
		return r, nil
	}
	return r, errors.New("只能管理当前 Bot 的记忆")
}
func (s *Store) CorrectMemory(ctx context.Context, in api.MemoryChange) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := mutation(in.RequestID, in.Text); e != nil {
		return e
	}
	if _, e := s.checkReceipt(ctx, in.ID); e != nil {
		return e
	}
	r, e := s.runtime.Management().CorrectReceipt(ctx, owner.CorrectReceiptRequest{ReceiptID: mem.ReceiptID(in.ID), ReplacementText: in.Text, Reason: "correction through Bot memory tool", IdempotencyKey: "correct-" + hash(in.RequestID)})
	if e != nil {
		return e
	}
	return s.rememberIndex(string(r.ReplacementReceiptID))
}
func (s *Store) forget(ctx context.Context, id, request string) error {
	if _, e := s.checkReceipt(ctx, id); e != nil {
		return e
	}
	_, e := s.runtime.Management().DeleteReceipt(ctx, owner.DeleteReceiptRequest{ReceiptID: mem.ReceiptID(id), Reason: "forgetting through Bot memory tool", IdempotencyKey: "forget-" + hash(request+"\x00"+id)})
	return e
}
func (s *Store) ForgetMemory(ctx context.Context, in api.MemoryChange) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := mutation(in.RequestID, ""); e != nil {
		return e
	}
	// Remove ancestors first. The current receipt retains the ancestry link until
	// all older content has been forgotten, so a crash can resume from the same id.
	chain := []string{}
	id := in.ID
	seen := map[string]bool{}
	for id != "" {
		if seen[id] || len(chain) >= 10000 {
			return errors.New("记忆更正链无法读取")
		}
		seen[id] = true
		trace, e := s.checkReceipt(ctx, id)
		if e != nil {
			return e
		}
		chain = append(chain, id)
		if trace.Receipt == nil {
			break
		}
		if id == in.ID && trace.Receipt.CorrectedBy != "" {
			return errors.New("记忆已更正，请重新读取后再遗忘")
		}
		id = string(trace.Receipt.CorrectionOf)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if e := s.forget(ctx, chain[i], in.RequestID); e != nil {
			return e
		}
	}
	previous := s.index.Receipts
	next := []string{}
	for _, id := range previous {
		if !seen[id] {
			next = append(next, id)
		}
	}
	s.index.Receipts = next
	if e := localstate.Write(filepath.Join(s.dir, "index.json"), s.index); e != nil {
		s.index.Receipts = previous
		return errors.New("记忆已遗忘，目录更新未确认；请重试")
	}
	return nil
}

// LegacyProfile is used once when retiring the independent profile UI.
func (s *Store) LegacyProfile(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	auth, err := s.Authorization(ctx, mem.OperationRecall)
	if err != nil {
		return "", err
	}
	current, err := s.runtime.Facts().ReadFacts(ctx, auth, facts.ReadRequest{Subject: "user", Budget: facts.Budget{MaxFacts: 64, MaxBytes: 16 << 10}})
	if err != nil {
		return "", err
	}
	if current.Truncated {
		return "", errors.New("旧个人资料超过一次迁移范围，原始数据已保留")
	}
	if len(current.Facts) == 0 {
		return "", nil
	}
	var out strings.Builder
	out.WriteString("# 旧版个人资料\n\n以下资料来自旧版设置页，保留供用户和 Bot 整理。\n")
	for _, f := range current.Facts {
		out.WriteString("\n## " + f.Metadata.Key + "\n\n" + f.Text + "\n")
	}
	return out.String(), nil
}

var _ api.PersonalTools = (*Store)(nil)
