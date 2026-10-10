package weixin

import (
	"strings"
	"unicode/utf8"
)

// The plugin advertises 4000 characters, but the iLink server does not publish
// a guaranteed text ceiling. Keep headroom for CJK and message decoration.
const textChunkUnits = 1800 // UTF-16 code units, matching JavaScript string length.
const textChunkBytes = 5500

type markdownBlock struct {
	text                string
	fenceOpen, fenceEnd string
	fenceClose          string
}

func textUnits(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}

func fitsText(s string) bool { return textUnits(s) <= textChunkUnits && len(s) <= textChunkBytes }

func fenceMarker(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return "", false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == trimmed[0] {
		n++
	}
	if n < 3 {
		return "", false
	}
	return trimmed[:n], true
}

func markdownBlocks(s string) []markdownBlock {
	var blocks []markdownBlock
	var current strings.Builder
	open, close, closeLine := "", "", ""
	flush := func() {
		if current.Len() > 0 {
			blocks = append(blocks, markdownBlock{text: current.String(), fenceOpen: open, fenceEnd: close, fenceClose: closeLine})
			current.Reset()
			closeLine = ""
		}
	}
	for _, line := range strings.SplitAfter(s, "\n") {
		if line == "" {
			continue
		}
		marker, isFence := fenceMarker(line)
		if open == "" && isFence {
			flush()
			open = line
			close = marker
			current.WriteString(line)
			continue
		}
		current.WriteString(line)
		if open != "" && isFence && marker[0] == close[0] && len(marker) >= len(close) && strings.TrimSpace(strings.TrimPrefix(strings.TrimLeft(line, " "), marker)) == "" {
			closeLine = line
			flush()
			open, close = "", ""
		} else if open == "" && strings.TrimSpace(line) == "" {
			flush()
		}
	}
	flush()
	return blocks
}

func chunks(s string) []string {
	if s == "" {
		return nil
	}
	if fitsText(s) {
		return []string{s}
	}
	var result []string
	current := ""
	flush := func() {
		if current != "" {
			result = append(result, current)
			current = ""
		}
	}
	for _, block := range markdownBlocks(s) {
		if fitsText(current + block.text) {
			current += block.text
			continue
		}
		flush()
		if fitsText(block.text) {
			current = block.text
			continue
		}
		if block.fenceOpen != "" {
			result = append(result, splitFencedBlock(block)...)
		} else {
			result = append(result, splitPlainBlock(block.text, textChunkUnits, textChunkBytes)...)
		}
	}
	flush()
	return result
}

func splitFencedBlock(block markdownBlock) []string {
	// A long fence is closed and reopened at each message boundary. The synthetic
	// markers are presentation only; the original body remains unchanged in Bot.
	content := strings.TrimPrefix(block.text, block.fenceOpen)
	if block.fenceClose != "" {
		content = strings.TrimSuffix(content, block.fenceClose)
	}
	opener := strings.TrimRight(block.fenceOpen, "\r\n") + "\n"
	closer := "\n" + block.fenceEnd + "\n"
	ending := block.fenceClose
	if ending == "" {
		ending = closer
	}
	unitBudget := textChunkUnits - textUnits(opener) - max(textUnits(closer), textUnits(ending))
	byteBudget := textChunkBytes - len(opener) - max(len(closer), len(ending))
	if unitBudget < 16 || byteBudget < 64 {
		return splitPlainBlock(block.text, textChunkUnits, textChunkBytes)
	}
	segments := splitPlainBlock(content, unitBudget, byteBudget)
	if len(segments) == 0 {
		return []string{block.text}
	}
	result := make([]string, 0, len(segments))
	for i, segment := range segments {
		part := opener + segment
		if i == len(segments)-1 {
			part += ending
		} else {
			part += closer
		}
		result = append(result, part)
	}
	return result
}

func splitPlainBlock(s string, units, bytes int) []string {
	var result []string
	for s != "" {
		if textUnits(s) <= units && len(s) <= bytes {
			result = append(result, s)
			break
		}
		last, boundary, count := 0, 0, 0
		for index, r := range s {
			_, width := utf8.DecodeRuneInString(s[index:])
			step := 1
			if r > 0xffff {
				step = 2
			}
			if count+step > units || index+width > bytes {
				break
			}
			count += step
			last = index + width
			if count >= units*2/3 && (r == '\n' || r == ' ' || r == '\t' || r == '。' || r == '！' || r == '？' || r == '；' || r == '.' || r == '!' || r == '?') {
				boundary = last
			}
		}
		if boundary > 0 {
			last = boundary
		}
		if last == 0 {
			_, width := utf8.DecodeRuneInString(s)
			last = width
		}
		result = append(result, s[:last])
		s = s[last:]
	}
	return result
}
