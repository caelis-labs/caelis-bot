// Package telegram mirrors the existing conversation to one paired private chat.
// It owns transport and presentation only; the resident Bot remains unchanged.
package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	tg "github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
)

const maxInputBytes = 8 << 20 // Accepted by both current backend adapters.
const maxOutputBytes = 50 << 20

type client interface {
	Me(context.Context) (*tg.User, error)
	Webhook(context.Context) (bool, error)
	TakeOver(context.Context) error
	Updates(context.Context, int) ([]tg.Update, error)
	Send(context.Context, int64, outgoingText, *tg.InlineKeyboardMarkup) (int, error)
	Edit(context.Context, int64, int, outgoingText, *tg.InlineKeyboardMarkup) error
	EditMarkup(context.Context, int64, int, *tg.InlineKeyboardMarkup) error
	Document(context.Context, int64, string) error
	Download(context.Context, string, string) error
	Answer(context.Context, string, string) error
	ChatAction(context.Context, int64) error
	Commands(context.Context) error
}

// outgoingText keeps display entities in the Telegram adapter. Its Text is the
// exact visible text; entity offsets and lengths use UTF-16 code units.
type outgoingText struct {
	Text     string
	Entities []tg.MessageEntity
	Markdown string // Assistant-only rich content; Text remains the original visible source.
}

func plainText(text string) outgoingText { return outgoingText{Text: text} }

type transportError struct {
	issue          string
	code, retry    int
	formatRejected bool
}

func (e *transportError) Error() string { return e.issue }
func safeError(err error) error {
	if err == nil {
		return nil
	}
	var e *telegoapi.Error
	if errors.As(err, &e) {
		issue := "telegram_error"
		if e.ErrorCode == 400 && strings.Contains(e.Description, "message is not modified") {
			issue = "unchanged"
		}
		switch e.ErrorCode {
		case 401:
			issue = "invalid_token"
		case 403:
			issue = "blocked"
		case 409:
			issue = "occupied"
		case 429:
			issue = "rate_limited"
		}
		retry := 0
		if e.Parameters != nil {
			retry = e.Parameters.RetryAfter
		}
		description := strings.ToLower(e.Description)
		formatRejected := (e.ErrorCode == 400 && (strings.Contains(description, "parse") || strings.Contains(description, "rich message") || strings.Contains(description, "rich_message") || strings.Contains(description, "format"))) ||
			(e.ErrorCode == 404 && strings.Contains(description, "method") && strings.Contains(description, "not found"))
		return &transportError{issue: issue, code: e.ErrorCode, retry: retry, formatRejected: formatRejected}
	}
	return &transportError{issue: "network", code: 0}
}
func issueOf(err error) string {
	var e *transportError
	if errors.As(err, &e) {
		return e.issue
	}
	return "storage"
}

type sdkClient struct {
	bot  *tg.Bot
	http *http.Client
}

