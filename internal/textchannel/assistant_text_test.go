package textchannel

import (
	"strings"
	"testing"
)

func TestReadableAssistantText(t *testing.T) {
	const block = "<a2ui>\nroot = Text(\"desktop\")\n</a2ui>"
	const hint = "Open chat"
	example := "```text\n" + block + "\n```"
	indented := "    " + strings.ReplaceAll(block, "\n", "\n    ")
	quote := "> " + strings.ReplaceAll(block, "\n", "\n> ")
	for _, tc := range []struct {
		name, input, want string
		receiving         bool
	}{
		{"complete", "Before\n\n" + block + " \t\n\nAfter", "Before\n\n\n\nAfter", false},
		{"partial", "Before\n\n" + strings.TrimSuffix(block, "</a2ui>"), "Before", true},
		{"partial marker", "<a2u", "", true},
		{"desktop-only", block, hint, false},
		{"desktop-only streaming", block, "", true},
		{"CRLF", strings.ReplaceAll(block, "\n", "\r\n"), hint, false},
		{"lone CR", strings.ReplaceAll(block, "\n", "\r"), hint, false},
		{"literal examples", example + "\n\n" + indented + "\n\n" + quote + "\n\n`<a2ui>`", example + "\n\n" + indented + "\n\n" + quote + "\n\n`<a2ui>`", false},
		{"preserve indentation", indented + "\n\n" + block, indented, false},
		{"recover heading", strings.TrimSuffix(block, "</a2ui>") + "\n## Keep this\n\n- And this", "## Keep this\n\n- And this", false},
		{"recover next block", strings.TrimSuffix(block, "</a2ui>") + block + "\n\nAfter", "After", true},
		{"complete malformed", "<a2ui>\ninvalid DSL\n\n## Still inside\n</a2ui>\n\nAfter", "After", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReadableAssistantText(tc.input, tc.receiving, hint); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
