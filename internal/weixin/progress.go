package weixin

import (
	"context"
	"fmt"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

const (
	progressBatchCount = 3
	progressBatchUnits = textChunkUnits * 7 / 10
	progressQueueLimit = 128
	taskSourceLimit    = 1024
	progressDivider    = "\n\n——\n\n"
)

func (b *Bridge) observeTaskSources(snapshot api.Snapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	oldOrder := append([]string(nil), b.state.TaskOrder...)
	added := []string{}
	evicted := map[string]string{}
	orphanOrigins := map[string]string{}
	orphanOwners := map[string]string{}
	for _, item := range snapshot.Items {
		if item.Task == nil || item.Task.ID == "" || item.TurnKey == "" || b.state.TaskTurns[item.Task.ID] != "" {
			continue
		}
		b.state.TaskTurns[item.Task.ID] = item.TurnKey
		b.state.TaskOrder = append(b.state.TaskOrder, item.Task.ID)
		added = append(added, item.Task.ID)
	}
	for len(b.state.TaskOrder) > taskSourceLimit {
		id := b.state.TaskOrder[0]
		evicted[id] = b.state.TaskTurns[id]
		delete(b.state.TaskTurns, id)
		b.state.TaskOrder = b.state.TaskOrder[1:]
	}
	for _, turn := range evicted {
		if b.state.TurnPhases[turn] == "" && !b.taskTurnReferencedLocked(turn) {
			orphanOrigins[turn] = b.state.TurnOrigins[turn]
			orphanOwners[turn] = b.state.TurnOwners[turn]
			delete(b.state.TurnOwners, turn)
			delete(b.state.TurnOrigins, turn)
		}
	}
	if len(added) == 0 && len(evicted) == 0 {
		return
	}
	if b.saveLocked() != nil {
		for _, id := range added {
			delete(b.state.TaskTurns, id)
		}
		for id, turn := range evicted {
			b.state.TaskTurns[id] = turn
		}
		for turn, origin := range orphanOrigins {
			if origin != "" {
				b.state.TurnOrigins[turn] = origin
			}
			if orphanOwners[turn] != "" {
				b.state.TurnOwners[turn] = orphanOwners[turn]
			}
		}
		b.state.TaskOrder = oldOrder
	}
}

func (b *Bridge) taskTurnReferencedLocked(turn string) bool {
	for _, source := range b.state.TaskTurns {
		if source == turn {
			return true
		}
	}
	return false
}

func (b *Bridge) ownsApproval(snapshot api.Snapshot, approval api.Approval) bool {
	if approval.Owner != "task" {
		return b.ownsTurn(snapshot, approval.TurnKey)
	}
	if approval.TaskID == "" {
		return false
	}
	b.mu.Lock()
	sourceTurn := b.state.TaskTurns[approval.TaskID]
	b.mu.Unlock()
	return b.ownsTurn(snapshot, sourceTurn)
}

// A phone send must be traced to the exact Weixin input of this Turn. A newer
// Weixin window or a similar-looking message is not evidence of ownership.
func (b *Bridge) ownsTurn(snapshot api.Snapshot, turn string) bool {
	if turn == "" {
		return false
	}
	reports := map[string]string{}
	for _, source := range snapshot.ReportSources {
		if source.TurnKey == turn && b.host.TaskForReport != nil {
			reports[source.ReportID] = b.host.TaskForReport(source.ReportID)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	oldOrigin, oldOwner := b.state.TurnOrigins[turn], b.state.TurnOwners[turn]
	if oldOwner != "" && oldOwner != snapshot.RuntimeOwner {
		b.routeWarningLocked(snapshot, turn, "runtime_mismatch")
		return false
	}
	origin := oldOrigin
	observe := func(value string) {
		if value != "" {
			if origin != "" && origin != value {
				value = "mixed"
			}
			origin = value
		}
	}
	for _, source := range snapshot.ReportSources {
		if source.TurnKey != turn {
			continue
		}
		if snapshot.RuntimeOwner == "" || source.RuntimeOwner != snapshot.RuntimeOwner {
			b.routeWarningLocked(snapshot, turn, "report_scope_mismatch")
			return false
		}
		task := reports[source.ReportID]
		original := b.state.TaskTurns[task]
		inherited := b.state.TurnOrigins[original]
		if source.ReportID == "" || task == "" || original == "" || inherited == "" {
			b.routeWarningLocked(snapshot, turn, "report_source_missing")
			return false
		}
		if b.state.TurnOwners[original] != snapshot.RuntimeOwner {
			b.routeWarningLocked(snapshot, turn, "task_scope_mismatch")
			return false
		}
		observe(inherited)
	}
	for _, item := range snapshot.Items {
		if item.TurnKey != turn || item.Kind != "user" {
			continue
		}
		observed := "other"
		if item.RequestID != "" && (item.RequestID == b.state.AcceptedInputID || b.state.Inputs[item.RequestID] == "accepted") {
			observed = "weixin"
		}
		observe(observed)
	}
	if origin == "" {
		b.routeWarningLocked(snapshot, turn, "turn_source_missing")
		return false
	}
	owner := oldOwner
	if owner == "" {
		owner = snapshot.RuntimeOwner
	}
	if origin != oldOrigin || owner != oldOwner {
		b.state.TurnOrigins[turn], b.state.TurnOwners[turn] = origin, owner
		if b.saveLocked() != nil {
			if oldOrigin == "" {
				delete(b.state.TurnOrigins, turn)
			} else {
				b.state.TurnOrigins[turn] = oldOrigin
			}
			if oldOwner == "" {
				delete(b.state.TurnOwners, turn)
			} else {
				b.state.TurnOwners[turn] = oldOwner
			}
			b.routeWarningLocked(snapshot, turn, "source_storage_failed")
			return false
		}
	}
	return origin == "weixin"
}

// A routing skip is distinguishable from an outbox/send failure. Never include
// peer IDs, reply contexts, report bodies or credentials in this diagnostic.
func (b *Bridge) routeWarningLocked(snapshot api.Snapshot, turn, reason string) {
	key := diagnosticlog.Fingerprint([]byte(snapshot.RuntimeOwner + "\x00" + turn + "\x00" + reason))
	if b.routeWarnings[key] || len(b.routeWarnings) >= 256 {
		return
	}
	if b.routeWarnings == nil {
		b.routeWarnings = map[string]bool{}
	}
	b.routeWarnings[key] = true
	b.host.Diagnostics.Write(diagnosticlog.Record{Level: "warn", Component: "weixin", Code: "route_source_unresolved", Reason: reason, Fingerprint: key})
}

func compactProgress(body string) string {
	if fitsText(body) {
		return body
	}
	runes := []rune(body)
	for keep := 1200; keep > 0; keep -= 32 {
		head := keep * 7 / 10
		tail := keep - head
		if head+tail >= len(runes) {
			continue
		}
		text := fmt.Sprintf("%s\n\n[... 中间省略 %d 字符 ...]\n\n%s", string(runes[:head]), len(runes)-keep, string(runes[len(runes)-tail:]))
		if fitsText(text) {
			return text
		}
	}
	return "[较长进展已保存在 Bot 聊天记录中]"
}

func (b *Bridge) queueProgress(item api.Item) {
	key := outputKey(item)
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, seen := b.state.Outputs[key]; seen {
		return
	}
	for _, pending := range b.state.Progress {
		if pending.ID == item.ID {
			return
		}
	}
	if len(b.state.Progress) >= progressQueueLimit {
		b.state.Outputs[key] = outbound{State: "skip"}
		b.state.Missed++
		if b.saveLocked() != nil {
			delete(b.state.Outputs, key)
			b.state.Missed--
		}
		return
	}
	oldWindow := b.state.ProgressWindowID
	if len(b.state.Progress) == 0 {
		b.state.ProgressWindowID = b.state.Window.InputID
	}
	b.state.Progress = append(b.state.Progress, pendingProgress{ID: item.ID, TurnKey: item.TurnKey, Text: compactProgress(item.Text)})
	if b.saveLocked() != nil {
		b.state.Progress = b.state.Progress[:len(b.state.Progress)-1]
		b.state.ProgressWindowID = oldWindow
	}
}

func progressBody(entries []pendingProgress) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, entry.Text)
	}
	return strings.Join(parts, progressDivider)
}

