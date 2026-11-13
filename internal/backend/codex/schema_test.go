package codex

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSchemaManifest(t *testing.T) {
	var manifest struct {
		Codex string
		Files map[string]string
	}
	if err := json.Unmarshal(schemaManifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Codex == "" || TestedVersion != manifest.Codex {
		t.Fatal("missing embedded schema baseline")
	}
	for _, file := range []string{"v1/InitializeParams.json", "v2/ThreadStartParams.json", "CommandExecutionRequestApprovalParams.json"} {
		if manifest.Files[file] == "" {
			t.Fatalf("missing native schema: %s", file)
		}
	}
	for file := range manifest.Files {
		b, err := os.ReadFile(filepath.Join("schema", file))
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != manifest.Files[file] {
			t.Fatalf("native schema changed: %s", file)
		}
	}
}
