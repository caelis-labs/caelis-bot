package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func readNotebookJSON(path string, out any) error { return app.ReadNotebookOwnerRecord(path, out) }

func runNotebookOwner(ctx context.Context, args []string, out io.Writer) error {
	f := flag.NewFlagSet("notebook-owner", flag.ContinueOnError)
	f.SetOutput(out)
	dir := f.String("directory", "", "existing enrolled node directory")
	node := f.String("node-id", "", "exact enrolled node")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || nodeagent.CheckPrivateDirectory(*dir) != nil {
		return errors.New("existing private enrolled directory required")
	}
	var enrolled struct {
		ID string `json:"id"`
	}
	// Enrollment may include a label; use the public exact-identity reader.
	identity, err := nodeagent.ReadNativeEnrollmentIdentity(*dir, *node)
	if err != nil || identity.NodeID != *node {
		return errors.New("Notebook owner enrollment differs")
	}
	enrolled.ID = *node
	var request nodeagent.NotebookOwnerRequest
	d := json.NewDecoder(io.LimitReader(os.Stdin, 16<<10))
	d.DisallowUnknownFields()
	if d.Decode(&request) != nil || d.Decode(new(any)) != io.EOF || nodeagent.ValidateNotebookOwnerRequest(request) != nil {
		return errors.New("closed Notebook owner request required")
	}
	profile := nodeagent.NotebookOwnerProfile(*dir)
	if request.Action == "prepare" {
		if err = prepareNotebookProfile(profile, enrolled.ID, request); err != nil {
			return err
		}
	}
	if nodeagent.CheckPrivateDirectory(profile) != nil {
		return errors.New("Notebook Bot profile must be prepared first")
	}
	unlock, err := lockProfile(filepath.Join(profile, ".notebook-control.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	var botState bot.State
	if readNotebookJSON(filepath.Join(profile, "bot.json"), &botState) != nil || botState.ID != request.BotID {
		return errors.New("Notebook Bot identity differs")
	}
	state, err := notebookOwnerState(ctx, profile, enrolled.ID, request.BotID)
	if err != nil {
		return err
	}
	switch request.Action {
	case "prepare", "status":
	case "handoff":
		if !state.Stopped {
			return errors.New("handoff requires stopped source proof")
		}
		state.Handoff = completedNotebookHandoff(profile)
	case "stop":
		if state.Stopped {
			return errors.New("source Bot is not active; stop outcome needs original-owner review")
		}
		client, err := notebookProductClient(ctx, profile, state)
		if err != nil {
			return err
		}
		defer client.Close()
		result, err := client.Command(ctx, productrpc.Command{ID: request.OperationID, Kind: "stop-bot"})
		if err == nil && result.Outcome == "rejected" && result.Code == productrpc.StopNotDispatchedCode {
			state.StopResult = &result
			break
		}
		if err != nil || result.Outcome != "accepted" {
			return errors.New("source stop unconfirmed; do not launch a second Bot")
		}
		state, err = waitNotebookOwner(ctx, profile, enrolled.ID, request.BotID, true)
		if err != nil {
			return err
		}
		state.Handoff = completedNotebookHandoff(profile)
	case "start":
		if !state.Stopped {
			return errors.New("target Bot is already active")
		}
		// Never redispatch an ambiguous original start. Its recorded process may
		// have exited or still be starting; status remains read-only.
		var previous struct {
			OperationID string `json:"operationId"`
			Phase       string `json:"phase"`
		}
		record := filepath.Join(profile, "Product", "notebook-start.json")
		if _, e := os.Lstat(record); e == nil {
			if readNotebookJSON(record, &previous) != nil || previous.Phase != "active" || previous.OperationID == request.OperationID {
				return errors.New("previous fresh start requires native owner review")
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		previous.OperationID, previous.Phase = request.OperationID, "starting"
		if err = localstate.Write(record, previous); err != nil {
			return err
		}
		if err = resetNotebookSession(profile, request.OperationID); err != nil {
			return err
		}
		if err = applyNotebookPreferences(profile); err != nil {
			return err
		}
		helper, err := os.Executable()
		if err != nil {
			return err
		}
		log, err := os.OpenFile(filepath.Join(profile, "Product", "owner.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		defer log.Close()
		command := exec.Command(helper, "serve-bot", "--profile", profile, "--auth-file", filepath.Join(profile, "Product", "product.auth"), "--listen", "127.0.0.1:0", "--owned-node-id", enrolled.ID)
		command.Stdout, command.Stderr = log, log
		command.Env = notebookOwnerEnvironment()
		if err = detachNotebookOwner(command); err != nil {
			return err
		}
		state, err = waitNotebookOwner(ctx, profile, enrolled.ID, request.BotID, false)
		if err != nil {
			return err
		}
		previous.Phase = "active"
		if err = localstate.Write(record, previous); err != nil {
			return err
		}
	}
	return json.NewEncoder(out).Encode(state)
}

// Preparation changes only the app-owned fixed standby profile. It reads the
// target's nonsecret preferences; it never copies auth or adopts another Bot.
func prepareNotebookProfile(profile, node string, r nodeagent.NotebookOwnerRequest) error {
	if _, err := os.Lstat(profile); err == nil {
		if nodeagent.CheckPrivateDirectory(profile) != nil {
			return errors.New("unsafe existing Notebook standby")
		}
		var state bot.State
		if readNotebookJSON(filepath.Join(profile, "bot.json"), &state) != nil || state.ID != r.BotID {
			return errors.New("existing Notebook standby belongs to another Bot; original data preserved")
		}
		release, e := lockProfile(filepath.Join(profile, ".product-owner.lock"))
		if e != nil {
			return errors.New("backup Bot is active; Runtime unchanged")
		}
		defer release()
		if e = backend.SaveRuntimeSettingsDocument(filepath.Join(profile, "runtime.json"), api.RuntimeSettings{Runtime: string(r.Runtime.Backend), CLIPath: r.Runtime.Binary, CaelisStore: r.Runtime.Store}); e != nil {
			return e
		}
		return applyNotebookPreferences(profile)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	prefs, err := nodeagent.ReadExecutionPreferences(filepath.Dir(profile))
	if err != nil {
		return err
	}
	if err = os.Mkdir(profile, 0700); err != nil {
		return err
	}
	runtime := api.RuntimeSettings{Runtime: string(r.Runtime.Backend), CLIPath: r.Runtime.Binary, CaelisStore: r.Runtime.Store}
	if err = backend.SaveRuntimeSettingsDocument(filepath.Join(profile, "runtime.json"), runtime); err != nil {
		return err
	}
	provider := profile
	approval := "auto"
	if runtime.Runtime == "caelis" {
		provider = filepath.Join(profile, "providers", "caelis")
		approval = "workspace-write"
	}
	execution := api.ExecutionSettings{Model: prefs.Conversation.Model, Effort: prefs.Conversation.Effort, ServiceTier: prefs.Conversation.ServiceTier, ApprovalMode: approval}
	if err = localstate.Write(filepath.Join(provider, "execution.json"), execution); err != nil {
		return err
	}
	if err = localstate.Write(filepath.Join(provider, "work-execution.json"), prefs.Worker); err != nil {
		return err
	}
	if err = localstate.Write(filepath.Join(profile, "bot.json"), bot.State{Version: 1, PersonalVersion: 1, ID: r.BotID, Schedules: []bot.Schedule{}}); err != nil {
		return err
	}
	if err = localstate.Write(filepath.Join(profile, "bot-initialization.json"), map[string]any{"version": 1, "id": "notebook-identity", "runtime": runtime.Runtime, "status": "accepted"}); err != nil {
		return err
	}
	if err = localstate.Write(filepath.Join(profile, "Product", "node.json"), map[string]string{"id": node}); err != nil {
		return err
	}
	// Reuse the existing product proxy credential format, generated on target.
	// Neither the value nor Runtime credentials leave this machine.
	if err = os.WriteFile(filepath.Join(profile, "Product", "product.auth"), []byte(rand.Text()+rand.Text()+"\n"), 0600); err != nil {
		return err
	}
	return os.Mkdir(filepath.Join(profile, "Notebook"), 0700)
}

func notebookOwnerState(ctx context.Context, profile, node, botID string) (nodeagent.NotebookOwnerState, error) {
	state := nodeagent.NotebookOwnerState{Identity: productrpc.Identity{NodeID: node, Scope: productrpc.Scope{BotID: productrpc.ProfileBotID(botID)}}}
	release, err := lockProfile(filepath.Join(profile, ".product-owner.lock"))
	if err == nil {
		release()
		state.Stopped = true
		return state, nil
	}
	if readNotebookJSON(filepath.Join(profile, "Product", "owner.json"), &state) != nil || state.Identity.NodeID != node || state.Identity.BotID != productrpc.ProfileBotID(botID) {
		return state, errors.New("active Notebook owner is not yet observable")
	}
	state.Stopped = false
	client, err := notebookProductClient(ctx, profile, state)
	if err != nil {
		return state, err
	}
	defer client.Close()
	return state, nil
}
func notebookProductClient(ctx context.Context, profile string, state nodeagent.NotebookOwnerState) (*productrpc.Client, error) {
	token, err := readProductToken(filepath.Join(profile, "Product", "product.auth"))
	if err != nil {
		return nil, err
	}
	client, err := productrpc.NewClient(productrpc.ClientOptions{URL: state.Endpoint, ExpectedNode: state.Identity.NodeID, ExpectedBot: state.Identity.BotID, Token: token})
	if err != nil {
		return nil, err
	}
	identity, err := client.Connect(ctx)
	if err != nil || identity.Scope != state.Identity.Scope {
		client.Close()
		return nil, errors.New("actual Notebook owner differs from published pairing")
	}
	return client, nil
}
func waitNotebookOwner(ctx context.Context, profile, node, botID string, stopped bool) (nodeagent.NotebookOwnerState, error) {
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := notebookOwnerState(ctx, profile, node, botID)
		if err == nil && state.Stopped == stopped {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-deadline.C:
			return state, errors.New("Notebook owner transition unconfirmed")
		case <-ticker.C:
		}
	}
}
func notebookOwnerEnvironment() []string {
	// Target Runtime designation must win over inherited process overrides.
	env := []string{}
	for _, entry := range os.Environ() {
		if bytes.HasPrefix([]byte(entry), []byte("CODEX_BIN=")) || bytes.HasPrefix([]byte(entry), []byte("CAELIS_CODEX_SOCKET=")) {
			continue
		}
		env = append(env, entry)
	}
	return env
}

// Preserve target-native state in place before creating a new conversation.
// These files never enter rsync and are never used to restore or replay work.
func resetNotebookSession(profile, operation string) error {
	return app.PrepareFreshNotebookProfile(profile, operation)
}
func applyNotebookPreferences(profile string) error {
	runtime, err := app.ReadNotebookRuntimeSettings(profile)
	if err != nil {
		return err
	}
	prefs, err := nodeagent.ReadExecutionPreferences(filepath.Dir(profile))
	if err != nil {
		return err
	}
	provider, approval := profile, "auto"
	if runtime.Runtime == "caelis" {
		provider, approval = filepath.Join(profile, "providers", "caelis"), "workspace-write"
	}
	if err = localstate.Write(filepath.Join(provider, "execution.json"), api.ExecutionSettings{Model: prefs.Conversation.Model, Effort: prefs.Conversation.Effort, ServiceTier: prefs.Conversation.ServiceTier, ApprovalMode: approval}); err != nil {
		return err
	}
	return localstate.Write(filepath.Join(provider, "work-execution.json"), prefs.Worker)
}

func completedNotebookHandoff(profile string) []byte {
	var handoff []byte
	if readNotebookJSON(filepath.Join(profile, "Product", "completed-handoff.json"), &handoff) != nil || len(handoff) == 0 || len(handoff) > 16<<10 {
		return nil
	}
	path := filepath.Join(profile, "Notebook", "HANDOFF.md")
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
		return nil
	}
	file, e := os.Open(path)
	if e != nil {
		return nil
	}
	defer file.Close()
	opened, e := file.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return nil
	}
	current, e := io.ReadAll(io.LimitReader(file, (16<<10)+1))
	if e != nil || !bytes.Equal(current, handoff) {
		return nil
	}
	return handoff
}
