package weixin

import (
	"strings"
	"testing"
)

func TestChineseReplyBelowClientLengthIsOneMessage(t *testing.T) {
	text := strings.Repeat("中", 1801)
	parts := chunks(text)
	if len(parts) != 1 || parts[0] != text {
		t.Fatalf("short Chinese reply fragmented: %d", len(parts))
	}
}

func TestMarkdownChunksKeepCompleteJSONFence(t *testing.T) {
	intro := "可以按下面几个步骤完成：\n\n" + strings.Repeat("- [ ] 每完成一步，记录可观察的结果。\n", 105) + "\n一个简单的数据示例：\n\n"
	code := "```json\n{\n  \"id\": \"reading-001\",\n  \"title\": \"关于专注与日常工作的思考\",\n  \"url\": \"https://example.com/articles/focus\",\n  \"note\": \"下次开始工作前，先写清楚这一小时准备完成什么。\",\n  \"tags\": [\"工作方法\", \"个人记录\"]\n}\n```\n\n"
	body := intro + code + strings.Repeat("这是后续说明。", 32)
	parts := chunks(body)
	if len(parts) < 2 || len(parts) > 3 || strings.Join(parts, "") != body {
		t.Fatalf("Markdown source lost or over-fragmented: %d parts, %d units", len(parts), textUnits(body))
	}
	for _, part := range parts {
		if !fitsText(part) || strings.Count(part, "```")%2 != 0 {
			t.Fatal("oversized or broken fenced message")
		}
	}
	complete := false
	for _, part := range parts {
		if strings.Contains(part, code) {
			complete = true
		}
	}
	if !complete {
		t.Fatal("JSON code block was cut across messages")
	}
}

func TestOversizedFenceReopensAndClosesWithinLimit(t *testing.T) {
	content := strings.Repeat("\"key\": \""+strings.Repeat("中", 130)+"\",\n", 24)
	parts := chunks("```json\n" + content + "```\n")
	if len(parts) < 2 {
		t.Fatal("oversized code block was not split")
	}
	for _, part := range parts {
		if !fitsText(part) || !strings.HasPrefix(part, "```json\n") || !strings.HasSuffix(part, "```\n") || strings.Count(part, "```") != 2 {
			t.Fatal("code continuation lost its fence")
		}
	}
}

func TestOversizedFenceKeepsIndentedClosingLine(t *testing.T) {
	parts := chunks("  ~~~json\n" + strings.Repeat("中", 2400) + "\n  ~~~   \n")
	if len(parts) < 2 || !strings.HasSuffix(parts[len(parts)-1], "  ~~~   \n") {
		t.Fatal("original closing fence was lost")
	}
	for _, part := range parts {
		if !fitsText(part) || !strings.HasPrefix(part, "  ~~~json\n") {
			t.Fatal("code continuation exceeded the limit or lost its opening fence")
		}
	}
}

func TestLongPlainLineKeepsUnicodeAndNaturalBoundary(t *testing.T) {
	body := strings.Repeat("这是完整的一句话。", 400)
	parts := chunks(body)
	if len(parts) < 2 || strings.Join(parts, "") != body {
		t.Fatal("long plain text lost source")
	}
	for _, part := range parts {
		if !fitsText(part) || !strings.HasSuffix(part, "。") {
			t.Fatal("plain text split inside a sentence or exceeded limit")
		}
	}
}
