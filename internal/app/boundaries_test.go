package app

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Check all source files, including inactive platform build tags. This catches
// a boundary leak even when the current workstation cannot compile that host.
func TestProductDependencyDirection(t *testing.T) {
	root := filepath.Join("..", "..")
	rules := map[string][]string{
		"desktop":       {"/internal/backend/codex", "/internal/backend/caelis", "/internal/bot"},
		"backend":       {"/internal/app", "/internal/desktop", "/internal/backend/codex", "/internal/backend/caelis"},
		"backend/api":   {"/internal/app", "/internal/desktop", "/internal/bot", "/internal/backend/codex", "/internal/backend/caelis"},
		"backend/codex": {"/internal/app", "/internal/desktop", "/internal/bot"},
		"bot":           {"/internal/app", "/internal/desktop", "/internal/backend/codex", "/internal/backend/caelis"},
	}
	for dir, forbidden := range rules {
		directory := filepath.Join(root, "internal", dir)
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			filename := filepath.Join(directory, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				for _, denied := range forbidden {
					if path == "github.com/caelis-labs/caelis-bot"+denied {
						t.Errorf("%s imports forbidden owner %s", filename, path)
					}
				}
				if strings.HasPrefix(path, "github.com/caelis-labs/caelis/internal/") {
					t.Errorf("private sibling import: %s", path)
				}
			}
		}
	}
}
