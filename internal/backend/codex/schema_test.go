package codex

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSchemaAndTestBaselineAgree(t *testing.T) {
	read := func(path string, value any) {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, value); err != nil {
			t.Fatal(err)
		}
	}
	var manifest struct {
		Codex string
		Files map[string]string
	}
	var toolchain struct{ Codex string }
	read("schema/manifest.json", &manifest)
	read("../../../toolchain.json", &toolchain)
	if manifest.Codex != TestedVersion || toolchain.Codex != TestedVersion {
		t.Fatal("schema / toolchain / test baseline mismatch")
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
