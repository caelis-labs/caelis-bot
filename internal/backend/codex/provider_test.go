package codex

import (
	"context"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
)

func TestProviderRejectsInvalidExecutionBeforeConnecting(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir(), Execution: api.ExecutionSettings{ApprovalMode: "foreign-policy"}})
	called := false
	s.start = func(context.Context, Options) (*Client, error) { called = true; return nil, nil }
	if err := s.Connect(context.Background()); err == nil || called {
		t.Fatal("invalid policy reached a native connection")
	}
}
func TestToolGrantIsCopiedAndTranslatedOnlyIntoNamedApprovals(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	config := &api.ToolConnection{Command: "synthetic", Args: []string{"--bot-tools"}, Env: map[string]string{"credential": "synthetic"}, ApprovedTools: botpolicy.ApprovedTools()}
	if err := s.ConfigureBotTools(config); err != nil {
		t.Fatal(err)
	}
	config.Env["credential"] = "changed"
	config.ApprovedTools[0] = "foreign"
	config.Args[0] = "changed"
	p := s.connectionParams()["config"].(map[string]any)["mcp_servers.caelis_bot"].(map[string]any)
	if _, ok := p["default_tools_approval_mode"]; ok {
		t.Fatal("server-wide approval introduced")
	}
	policy := p["tools"].(map[string]any)
	if len(policy) != 8 || policy["foreign"] != nil || policy["bot_clock"] == nil {
		t.Fatal("grant alias or scope leak")
	}
	if p["env"].(map[string]string)["credential"] != "synthetic" || p["args"].([]string)[0] != "--bot-tools" {
		t.Fatal("host mutation changed bound credentials")
	}
}
func TestUnknownStateCannotReplaceRuntime(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	s.state.Phase = "unknown"
	saved := false
	if _, err := s.ChangeRuntime(context.Background(), api.RuntimeSettings{Runtime: "codex"}, func() error { saved = true; return nil }); err == nil || saved {
		t.Fatal("unknown outcome permitted runtime replacement")
	}
}