func (b *Bridge) flushProgress(ctx context.Context, p *protocol, force bool) {
	b.mu.Lock()
	if len(b.state.Progress) == 0 || b.remainingProgressLocked() == 0 {
		b.mu.Unlock()
		return
	}
	batch := append([]string(nil), b.state.ProgressBatch...)
	if len(batch) == 0 {
		body := ""
		overflow := false
		for _, pending := range b.state.Progress {
			candidate := pending.Text
			if body != "" {
				candidate = body + progressDivider + candidate
			}
			if !fitsText(candidate) {
				overflow = true
				break
			}
			body = candidate
			batch = append(batch, pending.ID)
		}
		if len(batch) == 0 {
			b.mu.Unlock()
			return
		}
		if !force && !overflow && b.state.Window.ProgressUsed > 0 && len(batch) < progressBatchCount && textUnits(body) < progressBatchUnits {
			b.mu.Unlock()
			return
		}
		b.state.ProgressBatch = batch
		if b.saveLocked() != nil {
			b.state.ProgressBatch = nil
			b.mu.Unlock()
			return
		}
	}
	entries := make([]pendingProgress, 0, len(batch))
	for _, id := range batch {
		for _, pending := range b.state.Progress {
			if pending.ID == id {
				entries = append(entries, pending)
				break
			}
		}
	}
	b.mu.Unlock()
	if len(entries) == 0 {
		return
	}
	key := "progress:" + textDigest(strings.Join(batch, "\x00"))
	_, state := b.sendProgressOne(ctx, p, key, progressBody(entries))
	if state == "deferred" || state == "unavailable" {
		return
	}
	b.mu.Lock()
	removed := map[string]bool{}
	for _, entry := range entries {
		removed[entry.ID] = true
		b.state.Outputs["item:"+entry.ID] = outbound{State: state}
	}
	remaining := b.state.Progress[:0]
	for _, pending := range b.state.Progress {
		if !removed[pending.ID] {
			remaining = append(remaining, pending)
		}
	}
	b.state.Progress = remaining
	b.state.ProgressBatch = nil
	_ = b.saveLocked()
	b.mu.Unlock()
}

