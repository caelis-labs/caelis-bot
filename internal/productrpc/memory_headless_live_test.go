package productrpc

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type memoryHeadlessFixture struct {
	headlessFixture
	Stage                         string
	SourceBotID, SourceGeneration string
	SourceNative                  memoryNativeIdentity
}

type memoryNativeIdentity struct {
	SessionDigest, LogicalDigest, ProfileBotID string
	NoWorkers, ImportVerified                  bool
}

// This opt-in runs against an explicitly prepared synthetic-only fixture. The
// source run is stopped first, then the operator exports/copies/imports memory
// into an absent target profile and prepares a second fixture. No Runtime state,
// native session binding or target-generated credentials may be copied.
func TestNativeStoppedMemoryHeadless(t *testing.T) {
	path := os.Getenv("CAELIS_BOT_MEMORY_HEADLESS_FIXTURE")
	if path == "" {
		t.Skip("set isolated synthetic stopped-memory fixture metadata")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f memoryHeadlessFixture
	if json.Unmarshal(body, &f) != nil || !f.Synthetic || f.NativeVersion != "0.65.0" ||
		!regexp.MustCompile(`^/tmp/caelis-bot-issue47-product-[a-f0-9]{16}$`).MatchString(f.Root) ||
		!regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`).MatchString(f.Target) ||
		(f.Stage != "source" && f.Stage != "target") {
		t.Fatal("invalid isolated memory fixture metadata")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	c := connectHeadlessFixture(t, ctx, f.headlessFixture)
	state := waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.Connection == "ready" && s.Snapshot.CanSend })
	if state.Initialization.Status != "accepted" || state.Initialization.Required || len(state.TaskSummaries) != 0 {
		t.Fatal("memory activation lost accepted introduction or imported task execution")
	}
	identity := inspectMemoryNative(t, ctx, f.headlessFixture)
	if identity.ProfileBotID != state.BotID || !identity.ImportVerified || !identity.NoWorkers {
		t.Fatal("native identity/import ownership mismatch")
	}
	if f.Stage == "target" {
		if state.BotID != f.SourceBotID || state.Generation == f.SourceGeneration ||
			identity.LogicalDigest != f.SourceNative.LogicalDigest || identity.SessionDigest == f.SourceNative.SessionDigest {
			t.Fatal("target did not retain logical Bot identity in a new native session")
		}
		if receipt, err := c.Receipt(ctx, "memory-source-submit"); err != nil || receipt.Outcome != "unknown" || receipt.Code != "receipt-unavailable" || receipt.Submission != nil {
			t.Fatal("old product execution receipt migrated")
		}
		for _, item := range state.Snapshot.Items {
			if strings.Contains(item.Text, "CASE_PRODUCT_MEMORY_SOURCE") {
				t.Fatal("source native conversation migrated")
			}
		}
	}
	key := "CASE_PRODUCT_MEMORY_" + strings.ToUpper(f.Stage)
	id := "memory-" + f.Stage + "-submit"
	receipt, err := c.Command(ctx, Command{ID: id, Kind: "submit", Submission: &api.Submission{ID: id, Text: key}})
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal("synthetic native submission failed", receipt.Outcome, err)
	}
	waitHeadlessState(t, ctx, c, func(s State) bool {
		for _, item := range s.Snapshot.Items {
			if item.Kind == "assistant" && strings.Contains(item.Text, "PRODUCT_NATIVE_COMPLETE") {
				return s.Snapshot.CanSend
			}
		}
		return false
	})
	status := nativeFixtureControl(t, ctx, f.headlessFixture, "status")
	if status.Counts[key] != 1 || status.Counts["introduction"] != 0 || (f.Stage == "target" && status.Counts["CASE_PRODUCT_MEMORY_SOURCE"] != 0) {
		t.Fatal("synthetic invocation or introduction replay mismatch")
	}
	stop, err := c.Command(ctx, Command{ID: "memory-" + f.Stage + "-stop", Kind: "stop-bot"})
	if err != nil || stop.Outcome != "accepted" {
		t.Fatal("explicit source/target stop failed", stop.Outcome, err)
	}
	for {
		status = nativeFixtureControl(t, ctx, f.headlessFixture, "status")
		if !status.NodeAlive && !status.ListenerAlive {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("stopped memory owner remained active")
		}
	}
	// The owner has exited. This helper uses public BotMemory Recall and public
	// Memory TraceReceipt; it cannot open the live owner or bypass its lock.
	verify := exec.CommandContext(ctx, "ssh", fixtureSSHArgs(f.Target, quoteFixture(f.Root+"/memorycheck")+" verify-started "+quoteFixture(f.Root+"/profile"))...)
	verify.Stderr = os.Stderr
	if err := verify.Run(); err != nil {
		t.Fatal("stopped corrected/forgotten receipts, tombstones or Notebook failed", err)
	}
	_ = nativeFixtureControl(t, ctx, f.headlessFixture, "stop")
	if evidence := os.Getenv("CAELIS_BOT_MEMORY_NATIVE_EVIDENCE"); evidence != "" {
		result, err := json.Marshal(struct {
			Stage                        string
			BotID, Generation            string
			Native                       memoryNativeIdentity
			MemoryVerified, OwnerStopped bool
		}{f.Stage, state.BotID, state.Generation, identity, true, true})
		if err != nil || os.WriteFile(evidence, result, 0600) != nil {
			t.Fatal("could not retain isolated memory acceptance evidence")
		}
	}
}

func inspectMemoryNative(t *testing.T, ctx context.Context, f headlessFixture) memoryNativeIdentity {
	t.Helper()
	// Only this fresh synthetic fixture's binding is read on the target. Return
	// hashes and booleans, never connection credentials or native session IDs.
	script := `import hashlib,json,pathlib,sys
r=pathlib.Path(sys.argv[1]);p=r/'profile'
b=json.loads((p/'bot.json').read_text());n=json.loads((p/'providers/caelis/application.json').read_text())
sid=n['session']['session_id'];assert sid and b['id']
print(json.dumps({'SessionDigest':hashlib.sha256(sid.encode()).hexdigest(),'LogicalDigest':hashlib.sha256(b['id'].encode()).hexdigest(),'ProfileBotID':'bot-'+hashlib.sha256(('caelis-product-bot\x00'+b['id']).encode()).hexdigest(),'NoWorkers':not n.get('workers'),'ImportVerified':json.loads((r/'memory-import-verified.json').read_text())['verified']}))
`
	command := "python3 -c " + quoteFixture(script) + " " + quoteFixture(f.Root)
	cmd := exec.CommandContext(ctx, "ssh", fixtureSSHArgs(f.Target, command)...)
	cmd.Stderr = os.Stderr
	body, err := cmd.Output()
	if err != nil {
		t.Fatal("isolated native identity unavailable", err)
	}
	var out memoryNativeIdentity
	if json.Unmarshal(body, &out) != nil || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(out.SessionDigest) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(out.LogicalDigest) {
		t.Fatal("invalid native identity projection")
	}
	return out
}
