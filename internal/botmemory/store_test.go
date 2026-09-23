package botmemory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	facts "github.com/caelis-labs/memory/api/memory/facts/v1alpha1"
	owner "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	mem "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func openTest(t *testing.T, dir string) *Store {
	t.Helper()
	s, e := Open(t.Context(), dir, "stable-personal-bot")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestEvidenceSharedAcrossProvidersAndForgettingSurvivesRetry(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	r, e := s.Remember(t.Context(), "remember-codex-1", "喜欢安静的海边城市。", "codex")
	if e != nil {
		t.Fatal(e)
	}
	same, e := s.Remember(t.Context(), "remember-codex-1", "喜欢安静的海边城市。", "codex")
	if e != nil || same.ID != r.ID {
		t.Fatal("remember retry duplicated", e)
	}
	v, e := s.ReadMemory(t.Context(), "海边")
	if e != nil || len(v.Evidence) != 1 {
		t.Fatal("inference became fact or missing recall", v, e)
	}
	empty, e := s.ReadMemory(t.Context(), "自动复制")
	if e != nil || len(empty.Evidence) != 0 {
		t.Fatal("notebook silently indexed", e)
	}
	s.Close()
	s = openTest(t, dir)
	defer s.Close()
	v, e = s.ReadMemory(t.Context(), "")
	if e != nil || len(v.Evidence) != 1 {
		t.Fatal("new runtime lost shared evidence", v, e)
	}
	if e = s.CorrectMemory(t.Context(), api.MemoryChange{RequestID: "correction-1", ID: r.ID, Text: "更喜欢安静的山间城市。"}); e != nil {
		t.Fatal(e)
	}
	v, e = s.ReadMemory(t.Context(), "海边")
	if e != nil || len(v.Evidence) != 0 {
		t.Fatal("corrected clue recalled", v, e)
	}
	v, e = s.ReadMemory(t.Context(), "山间")
	if e != nil || len(v.Evidence) != 1 {
		t.Fatal(v, e)
	}
	target := v.Evidence[0].ID
	if e = s.ForgetMemory(t.Context(), api.MemoryChange{RequestID: "forget-memory-1", ID: target}); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{r.ID, target} {
		trace, err := s.runtime.Management().TraceReceipt(t.Context(), owner.TraceReceiptRequest{ReceiptID: mem.ReceiptID(id)})
		if err != nil || trace.State != owner.ReceiptStateDeleted || trace.Receipt != nil {
			t.Fatal("forgotten chain retained content", err)
		}
	}
	s.Close()
	s = openTest(t, dir)
	defer s.Close()
	v, e = s.ReadMemory(t.Context(), "山间")
	if e != nil || len(v.Evidence) != 0 {
		t.Fatal("forgotten clue returned", v, e)
	}
	if _, e = s.Remember(t.Context(), "remember-codex-1", "喜欢安静的海边城市。", "codex"); e == nil {
		t.Fatal("old retry claimed active memory")
	}
	b, e := os.ReadFile(filepath.Join(dir, "index.json"))
	if e != nil || strings.Contains(string(b), "海边") || strings.Contains(string(b), "capability") {
		t.Fatal("index copied private evidence or token", e)
	}
}
func TestLegacyProfileOnlyExportsForMigration(t *testing.T) {
	s := openTest(t, t.TempDir())
	defer s.Close()
	auth, err := s.Authorization(t.Context(), mem.OperationRemember)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.runtime.Evidence().SubmitEvidence(t.Context(), auth, facts.SubmitEvidenceRequest{Source: facts.Source{Producer: "caelis-bot-settings", EventID: "legacy-event", Revision: "1", Fragment: "0", Subject: "user", Role: facts.RoleConfirmation, FactKey: "name"}, Text: "LegacyName", IdempotencyKey: "legacy-profile", Mutations: []facts.Mutation{{Transition: facts.TransitionEstablish, Subject: "user", Key: "name", Text: "LegacyName"}}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.LegacyProfile(t.Context())
	if err != nil || !strings.Contains(profile, "LegacyName") {
		t.Fatal("legacy data not exported", err)
	}
	recall, err := s.ReadMemory(t.Context(), "LegacyName")
	if err != nil || len(recall.Evidence) != 0 {
		t.Fatal("legacy profile reintroduced as current truth", err)
	}
}
func TestScopeAndCredentialProjection(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	_, e := s.Remember(t.Context(), "receipt-check-1", "可检索资料", "generic-runtime")
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.ReadMemory(t.Context(), "")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	for _, secret := range []string{s.issuer, string(s.capability.Token), s.scope} {
		if secret != "" && strings.Contains(string(b), secret) {
			t.Fatal("private authority projected")
		}
	}
	s.Close()
	if other, e := Open(t.Context(), dir, "other-bot"); e == nil {
		other.Close()
		t.Fatal("another identity adopted store")
	}
}