func (b *Bridge) mirrorFinal(ctx context.Context, p *protocol, item api.Item, extraNames []api.Artifact) {
	b.mu.Lock()
	if b.state.Outputs[outputKey(item)].State == "skip" {
		delete(b.state.Outputs, outputKey(item)) // a dropped progress may be the terminal reply
	}
	if old, sent := b.state.Outputs[outputKey(item)]; sent && old.State != "skip" {
		// This completed item was already delivered as the first progress.
		// Earlier queued messages were not part of that send.
		remaining := b.state.Progress[:0]
		for _, pending := range b.state.Progress {
			if pending.ID != item.ID {
				remaining = append(remaining, pending)
			}
		}
		b.state.Progress = remaining
		_ = b.saveLocked()
		b.mu.Unlock()
		// A first progress may later become this Turn's final item. Files
		// published after that progress still need a visible filename reply.
		if body := withArtifactNames("", extraNames); body != "" {
			if !fitsText(body) {
				body = compactFinal(body)
			}
			b.sendOne(ctx, p, "final-files:"+item.ID, body, true)
		}
		return
	}
	ids := append([]string(nil), b.state.FinalProgressIDs...)
	if b.state.FinalProgressFor != item.ID {
		ids = nil
		if fitsText(item.Text) {
			var entries []pendingProgress
			for _, pending := range b.state.Progress {
				if pending.TurnKey != item.TurnKey || pending.ID == item.ID {
					continue
				}
				candidate := append(append([]pendingProgress(nil), entries...), pending)
				if !fitsText(progressBody(candidate) + progressDivider + item.Text) {
					break
				}
				entries = candidate
				ids = append(ids, pending.ID)
			}
		}
		if len(ids) != 0 {
			b.state.FinalProgressFor, b.state.FinalProgressIDs = item.ID, ids
			if b.saveLocked() != nil {
				b.state.FinalProgressFor, b.state.FinalProgressIDs = "", nil
				ids = nil
			}
		}
	}
	entries := make([]pendingProgress, 0, len(ids))
	for _, id := range ids {
		for _, pending := range b.state.Progress {
			if pending.ID == id {
				entries = append(entries, pending)
				break
			}
		}
	}
	b.mu.Unlock()
	if len(entries) > 0 {
		item.Text = progressBody(entries) + progressDivider + item.Text
	}
	b.mirrorItem(ctx, p, item, "final")
	b.mu.Lock()
	state := b.state.Outputs[outputKey(item)+":0"].State
	if state == "" {
		state = b.state.Outputs[outputKey(item)+":compact"].State
	}
	if state == "" {
		state = b.state.Outputs[outputKey(item)].State // already sent as the first progress
	}
	if state == "" {
		b.mu.Unlock() // no send attempt yet
		return
	}
	removed := map[string]bool{}
	removed[item.ID] = true
	for _, entry := range entries {
		removed[entry.ID] = true
		b.state.Outputs["item:"+entry.ID] = outbound{State: state}
	}
	remaining := b.state.Progress[:0]
	for _, pending := range b.state.Progress {
		if !removed[pending.ID] {
			remaining = append(remaining, pending)
		}
	}
	b.state.Progress = remaining
	b.state.FinalProgressFor, b.state.FinalProgressIDs = "", nil
	_ = b.saveLocked()
	b.mu.Unlock()
}
