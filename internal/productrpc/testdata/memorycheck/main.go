package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botmemory"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
	owner "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	mem "github.com/caelis-labs/memory/api/memory/v1alpha1"
	"github.com/caelis-labs/memory/appliance"
)

const correctedText = "Synthetic fixture prefers tea in morning."
const forgottenText = "Synthetic forgotten receipt."
const forgottenReplacementText = "Synthetic forgotten replacement."

type expected struct {
	BotID             string
	CorrectedOriginal string
	Forgotten         []string
}
type syntheticEngine struct{}

func (syntheticEngine) Snapshot() api.Snapshot { return api.Snapshot{CanSend: true} }
func (syntheticEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}
func write(root, p string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, p), body, 0600)
}
func main() {
	if len(os.Args) != 3 {
		panic("fixture seed|verify|verify-started ABS_PROFILE")
	}
	ctx := context.Background()
	var err error
	switch os.Args[1] {
	case "seed":
		err = seed(ctx, os.Args[2])
	case "verify":
		err = verify(ctx, os.Args[2], false)
	case "verify-started":
		err = verify(ctx, os.Args[2], true)
	default:
		err = errors.New("unknown mode")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func seed(ctx context.Context, root string) error {
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		return errors.New("seed profile must be absent")
	}
	runtime, err := bot.New(filepath.Join(root, "bot.json"), nil)
	if err != nil {
		return err
	}
	id := runtime.State().ID
	intro, err := bot.OpenInitializer(filepath.Join(root, "bot-initialization.json"))
	if err != nil {
		return err
	}
	if _, err = intro.Initialize(ctx, api.BotIntroduction{Name: "Synthetic Oak", Description: "Offline transfer acceptance only."}); err != nil {
		return err
	}
	if err = intro.Deliver(ctx, syntheticEngine{}, "fixture"); err != nil {
		return err
	}
	if intro.Initialization().Status != "accepted" {
		return errors.New("synthetic public introduction workflow did not accept")
	}
	store, err := botmemory.Open(ctx, filepath.Join(root, "personal"), id)
	if err != nil {
		return err
	}
	defer store.Close()
	old, err := store.Remember(ctx, "fixture-remember-coffee", "Synthetic fixture prefers coffee in morning.", "settings")
	if err != nil {
		return err
	}
	if err = store.CorrectMemory(ctx, api.MemoryChange{RequestID: "fixture-correct-coffee", ID: old.ID, Text: correctedText}); err != nil {
		return err
	}
	forgotten, err := store.Remember(ctx, "fixture-remember-private", forgottenText, "settings")
	if err != nil {
		return err
	}
	if err = store.CorrectMemory(ctx, api.MemoryChange{RequestID: "fixture-correct-private", ID: forgotten.ID, Text: forgottenReplacementText}); err != nil {
		return err
	}
	evidence, err := store.ReadMemory(ctx, "")
	if err != nil {
		return err
	}
	var replacement string
	for _, entry := range evidence.Evidence {
		if entry.Text == forgottenReplacementText {
			replacement = entry.ID
		}
	}
	if replacement == "" {
		return errors.New("forgotten replacement missing")
	}
	if err = store.ForgetMemory(ctx, api.MemoryChange{RequestID: "fixture-forget-private", ID: replacement}); err != nil {
		return err
	}
	if err = store.Close(); err != nil {
		return err
	}
	vault, err := notebook.OpenVault(filepath.Join(root, "Notebook"))
	if err != nil {
		return err
	}
	if err = vault.Close(); err != nil {
		return err
	}
	if err = write(root, "Notebook/MEMORY.md", []byte("# Memory\n\nSynthetic Oak is an offline acceptance fixture.\n")); err != nil {
		return err
	}
	body, err := json.Marshal(expected{BotID: id, CorrectedOriginal: old.ID, Forgotten: []string{forgotten.ID, replacement}})
	if err != nil {
		return err
	}
	if err = write(root, "Notebook/receipt-checks.md", append([]byte("# Synthetic receipt expectations\n\n"), body...)); err != nil {
		return err
	}
	if err = write(root, "Notebook/2026/09/30/note.md", []byte("# Synthetic authored note\n\nThis body survives the round trip.\n\n![Diagram](../../../assets/diagram.png)\n")); err != nil {
		return err
	}
	if err = write(root, "Notebook/assets/diagram.png", []byte("synthetic nonsecret attachment")); err != nil {
		return err
	}
	if err = localstate.Write(filepath.Join(root, "notebook-migration.json"), struct {
		Version int `json:"version"`
	}{1}); err != nil {
		return err
	}
	fmt.Println(`{"fixture_seeded":true,"models_started":false,"bot_started":false,"stores_closed":true}`)
	return nil
}
func verify(ctx context.Context, root string, started bool) error {
	body, err := os.ReadFile(filepath.Join(root, "Notebook", "receipt-checks.md"))
	if err != nil {
		return err
	}
	_, raw, ok := strings.Cut(string(body), "\n\n")
	if !ok {
		return errors.New("expected fixture note missing")
	}
	var want expected
	if err = json.Unmarshal([]byte(raw), &want); err != nil {
		return err
	}
	runtime, err := bot.New(filepath.Join(root, "bot.json"), nil)
	if err != nil {
		return err
	}
	state := runtime.State()
	if state.ID != want.BotID || state.Version != 1 || state.PersonalVersion != 1 || len(state.Schedules) != 0 || state.Wake != nil {
		return errors.New("identity/schedule/Wake mismatch")
	}
	intro, err := bot.OpenInitializer(filepath.Join(root, "bot-initialization.json"))
	if err != nil {
		return err
	}
	if intro.Initialization().Status != "accepted" {
		return errors.New("accepted synthetic introduction missing")
	}
	for _, p := range []string{"tasks.json", "conversation.json", "providers", "binding.json", "wake.json"} {
		if started {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, p)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("execution state unexpectedly migrated: %s", p)
		}
	}
	store, err := botmemory.Open(ctx, filepath.Join(root, "personal"), want.BotID)
	if err != nil {
		return err
	}
	defer store.Close()
	evidence, err := store.ReadMemory(ctx, "")
	if err != nil || len(evidence.Evidence) != 1 || evidence.Evidence[0].Text != correctedText {
		return errors.New("authoritative evidence mismatch")
	}
	evidence, err = store.ReadMemory(ctx, "tea")
	if err != nil || len(evidence.Evidence) != 1 || evidence.Evidence[0].Text != correctedText {
		return errors.New("corrected public Recall mismatch")
	}
	evidence, err = store.ReadMemory(ctx, "forgotten")
	if err != nil || len(evidence.Evidence) != 0 {
		return errors.New("forgotten content recalled")
	}
	if _, err = store.Remember(ctx, "fixture-remember-private", forgottenText, "settings"); err == nil {
		return errors.New("forgotten receipt replay resurrected")
	}
	if _, err = store.Remember(ctx, "fixture-remember-coffee", "Synthetic fixture prefers coffee in morning.", "settings"); err == nil {
		return errors.New("corrected receipt replay resurrected")
	}
	if err = store.Close(); err != nil {
		return err
	}
	memory, err := appliance.Open(ctx, appliance.Options{DataDir: filepath.Join(root, "personal", "memory")})
	if err != nil {
		return err
	}
	defer memory.Close()
	trace, err := memory.Management().TraceReceipt(ctx, owner.TraceReceiptRequest{ReceiptID: mem.ReceiptID(want.CorrectedOriginal)})
	if err != nil || trace.State != owner.ReceiptStateCorrected || trace.Receipt == nil || trace.Receipt.CorrectedBy == "" {
		return errors.New("correction chain mismatch")
	}
	for _, id := range want.Forgotten {
		trace, err := memory.Management().TraceReceipt(ctx, owner.TraceReceiptRequest{ReceiptID: mem.ReceiptID(id)})
		if err != nil || trace.State != owner.ReceiptStateDeleted || trace.Receipt != nil || trace.Tombstone == nil {
			return errors.New("forgotten tombstone mismatch")
		}
	}
	if err = memory.Close(); err != nil {
		return err
	}
	note, err := os.ReadFile(filepath.Join(root, "Notebook", "2026", "09", "30", "note.md"))
	if err != nil || !strings.Contains(string(note), "This body survives the round trip.") {
		return errors.New("authored note missing")
	}
	index, err := os.ReadFile(filepath.Join(root, "Notebook", "INDEX.md"))
	if err != nil || !strings.Contains(string(index), "Synthetic authored note") {
		return errors.New("INDEX was not regenerated")
	}
	attachment, err := os.ReadFile(filepath.Join(root, "Notebook", "assets", "diagram.png"))
	if err != nil || string(attachment) != "synthetic nonsecret attachment" {
		return errors.New("allowlisted attachment missing")
	}
	fmt.Println(`{"fixture_verified":true,"same_bot_identity":true,"corrected_recall":true,"forgotten_recall_empty":true,"both_tombstones":true,"replay_cannot_resurrect":true,"authored_notebook":true,"index_regenerated":true,"accepted_synthetic_introduction":true,"stopped_memory_verified":true,"stores_closed":true}`)
	return nil
}
