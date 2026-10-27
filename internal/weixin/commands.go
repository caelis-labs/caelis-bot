package weixin

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func localCommand(body string) bool {
	fields := strings.Fields(body)
	if len(fields) != 1 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "/refresh", "/r", "/help", "/status", "/stop":
		return true
	}
	return false
}

func (b *Bridge) localFeedback(ctx context.Context, p *protocol, in inbound, body string) {
	b.mu.Lock()
	if b.state.Window.InputID != in.ID {
		b.mu.Unlock()
		return
	}
	b.enqueueDeliveryLocked(queuedDelivery{Key: "local:" + in.ID, Body: body, Priority: resultPriority})
	_ = b.saveLocked()
	b.mu.Unlock()
	b.output(ctx, p)
}

func (b *Bridge) handleLocalCommand(ctx context.Context, p *protocol, in inbound) {
	command := strings.ToLower(strings.TrimSpace(in.Text))
	switch command {
	case "/refresh", "/r":
		b.mu.Lock()
		if b.state.Window.InputID != in.ID {
			b.mu.Unlock()
			return
		}
		b.state.Window.Recover = true
		b.rotateDeliveriesLocked()
		for i := range b.state.Delivery.Queue {
			if b.state.Delivery.Queue[i].Priority == resultPriority {
				b.state.Delivery.Queue[i].Held = false
			}
		}
		// /r selects only known unattempted results in the existing delivery spool.
		// Accepted / unknown intents are reconciled by their original keys, never replayed.
		_ = b.saveLocked()
		b.mu.Unlock()
		b.output(ctx, p)
		b.mu.Lock()
		used := b.state.Window.Used
		b.mu.Unlock()
		if used == 0 {
			b.localFeedback(ctx, p, in, "当前没有可恢复的未发送结果；投递结果未知的消息不会重发，完整记录可在桌面同一 Bot 对话查看。")
		}

	case "/help":
		b.localFeedback(ctx, p, in, "微信命令：/r（或 /refresh）刷新接收窗口并恢复漏发结果，不重新执行工作；/status 查看状态；/stop 停止当前工作；/help 查看帮助。审批与问题请使用卡片中的 /approve 或 /answer 命令。")
	case "/status":
		phase, connection := "未知", "未知"
		if b.host.Snapshot != nil {
			s := b.host.Snapshot()
			phase, connection = s.Phase, s.Connection
		}
		b.mu.Lock()
		used, pending, exhausted := b.state.Window.Used, len(b.state.Delivery.Queue), b.state.Window.Exhausted
		if b.state.PendingFinal != "" {
			pending++
		}
		b.mu.Unlock()
		budget := fmt.Sprintf("本轮微信发送估计 %d/%d", used, windowSendLimit)
		if exhausted {
			budget = "本轮微信发送窗口可能已用尽"
		}
		b.localFeedback(ctx, p, in, fmt.Sprintf("Bot：%s；Runtime：%s。待发消息：%d；%s。", phase, connection, pending, budget))
	case "/stop":
		if b.host.Interrupt == nil {
			b.localFeedback(ctx, p, in, "当前无法提交停止请求。")
			return
		}
		work, cancel := context.WithTimeout(ctx, 25*time.Second)
		err := b.host.Interrupt(work)
		cancel()
		if err != nil {
			b.localFeedback(ctx, p, in, "停止结果尚未确认；请用 /status 核对原工作。")
		} else {
			b.localFeedback(ctx, p, in, "停止请求已提交，正在核对原工作。")
		}
	}
}
