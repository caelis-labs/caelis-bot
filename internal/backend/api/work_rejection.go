package api

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// WorkAdmissionError classifies an admission condition without settling any
// input receipt. A failed admission check can also occur while reconciling an
// earlier dispatch, so it must not itself be a WorkMessageRejection.
type WorkAdmissionError struct {
	Code  string
	Cause error
}

func (e *WorkAdmissionError) Error() string { return e.Cause.Error() }
func (e *WorkAdmissionError) Unwrap() error { return e.Cause }

// WorkAdmissionReason preserves only adapter-proven classifications. Unknown
// admission failures carry no assertion that waiting will make them succeed.
func WorkAdmissionReason(err error) string {
	var admission *WorkAdmissionError
	if errors.As(err, &admission) && admission.Code != "" {
		return admission.Code
	}
	return "work_admission_failed"
}

var delegationPrivateTail = regexp.MustCompile(`(?i)(?:\b(?:body|prompt|payload|request[ _-]?body|response[ _-]?body|authorization|proxy-authorization|cookie|set-cookie|headers?|credentials?|password|passwd|secret|access[ _-]?token|refresh[ _-]?token|token|api[ _-]?key)\b["']?\s*(?:[:=]|\s)|\b(?:request|response)["']?\s*[:=]|\b(?:bearer|basic)\s+|[\{\[])`)
var delegationURL = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s<>"']+`)
var delegationSecret = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]+|gh[pousr]_[a-z0-9_]+|github_pat_[a-z0-9_]+|AKIA[A-Z0-9]{16}|eyJ[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+)\b`)

// WorkRejectionDetail projects an internal refusal for Bot diagnostics only.
// Causes can contain complete prompts, credentials and native response bodies.
// Retain only a sanitized first-line summary; never retain a private payload tail.
func WorkRejectionDetail(cause error) string {
	if cause == nil {
		return "No additional cause details are available."
	}
	text := cause.Error()
	var native interface{ WorkRejectionDetail() string }
	if errors.As(cause, &native) && native.WorkRejectionDetail() != "" {
		text = native.WorkRejectionDetail()
	}
	if end := strings.IndexAny(text, "\r\n"); end >= 0 {
		text = text[:end]
	}
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(text, ""))
	// Remove URLs before looking for credential keys in their query strings.
	// Do not let our replacement markers become structured-payload markers.
	text = delegationURL.ReplaceAllString(text, "URL_REDACTED")
	if location := delegationPrivateTail.FindStringIndex(text); location != nil {
		text = text[:location[0]] + "[private details omitted]"
	}
	text = strings.ReplaceAll(text, "URL_REDACTED", "[URL redacted]")
	text = delegationSecret.ReplaceAllString(text, "[credential redacted]")
	text = strings.TrimSpace(text)
	if len(text) > 512 {
		end := 509
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		text = text[:end] + "…"
	}
	if text == "" {
		return "Additional cause details were withheld for privacy."
	}
	return text
}
