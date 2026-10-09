package telegram

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"html"
	"io"
	"net/url"
	"strings"
	"unicode/utf16"

	"github.com/mymmrac/telego"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

var telegramMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(gmhtml.WithXHTML()))

// Split on lines so links and table rows stay intact. Reopen fenced code in
// the next rich chunk; Text always retains the exact original source portion.
func assistantMessages(body string) []outgoingText {
	if body == "" {
		return nil
	}
	if utf16Length(body) <= 4000 {
		return []outgoingText{{Text: body, Markdown: body}}
	}
	const limit = 4000
	var result []outgoingText
	var raw, formatted strings.Builder
	fenceOpen, fenceClose := "", ""
	tableHeader, tableDivider, tableUntil := "", "", -1
	flush := func() {
		if raw.Len() == 0 {
			return
		}
		markdown := formatted.String()
		if fenceClose != "" {
			markdown += "\n" + fenceClose + "\n"
		}
		result = append(result, outgoingText{Text: raw.String(), Markdown: markdown})
		raw.Reset()
		formatted.Reset()
		if fenceOpen != "" {
			formatted.WriteString(fenceOpen + "\n")
		}
	}
	lines := strings.SplitAfter(body, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		if i+1 < len(lines) && strings.Contains(line, "|") && isMarkdownTableDivider(lines[i+1]) {
			tableHeader, tableDivider, tableUntil = line, lines[i+1], i+1
		} else if i > tableUntil && tableUntil >= 0 {
			if strings.TrimSpace(line) == "" || !strings.Contains(line, "|") {
				tableHeader, tableDivider, tableUntil = "", "", -1
			}
		}
		lineUnits := len(utf16.Encode([]rune(line)))
		if lineUnits > limit-64 {
			flush()
			for _, part := range splitTextLimit(line, limit) {
				result = append(result, plainText(part))
			}
			continue
		}
		reserve := len(fenceClose) + 4
		if raw.Len() > 0 && utf16Length(formatted.String())+lineUnits+reserve > limit {
			flush()
			if tableUntil >= 0 && i > tableUntil {
				formatted.WriteString(tableHeader)
				formatted.WriteString(tableDivider)
			}
		}
		raw.WriteString(line)
		formatted.WriteString(line)
		marker := strings.TrimLeft(line, " ")
		if len(line)-len(marker) <= 3 && len(marker) >= 3 && (marker[0] == '`' || marker[0] == '~') {
			n := 0
			for n < len(marker) && marker[n] == marker[0] {
				n++
			}
			if n >= 3 {
				if fenceClose == "" {
					fenceOpen = strings.TrimRight(line, "\r\n")
					fenceClose = strings.Repeat(string(marker[0]), n)
				} else if marker[0] == fenceClose[0] && n >= len(fenceClose) {
					fenceOpen, fenceClose = "", ""
				}
			}
		}
	}
	flush()
	return result
}

