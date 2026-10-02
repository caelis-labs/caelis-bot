package codex

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// Exercise the new product port against the real Codex adapter and its native
// protocol fixture. No Codex executable, user credentials or model is used.
func TestProductExecutionCodexCatalogAndNextRequestPreservePolicy(t *testing.T) {
	session, fixture := sessionPair(t, "")
	entry := catalogEntry()
	entry["supportedReasoningEfforts"] = []any{map[string]any{"reasoningEffort": "high"}, map[string]any{"reasoningEffort": "low"}}
	fixture.mu.Lock()
	fixture.modelPages = map[string]any{"": map[string]any{"data": []any{entry}}}
	fixture.mu.Unlock()
	root := t.TempDir()
	service := backend.NewService(session, nil, nil, nil, nil)
	service.ConfigureExecution(filepath.Join(root, "execution.json"), api.ExecutionSettings{Model: "test-model", Effort: "high", ServiceTier: "fast", ApprovalMode: "ask"})
	service.ConfigureWorkExecution(filepath.Join(root, "work.json"), api.WorkExecutionSettings{})
	server, err := productrpc.NewServer(productrpc.ServicePort{Service: service}, productrpc.Options{NodeID: "node", BotID: "bot", Token: strings.Repeat("x", 64), JournalFile: filepath.Join(root, "receipts.json"), Execution: func(scope productmanagement.Scope) (productmanagement.ExecutionPort, error) {
		return productmanagement.NewExecution(scope, service)
	}})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	client, err := productrpc.NewClient(productrpc.ClientOptions{URL: httpServer.URL, ExpectedNode: "node", ExpectedBot: "bot", Token: strings.Repeat("x", 64)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err = client.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	caps, err := client.ManagementCapabilities(testContext(t))
	if err != nil || !caps.Execution || caps.Configuration {
		t.Fatal("Codex depended on Host configuration", caps, err)
	}
	view, err := client.ExecutionSettings(testContext(t))
	if err != nil || len(view.Models) != 1 || view.ConversationDefault || view.Work == nil {
		t.Fatal(view, err)
	}
	workResult, err := client.ChangeExecutionSettings(testContext(t), productmanagement.ExecutionCommand{ID: "codex-work-override", Target: "work", ExpectedRevision: view.Revision, Selection: productmanagement.Selection{Model: "test-model", Effort: "high"}})
	if err != nil || workResult.Outcome != "accepted" {
		t.Fatal(workResult, err)
	}
	view, err = client.ExecutionSettings(testContext(t))
	if err != nil || view.Work == nil || view.Work.Model != "test-model" {
		t.Fatal(view, err)
	}
	workResult, err = client.ChangeExecutionSettings(testContext(t), productmanagement.ExecutionCommand{ID: "codex-work-inherit", Target: "work", ExpectedRevision: view.Revision, Selection: productmanagement.Selection{}})
	if err != nil || workResult.Outcome != "accepted" {
		t.Fatal(workResult, err)
	}
	work, err := backend.LoadWorkExecutionSettings(filepath.Join(root, "work.json"))
	if err != nil || work != (api.WorkExecutionSettings{}) {
		t.Fatal(work, err)
	}
	view, err = client.ExecutionSettings(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ChangeExecutionSettings(testContext(t), productmanagement.ExecutionCommand{ID: "codex-model-only", Target: "conversation", ExpectedRevision: view.Revision, Selection: productmanagement.Selection{Model: "test-model", Effort: "low"}})
	if err != nil || result.Outcome != "accepted" {
		t.Fatal(result, err)
	}
	persisted, err := backend.LoadExecutionSettings(filepath.Join(root, "execution.json"), api.ExecutionSettings{})
	if err != nil || persisted.Effort != "low" || persisted.ServiceTier != "fast" || persisted.ApprovalMode != "ask" {
		t.Fatal(persisted, err)
	}
	receipt, err := session.Submit(testContext(t), api.Submission{ID: "native-request", Text: "synthetic"}, nil)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	fixture.mu.Lock()
	params := fixture.lastParams
	fixture.mu.Unlock()
	for key, want := range map[string]string{"model": `"test-model"`, "effort": `"low"`, "serviceTier": `"fast"`, "approvalPolicy": `"on-request"`, "approvalsReviewer": `"user"`} {
		if string(params[key]) != want {
			t.Fatal("model selection changed native policy", key, string(params[key]))
		}
	}
}