func newClient(token string) (client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = systemProxy
	h := &http.Client{Timeout: 40 * time.Second, Transport: transport}
	bot, err := tg.NewBot(token, tg.WithHTTPClient(h), tg.WithDiscardLogger())
	if err != nil {
		return nil, &transportError{issue: "invalid_token"}
	}
	return &sdkClient{bot, h}, nil
}
func (s *sdkClient) Me(ctx context.Context) (*tg.User, error) {
	v, e := s.bot.GetMe(ctx)
	return v, safeError(e)
}
func (s *sdkClient) Webhook(ctx context.Context) (bool, error) {
	v, e := s.bot.GetWebhookInfo(ctx)
	if e != nil {
		return false, safeError(e)
	}
	return v.URL != "", nil
}
func (s *sdkClient) TakeOver(ctx context.Context) error {
	return safeError(s.bot.DeleteWebhook(ctx, &tg.DeleteWebhookParams{DropPendingUpdates: true}))
}
func (s *sdkClient) Updates(ctx context.Context, offset int) ([]tg.Update, error) {
	v, e := s.bot.GetUpdates(ctx, &tg.GetUpdatesParams{Offset: offset, Timeout: 25, AllowedUpdates: []string{"message", "callback_query"}})
	return v, safeError(e)
}
func (s *sdkClient) Send(ctx context.Context, chat int64, message outgoingText, keys *tg.InlineKeyboardMarkup) (int, error) {
	if message.Markdown != "" {
		p := &tg.SendRichMessageParams{ChatID: tg.ChatID{ID: chat}, RichMessage: tg.InputRichMessage{Markdown: message.Markdown}, ReplyMarkup: keys}
		v, e := s.bot.SendRichMessage(ctx, p)
		if e == nil {
			return v.MessageID, nil
		}
		if !isFormatRejection(e) {
			return 0, safeError(e)
		}
		return s.sendFormattedFallback(ctx, chat, message, keys)
	}
	p := &tg.SendMessageParams{ChatID: tg.ChatID{ID: chat}, Text: message.Text, Entities: message.Entities}
	if keys != nil {
		p.ReplyMarkup = keys
	}
	v, e := s.bot.SendMessage(ctx, p)
	if e != nil {
		return 0, safeError(e)
	}
	return v.MessageID, nil
}
func (s *sdkClient) Edit(ctx context.Context, chat int64, id int, message outgoingText, keys *tg.InlineKeyboardMarkup) error {
	if message.Markdown != "" {
		_, e := s.bot.EditMessageText(ctx, &tg.EditMessageTextParams{ChatID: tg.ChatID{ID: chat}, MessageID: id, RichMessage: &tg.InputRichMessage{Markdown: message.Markdown}, ReplyMarkup: keys})
		if e == nil || issueOf(safeError(e)) == "unchanged" {
			return nil
		}
		if !isFormatRejection(e) {
			return safeError(e)
		}
		return s.editFormattedFallback(ctx, chat, id, message, keys)
	}
	_, e := s.bot.EditMessageText(ctx, &tg.EditMessageTextParams{ChatID: tg.ChatID{ID: chat}, MessageID: id, Text: message.Text, Entities: message.Entities, ReplyMarkup: keys})
	err := safeError(e)
	if err != nil && issueOf(err) == "unchanged" {
		return nil
	}
	return err
}
func (s *sdkClient) EditMarkup(ctx context.Context, chat int64, id int, keys *tg.InlineKeyboardMarkup) error {
	_, e := s.bot.EditMessageReplyMarkup(ctx, &tg.EditMessageReplyMarkupParams{ChatID: tg.ChatID{ID: chat}, MessageID: id, ReplyMarkup: keys})
	err := safeError(e)
	if err != nil && issueOf(err) == "unchanged" {
		return nil
	}
	return err
}
func (s *sdkClient) Document(ctx context.Context, chat int64, path string) error {
	f, e := os.Open(path)
	if e != nil {
		return errors.New("file_unavailable")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > maxOutputBytes {
		return errors.New("file_too_large")
	}
	_, e = s.bot.SendDocument(ctx, &tg.SendDocumentParams{ChatID: tg.ChatID{ID: chat}, Document: tg.InputFile{File: f}})
	return safeError(e)
}
func (s *sdkClient) Download(ctx context.Context, id, path string) error {
	v, e := s.bot.GetFile(ctx, &tg.GetFileParams{FileID: id})
	if e != nil {
		return safeError(e)
	}
	if v.FileSize > maxInputBytes || v.FilePath == "" {
		return errors.New("file_too_large")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, s.bot.FileDownloadURL(v.FilePath), nil)
	if e != nil {
		return errors.New("download_failed")
	}
	res, e := s.http.Do(req)
	if e != nil {
		return safeError(e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("download_failed")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("storage")
	}
	n, e := io.Copy(f, io.LimitReader(res.Body, maxInputBytes+1))
	closeErr := f.Close()
	if e != nil || closeErr != nil || n > maxInputBytes {
		os.Remove(path)
		return errors.New("download_failed")
	}
	return nil
}
func (s *sdkClient) Answer(ctx context.Context, id, text string) error {
	return safeError(s.bot.AnswerCallbackQuery(ctx, &tg.AnswerCallbackQueryParams{CallbackQueryID: id, Text: text}))
}
func (s *sdkClient) ChatAction(ctx context.Context, chat int64) error {
	return safeError(s.bot.SendChatAction(ctx, &tg.SendChatActionParams{ChatID: tg.ChatID{ID: chat}, Action: tg.ChatActionTyping}))
}
func (s *sdkClient) Commands(ctx context.Context) error {
	return safeError(s.bot.SetMyCommands(ctx, &tg.SetMyCommandsParams{Commands: []tg.BotCommand{{Command: "stop", Description: "Stop current work / 停止当前工作"}, {Command: "status", Description: "Connection and work status / 查看状态"}}}))
}
