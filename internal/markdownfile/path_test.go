package markdownfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func TestMarkdownLinkTargetKeepsNativeWindowsSeparators(t *testing.T) {
	const nativePath = `C:\Users\RUNNER~1\Notebook\.agents\skills\bot-core\SKILL.md:47`
	const displayedPath = `C:\Users\RUNNER~1\Notebook.agents\skills\bot-core\SKILL.md:47`
	source := []byte("[SKILL.md:47](" + nativePath + ")")
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	link, ok := document.FirstChild().FirstChild().(*ast.Link)
	if !ok {
		t.Fatal("Markdown did not parse the citation")
	}
	destination, file := LinkTarget(link.Destination)
	if destination != displayedPath || file != nativePath {
		t.Fatalf("renderer received %q, filesystem received %q", destination, file)
	}
	const slashStyle = `C:/Project/report\(draft\).md`
	destination, file = LinkTarget([]byte(slashStyle))
	if destination != "C:/Project/report(draft).md" || file != destination {
		t.Fatalf("slash-style Markdown escapes became filesystem separators: %q and %q", destination, file)
	}
}

func TestFileURIPathAcceptsOnlyLocalWindowsDriveForms(t *testing.T) {
	for _, tc := range []struct {
		destination string
		windows     bool
		want        string
	}{
		{"file:///C:/目录/设计%20图.png", true, "C:/目录/设计 图.png"},
		{"file://C:/目录/设计%20图.png", true, "C:/目录/设计 图.png"},
		{"file://d:/Notebook/SKILL.md:47", true, "d:/Notebook/SKILL.md:47"},
		{"file://server/share/file.png", true, ""},
		{"file://C:80/Notebook/SKILL.md", true, ""},
		{"file://user@C:/Notebook/SKILL.md", true, ""},
		{"file:////server/share/file.png", true, ""},
		{"file:///C:/Notebook/SKILL.md?x=47", true, ""},
		{"file:///C:/Notebook/SKILL.md#47", true, ""},
		{"file://C:/Notebook/SKILL.md", false, ""},
		{"file:///tmp/设计%20图.png", false, "/tmp/设计 图.png"},
	} {
		if got := fileURIPath(tc.destination, tc.windows); got != tc.want {
			t.Errorf("fileURIPath(%q, windows=%t) = %q, want %q", tc.destination, tc.windows, got, tc.want)
		}
	}
}

func TestRegularFileResolvesALineOnTheNativeFilesystem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "设计 文档.md")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := path + ":47"
	if got := Path(filepath.ToSlash(destination)); got != destination {
		t.Fatalf("native path conversion = %q, want %q", got, destination)
	}
	if got, ok := RegularFile(destination); !ok || got != path {
		t.Fatalf("line citation resolved to %q, want %q", got, path)
	}
	if _, ok := RegularFile(path + ":0"); ok {
		t.Fatal("invalid line resolved")
	}
	if filepath.Separator == '\\' {
		if ScopePath(destination) != path || ScopePath(path+":stream") != "" || ScopePath(path+":stream:47") != "" {
			t.Fatal("Windows scope check retained a stream suffix")
		}
	} else {
		if err := os.WriteFile(destination, []byte("literal colon filename"), 0600); err != nil {
			t.Fatal(err)
		}
		if got, ok := RegularFile(destination); !ok || got != destination {
			t.Fatalf("literal filename resolved to %q, want %q", got, destination)
		}
	}
}
