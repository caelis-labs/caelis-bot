package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
)

// Local bootstrap fixture: neither SSH authentication nor a native model is used.
func TestNativeWorkerFactorySelectsExplicitCaelisScopeWithoutActivatingBot(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "legacy"
		expected := caelis.WorkerProtocolSharedNative
		if explicit {
			name, expected = "explicit", caelis.WorkerProtocolBoundedApplication
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			request := filepath.Join(root, "request.json")
			// The system ssh client is replaced only for this test. Capture its actual
			// bounded stdin request, then return read-only public probe facts.
			script := "#!/bin/sh\ncat > '" + request + "'\nprintf '%s' '{\"os\":\"linux\",\"arch\":\"amd64\"}'\n"
			if err := os.WriteFile(filepath.Join(root, "ssh"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
			engine := &nodeSourceEngine{testEngine: newTestEngine()}
			app := &Application{engine: engine}
			config := nodeConfig()
			if explicit {
				config.Backend = "caelis"
			}
			adapter, err := app.newWorkerNodeAdapter(config, filepath.Join(root, "private"))
			if err != nil {
				t.Fatal(err)
			}
			defer adapter.Close(t.Context())
			if _, err = adapter.Probe(t.Context()); err != nil {
				t.Fatal(err)
			}
			bytes, err := os.ReadFile(request)
			if err != nil {
				t.Fatal(err)
			}
			var sent caelis.WorkerBootstrapRequest
			if err = json.Unmarshal(bytes, &sent); err != nil {
				t.Fatal(err)
			}
			if sent.Protocol != expected || sent.Action != "probe" || sent.OperationID != "" || sent.AppCredential != "" || engine.sources != 0 {
				t.Fatal("setup changed scope, enrolled, or fabricated native activation", sent.Protocol, sent.Action)
			}
			config.Backend = "codex"
			if _, err = app.newWorkerNodeAdapter(config, filepath.Join(root, "unsupported")); err == nil {
				t.Fatal("unavailable Codex bridge accepted on Stage 1")
			}
		})
	}
}
