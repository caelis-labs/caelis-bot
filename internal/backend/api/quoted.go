package api

import (
	"fmt"
	"strings"
)

// QuotedMessage is the text available in a user's reply to an older message.
// LocalID is optional and never an authorization handle. An excerpt is not a
// claim that the original message was fully available to the transport.
type QuotedMessage struct {
	HostID    string `json:"hostId,omitempty"` // transport message ID, if supplied; never decision authority
	LocalID   string `json:"localId,omitempty"`
	Role      string `json:"role,omitempty"` // user, assistant, or unknown
	Text      string `json:"text,omitempty"`
	Excerpt   bool   `json:"excerpt,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	// OmittedChars counts Unicode code points removed by the host's middle cut.
	OmittedChars int `json:"omittedChars,omitempty"`
}

const maxQuotedChars = 4096

var quoteEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func BoundQuote(q *QuotedMessage) *QuotedMessage {
	if q == nil {
		return nil
	}
	copy := *q
	if copy.Role != "user" && copy.Role != "assistant" {
		copy.Role = "unknown"
	}
	runes := []rune(copy.Text)
	// Rebinding a quote already cut by the host must not trim its retained tail.
	// The small allowance holds the omission marker, not another quoted body.
	if copy.OmittedChars > 0 && len(runes) <= maxQuotedChars+64 {
		return &copy
	}
	copy.OmittedChars = 0
	if len(runes) > maxQuotedChars {
		head := maxQuotedChars * 70 / 100
		tail := maxQuotedChars - head
		copy.OmittedChars = len(runes) - head - tail
		copy.Text = string(runes[:head]) + fmt.Sprintf("[... 中间省略 %d 字符 ...]", copy.OmittedChars) + string(runes[len(runes)-tail:])
		copy.Truncated = true
	}
	return &copy
}

// ModelQuotePrefix is only for native model text inputs. Submission.Text stays
// the user's visible body, so mirrored chat never repeats this wrapper.
func (s Submission) ModelQuotePrefix() string {
	if s.ModelInputOverride != "" {
		// Native user-item projection removes the whole structured envelope;
		// the local transcript keeps the original visible Text.
		return s.ModelInputOverride
	}
	q := BoundQuote(s.Quoted)
	if q == nil {
		return ""
	}
	return "<reference>\n" + quoteEscaper.Replace(q.Text) + "\n</reference>\n\n"
}

func (s Submission) ModelInputText() string {
	if s.ModelInputOverride != "" {
		return s.ModelInputOverride
	}
	return s.ModelQuotePrefix() + s.Text
}
