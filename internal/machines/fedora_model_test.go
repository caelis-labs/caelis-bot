package machines

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Opt-in metadata/settings acceptance. No inference, Worker or global runtime
// configuration write. A named disposable controller may be retained for UI QA.
func TestFedoraModelSettings(t *testing.T) {
	host := os.Getenv("CAELIS_BOT_TEST_MODEL_SSH")
	if host == "" {
		t.Skip("set CAELIS_BOT_TEST_MODEL_SSH for remote settings acceptance")
	}
	root := os.Getenv("CAELIS_BOT_TEST_MODEL_STORE")
	if root == "" {
		root = t.TempDir()
	}
	helper := os.Getenv("CAELIS_BOT_REMOTE_HELPER_DIR")
	s, err := Open(root, inert{}, func(arch string) ([]byte, error) { return os.ReadFile(filepath.Join(helper, "linux-"+arch)) })
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Machines()) != 0 {
		t.Fatal("acceptance requires an empty disposable controller")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	for _, runtime := range []string{"codex", "caelis"} {
		t.Run(runtime, func(t *testing.T) {
			input := api.MachineInput{Address: host, Port: 22, Authentication: "agent", Name: "Fedora " + map[string]string{"codex": "Codex", "caelis": "Caelis"}[runtime]}
			node, err := s.ConnectMachine(ctx, input)
			if err != nil || node.State != "trust" {
				t.Fatalf("trust preparation: %s %s %v", node.State, node.Issue, err)
			}
			input.ID, input.TrustFingerprint = node.ID, node.Fingerprint
			node, err = s.ConnectMachine(ctx, input)
			if err != nil || node.State == "offline" {
				t.Fatalf("connect: %s %s %v", node.State, node.Issue, err)
			}
			node, err = s.InspectMachine(ctx, node.ID, runtime)
			if err != nil || node.State != "ready" || len(node.Models) == 0 {
				t.Fatalf("metadata: %s %s models=%d %v", node.State, node.Issue, len(node.Models), err)
			}
			choice := node.Models[0]
			for _, model := range node.Models {
				if model.Default {
					choice = model
					break
				}
			}
			selection := api.WorkExecutionSettings{Model: choice.Model, Effort: choice.DefaultEffort}
			if len(choice.Efforts) > 0 {
				selection.Effort = choice.Efforts[len(choice.Efforts)-1]
			}
			for _, tier := range choice.ServiceTiers {
				if tier.Name == "Fast" || tier.ID == "fast" {
					selection.ServiceTier = tier.ID
					break
				}
			}
			saved, err := s.SaveMachineModel(ctx, api.MachineModel{ID: node.ID, Selection: selection})
			if err != nil || saved.Work != selection {
				t.Fatalf("parameter readback: %+v %v", saved.Work, err)
			}
			if saved.RuntimeDefault == nil != (node.RuntimeDefault == nil) || saved.RuntimeDefault != nil && *saved.RuntimeDefault != *node.RuntimeDefault {
				t.Fatal("node selection changed runtime default")
			}
			reopened, err := Open(root, inert{}, s.artifact)
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := reopened.InspectMachine(ctx, node.ID, runtime)
			if err != nil || persisted.Work != selection {
				t.Fatal("parameters lost after reconnect", err)
			}
			reset, err := s.SaveMachineModel(ctx, api.MachineModel{ID: node.ID})
			if err != nil || reset.Work != (api.WorkExecutionSettings{}) {
				t.Fatal("default reset failed", err)
			}
			t.Logf("runtime=%s models=%d defaultReadable=%t effort=%s fast=%t; save/reconnect/reset passed", runtime, len(node.Models), node.RuntimeDefault != nil, selection.Effort, selection.ServiceTier != "")
		})
	}
}
