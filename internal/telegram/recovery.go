package telegram

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tg "github.com/mymmrac/telego"
)

const recoveryMessageKey = "runtime:recovery"

func (b *Bridge) recoveryState() api.RecoveryState {
	if b.host.Recovery == nil {
		return api.RecoveryState{}
	}
	return b.host.Recovery()
}

func recoveryCallbackID(nonce, fence string, window int64) string {
	return "r:" + digest(nonce + "\x00" + fence + "\x00" + strconv.FormatInt(window, 10))[:40]
}

func recoveryWindow() int64 { return time.Now().Unix() / int64((15 * time.Minute).Seconds()) }

func (b *Bridge) mirrorRecovery(ctx context.Context, c client, snapshot api.Snapshot) {
	state := b.recoveryState()
	b.mu.Lock()
	chat := b.state.ChatID
	enabled := b.state.Enabled && !b.closed
	nonce := b.recoveryNonce
	record, existing := b.state.Messages[recoveryMessageKey]
	b.mu.Unlock()
	if !enabled || chat == 0 || (!existing && (!state.Manual || state.Fence == "" || b.host.Recover == nil)) {
		return
	}
	text := b.text("The local Runtime is connected.", "本机 Runtime 已连接。")
	if state.Automatic || state.InProgress {
		text = b.text("Telegram is connected; checking the original Runtime connection. The Mac app must remain running.", "Telegram 已连接，正在核对原 Runtime 连接；Mac 应用需保持运行。")
	}
	if !state.Automatic && !state.InProgress && ready(snapshot) && snapshot.Phase == "unknown" {
		text = b.text("The local Runtime is connected; the original request outcome is still uncertain. Check it on your Mac before resending.", "本机 Runtime 已连接，但原请求结果仍不确定。再次发送前请在 Mac 核对。")
	}
	var keys *tg.InlineKeyboardMarkup
	if state.Manual && !state.Automatic && !state.InProgress && state.Fence != "" && b.host.Recover != nil && !ready(snapshot) {
		text = b.recoveryMessage(snapshot)
		keys = &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{{{
			Text: b.text("Reconnect", "重新连接"), CallbackData: recoveryCallbackID(nonce, state.Fence, recoveryWindow()),
		}}}}
	} else if !state.Automatic && !state.InProgress && snapshot.Connection == "login" {
		text = b.text("Sign in to the local Runtime on your Mac to continue recovery.", "请先在 Mac 登录本机 Runtime，再继续恢复。")
	} else if !ready(snapshot) {
		text = b.text("Telegram is connected; checking the original Runtime connection. The Mac app must remain running.", "Telegram 已连接，正在核对原 Runtime 连接；Mac 应用需保持运行。")
	}
	if existing && record.Skip {
		return
	}
	b.sendText(ctx, c, recoveryMessageKey, chat, text, keys)
}

func (b *Bridge) recoveryMessage(snapshot api.Snapshot) string {
	switch snapshot.ConnectionIssue {
	case "resource_exhausted":
		return b.text("Telegram is connected, but the local Runtime lacks connection resources. Free resources on your Mac, then reconnect. Unknown work stays on its original receipt.", "Telegram 已连接，但本机 Runtime 连接资源不足。请在 Mac 释放资源后重新连接；结果未知的工作保留原收据核对。")
	case "runtime_missing":
		return b.text("Telegram is connected, but the local Runtime is missing. Check its installation on your Mac, then reconnect.", "Telegram 已连接，但本机 Runtime 未找到。请在 Mac 检查安装后重新连接。")
	case "runtime_protocol":
		return b.text("Telegram is connected, but the local Runtime is incompatible. Update it on your Mac, then reconnect.", "Telegram 已连接，但本机 Runtime 协议不兼容。请在 Mac 更新后重新连接。")
	case "authentication":
		return b.text("Telegram is connected, but the local Runtime needs sign-in on your Mac before reconnection.", "Telegram 已连接，但本机 Runtime 需要先在 Mac 登录才能重新连接。")
	default:
		return b.text("Telegram is connected; the local Runtime is offline. Reconnect to the original work on your Mac. This button works only while the Mac app is running.", "Telegram 已连接，本机 Runtime 离线。可重新连接 Mac 上的原工作；按钮仅在 Mac 应用运行时有效。")
	}
}

func (b *Bridge) recoveryCallback(ctx context.Context, c client, q *tg.CallbackQuery, message *tg.Message) bool {
	state := b.recoveryState()
	b.mu.Lock()
	record := b.state.Messages[recoveryMessageKey]
	valid := b.state.Enabled && !b.closed && b.state.ChatID == message.Chat.ID && message.Chat.Type == "private" &&
		message.From != nil && message.From.IsBot && message.From.ID == b.state.BotID &&
		state.Manual && state.Fence != "" && b.host.Recover != nil &&
		q.Data == recoveryCallbackID(b.recoveryNonce, state.Fence, recoveryWindow()) &&
		!record.Skip && record.Keyboard != "" && len(record.IDs) > 0 &&
		record.IDs[len(record.IDs)-1] == message.MessageID &&
		b.recoveryClaim != state.Fence
	if valid {
		b.recoveryClaim = state.Fence
	}
	b.mu.Unlock()
	answerCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	if !valid {
		_ = c.Answer(answerCtx, q.ID, b.text("This reconnect button has expired. Check /status.", "此重新连接按钮已过期，请查看 /status。"))
		cancel()
		return true
	}
	_ = c.Answer(answerCtx, q.ID, b.text("Checking the original connection…", "正在核对原连接…"))
	cancel()
	if ctx.Err() != nil {
		return true
	}
	b.recoveryWait.Add(1)
	go func() {
		defer b.recoveryWait.Done()
		workCtx, stop := context.WithTimeout(ctx, 35*time.Second)
		defer stop()
		_ = b.host.Recover(workCtx, state.Fence)
	}()
	return true
}

func isRecoveryButton(data string) bool { return strings.HasPrefix(data, "r:") }
