package textchannel

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const assistantPreviewTag = "<a2ui>"

var (
	assistantPreviewKind = ast.NewNodeKind("AssistantDesktopPreview")
	markdownAfterPreview = regexp.MustCompile("^(?:#{1,6}[ \t]|[-+*][ \t]|[0-9]+[.)][ \t]|\\[[^\\]]+\\](?:\\(|:)|>|\x60{3}|~{3})")
	assistantMarkdown    = goldmark.New(goldmark.WithParserOptions(parser.WithBlockParsers(
		util.Prioritized(previewParser{}, 850),
	))).Parser()
)

// ReadableAssistantText projects the reply for a text channel. Use the Markdown
// parser for block ownership so literal syntax in samples stays intact.
func ReadableAssistantText(source string, receiving bool, placeholder string) string {
	if !strings.Contains(source, "<a2") {
		return source
	}
	input := []byte(source)
	// Goldmark handles CRLF as-is; normalize lone CR without moving offsets.
	for i, b := range input {
		if b == '\r' && (i+1 == len(input) || input[i+1] != '\n') {
			input[i] = '\n'
		}
	}
	root := assistantMarkdown.Parse(text.NewReader(input))
	var prose strings.Builder
	end := 0
	for child := root.FirstChild(); child != nil; child = child.NextSibling() {
		if block, ok := child.(*previewBlock); ok {
			prose.WriteString(source[end:block.start])
			end = block.end
		}
	}
	if end == 0 {
		return source
	}
	prose.WriteString(source[end:])
	if body := strings.Trim(prose.String(), "\r\n"); strings.TrimSpace(body) != "" {
		return body
	}
	if receiving {
		return ""
	}
	return placeholder
}

type previewBlock struct {
	ast.BaseBlock
	start, end int
}

func (*previewBlock) Kind() ast.NodeKind { return assistantPreviewKind }
func (*previewBlock) IsRaw() bool        { return true }
func (b *previewBlock) Dump(source []byte, level int) {
	ast.DumpHelper(b, source, level, nil, nil)
}

type previewParser struct{}

func (previewParser) Trigger() []byte                             { return []byte{'<'} }
func (previewParser) CanInterruptParagraph() bool                 { return false }
func (previewParser) CanAcceptIndentedLine() bool                 { return false }
func (previewParser) Close(ast.Node, text.Reader, parser.Context) {}

func (previewParser) Open(parent ast.Node, reader text.Reader, _ parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	if parent.Kind() != ast.KindDocument || reader.LineOffset() != 0 {
		return nil, parser.NoChildren
	}
	end := segment.Stop
	switch {
	case string(bytes.TrimRight(line, " \t\r\n")) == assistantPreviewTag:
		end = previewEnd(reader.Source(), end)
	case segment.Stop == len(reader.Source()) && len(line) >= 3 && bytes.HasPrefix([]byte(assistantPreviewTag), line):
		// A streamed prefix at EOF belongs to the preview.
	default:
		return nil, parser.NoChildren
	}
	reader.AdvanceToEOL()
	return &previewBlock{start: segment.Start, end: end}, parser.NoChildren
}

func (previewParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	_, segment := reader.PeekLine()
	if segment.Start >= node.(*previewBlock).end {
		return parser.Close
	}
	reader.AdvanceToEOL()
	return parser.Continue | parser.NoChildren
}

// Match the desktop envelope. A complete block owns every line up to its
// close. A following opener or explicit Markdown boundary can recover a
// truncated block without interpreting any of its DSL.
func previewEnd(source []byte, start int) int {
	recovery := len(source)
	blank := false
	for start < len(source) {
		line, rest, found := bytes.Cut(source[start:], []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		trimmed := bytes.TrimRight(line, " \t")
		switch string(trimmed) {
		case assistantPreviewTag:
			return min(recovery, start)
		case "</a2ui>":
			return start + len(line)
		}
		if blank && markdownAfterPreview.Match(line) {
			recovery = min(recovery, start)
		}
		blank = len(trimmed) == 0
		if !found {
			break
		}
		start = len(source) - len(rest)
	}
	return recovery
}
