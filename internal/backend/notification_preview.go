package backend

import (
	"html"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The OS receives a bounded plain-text excerpt, not Markdown, link destinations,
// raw HTML or code blocks. It never receives tool output or a conversation dump.
func notificationPreview(message string) string {
	source := []byte(message)
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	var out strings.Builder
	_ = ast.Walk(document, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			if n.Type() == ast.TypeBlock {
				out.WriteByte(' ')
			}
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock, *ast.RawHTML, *ast.Image, *ast.AutoLink:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			out.Write(util.UnescapePunctuations(node.Segment.Value(source)))
			if node.SoftLineBreak() || node.HardLineBreak() {
				out.WriteByte(' ')
			}
		case *ast.String:
			out.Write(node.Value)
		}
		return ast.WalkContinue, nil
	})
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, html.UnescapeString(out.String()))
	runes := []rune(strings.Join(strings.Fields(clean), " "))
	const limit = 180
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return string(runes)
}