func isMarkdownTableDivider(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "|") || !strings.Contains(line, "-") {
		return false
	}
	for _, r := range line {
		if r != '|' && r != '-' && r != ':' && r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

func isFormatRejection(err error) bool {
	var transport *transportError
	return errors.As(err, &transport) && transport.formatRejected
}

// Telegram's legacy HTML mode accepts a small subset of HTML. Render CommonMark
// with GFM first, then allow only Bot API tags. Text and link attributes are
// escaped independently; raw HTML in the model response is never trusted.
func telegramHTML(markdown string) string {
	var rendered bytes.Buffer
	if telegramMarkdown.Convert([]byte(markdown), &rendered) != nil {
		return ""
	}
	z := xml.NewDecoder(&rendered)
	z.Strict = false
	var out strings.Builder
	inTable, cellCount := false, 0
	linkOpen := false
	for {
		value, err := z.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ""
		}
		switch tok := value.(type) {
		case xml.CharData:
			content := string(tok)
			if inTable {
				content = strings.TrimSpace(content)
			}
			out.WriteString(html.EscapeString(content))
		case xml.StartElement:
			switch tok.Name.Local {
			case "table":
				inTable = true
				out.WriteString("<pre>")
			case "tr":
				cellCount = 0
			case "td", "th":
				if cellCount > 0 {
					out.WriteString(" | ")
				}
				cellCount++
			case "h1", "h2", "h3", "h4", "h5", "h6", "strong", "b":
				if !inTable {
					out.WriteString("<b>")
				}
			case "em", "i":
				if !inTable {
					out.WriteString("<i>")
				}
			case "del", "s":
				if !inTable {
					out.WriteString("<s>")
				}
			case "code":
				if !inTable {
					out.WriteString("<code>")
				}
			case "pre":
				if !inTable {
					out.WriteString("<pre>")
				}
			case "blockquote":
				if !inTable {
					out.WriteString("<blockquote>")
				}
			case "li":
				out.WriteString("• ")
			case "a":
				if inTable {
					break
				}
				for _, a := range tok.Attr {
					if a.Name.Local != "href" {
						continue
					}
					u, err := url.Parse(a.Value)
					if err == nil && (u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "tg") {
						out.WriteString("<a href=\"")
						out.WriteString(html.EscapeString(a.Value))
						out.WriteString("\">")
						linkOpen = true
					}
				}
			case "br":
				out.WriteByte('\n')
			}
		case xml.EndElement:
			switch tok.Name.Local {
			case "table":
				out.WriteString("</pre>\n")
				inTable = false
			case "tr":
				out.WriteByte('\n')
			case "h1", "h2", "h3", "h4", "h5", "h6":
				if !inTable {
					out.WriteString("</b>\n")
				}
			case "strong", "b":
				if !inTable {
					out.WriteString("</b>")
				}
			case "em", "i":
				if !inTable {
					out.WriteString("</i>")
				}
			case "del", "s":
				if !inTable {
					out.WriteString("</s>")
				}
			case "code":
				if !inTable {
					out.WriteString("</code>")
				}
			case "pre":
				if !inTable {
					out.WriteString("</pre>\n")
				}
			case "blockquote":
				if !inTable {
					out.WriteString("</blockquote>\n")
				}
			case "p", "li":
				out.WriteByte('\n')
			case "a":
				if linkOpen {
					out.WriteString("</a>")
					linkOpen = false
				}
			}
		}
	}
	return strings.TrimSpace(out.String())
}

func (s *sdkClient) sendFormattedFallback(ctx context.Context, chat int64, message outgoingText, keys *telego.InlineKeyboardMarkup) (int, error) {
	if formatted := telegramHTML(message.Markdown); formatted != "" {
		v, err := s.bot.SendMessage(ctx, &telego.SendMessageParams{ChatID: telego.ChatID{ID: chat}, Text: formatted, ParseMode: "HTML", ReplyMarkup: keys})
		safe := safeMethodError("sendMessage", err)
		reportDeliveryAttempt(ctx, "sendMessage", "html", safe, true)
		if safe == nil {
			return v.MessageID, nil
		}
		if !isFormatRejection(safe) {
			return 0, safe
		}
	}
	v, err := s.bot.SendMessage(ctx, &telego.SendMessageParams{ChatID: telego.ChatID{ID: chat}, Text: message.Text, ReplyMarkup: keys})
	safe := safeMethodError("sendMessage", err)
	reportDeliveryAttempt(ctx, "sendMessage", "plain", safe, true)
	if safe != nil {
		return 0, safe
	}
	return v.MessageID, nil
}

func (s *sdkClient) editFormattedFallback(ctx context.Context, chat int64, id int, message outgoingText, keys *telego.InlineKeyboardMarkup) error {
	if formatted := telegramHTML(message.Markdown); formatted != "" {
		_, err := s.bot.EditMessageText(ctx, &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: chat}, MessageID: id, Text: formatted, ParseMode: "HTML", ReplyMarkup: keys})
		safe := safeMethodError("editMessageText", err)
		reportDeliveryAttempt(ctx, "editMessageText", "html", safe, true)
		if safe == nil || issueOf(safe) == "unchanged" {
			return nil
		}
		if !isFormatRejection(safe) {
			return safe
		}
	}
	_, err := s.bot.EditMessageText(ctx, &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: chat}, MessageID: id, Text: message.Text, ReplyMarkup: keys})
	safe := safeMethodError("editMessageText", err)
	reportDeliveryAttempt(ctx, "editMessageText", "plain", safe, true)
	if issueOf(safe) == "unchanged" {
		return nil
	}
	return safe
}
