package textchannel

import (
	"cmp"
	"maps"
	"slices"
	"time"
)

const controlHistoryLimit = 4096

// Active native targets and uncertain claims are bounded by the 999-number
// pools. Settled feedback/card caches are disposable; native IDs remain the
// final authority. Retired secret card references enter a fixed-size privacy
// filter that can hide text but cannot supply authority.
func (s *Store) compactNativeControls() {
	now := time.Now().Unix()
	keys := map[string]bool{}
	for id, p := range s.state.Prompts {
		if shortIDExpired(p.IssuedAt) && s.state.Claims[p.NativeID].State != "unknown" {
			delete(s.state.Prompts, id)
			continue
		}
		if s.state.Claims[p.NativeID].State == "unknown" && shortIDExpired(p.IssuedAt) {
			p.Options = nil
			s.state.Prompts[id] = p
			continue
		}
		keys[answerKey(p.NativeID, p.Schema)] = true
	}
	for key := range s.state.Answers {
		if !keys[key] {
			delete(s.state.Answers, key)
		}
	}
	for key := range s.secretAnswers {
		if !keys[key] {
			delete(s.secretAnswers, key)
		}
	}
	settled := []string{}
	for id, c := range s.state.Claims {
		if c.IssuedAt == 0 {
			c.IssuedAt = now
			s.state.Claims[id] = c
		}
		if c.State != "unknown" {
			settled = append(settled, id)
		}
	}
	slices.SortFunc(settled, func(a, b string) int { return cmp.Compare(s.state.Claims[b].IssuedAt, s.state.Claims[a].IssuedAt) })
	for i, id := range settled {
		if i >= controlHistoryLimit || shortIDExpired(s.state.Claims[id].IssuedAt) {
			delete(s.state.Claims, id)
		}
	}
	cards := []string{}
	for id, c := range s.state.Cards {
		if c.IssuedAt == 0 {
			c.IssuedAt = now
			s.state.Cards[id] = c
		}
		cards = append(cards, id)
	}
	slices.SortFunc(cards, func(a, b string) int { return cmp.Compare(s.state.Cards[b].IssuedAt, s.state.Cards[a].IssuedAt) })
	for i, id := range cards {
		if i >= controlHistoryLimit || shortIDExpired(s.state.Cards[id].IssuedAt) {
			if s.state.Cards[id].Secret {
				s.retireSecretCard(id)
			}
			delete(s.state.Cards, id)
		}
	}
	if len(s.state.ReplyOrder) > controlHistoryLimit {
		for _, id := range s.state.ReplyOrder[:len(s.state.ReplyOrder)-controlHistoryLimit] {
			delete(s.state.Replies, id)
			delete(s.state.ReplyTimes, id)
		}
		s.state.ReplyOrder = slices.Clone(s.state.ReplyOrder[len(s.state.ReplyOrder)-controlHistoryLimit:])
	}
}

func cloneMaintenanceState(d document) document {
	d.RetiredSecretCards = slices.Clone(d.RetiredSecretCards)
	d.AsyncQuestions = maps.Clone(d.AsyncQuestions)
	d.AsyncClosed = maps.Clone(d.AsyncClosed)
	d.AsyncActive = maps.Clone(d.AsyncActive)
	d.AsyncRetired = maps.Clone(d.AsyncRetired)
	d.AsyncReceipts = maps.Clone(d.AsyncReceipts)
	d.Claims = maps.Clone(d.Claims)
	d.Prompts = maps.Clone(d.Prompts)
	d.Answers = maps.Clone(d.Answers)
	d.Cards = maps.Clone(d.Cards)
	d.Replies = maps.Clone(d.Replies)
	d.ReplyTimes = maps.Clone(d.ReplyTimes)
	return d
}
