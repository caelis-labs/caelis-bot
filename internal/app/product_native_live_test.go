package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

type thinNativeFixture struct {
	Target, Root, Endpoint string
	Identity               productrpc.Identity
	Synthetic              bool
	NativeVersion          string `json:"native_version"`
}

type thinNativeFacts struct {
	Counts        map[string]int `json:"counts"`
	NodeAlive     bool           `json:"node_alive"`
	HostAlive     bool           `json:"host_alive"`
	ListenerAlive bool           `json:"listener_alive"`
	Dropped       bool           `json:"response_dropped"`
}

func thinNativeControl(t *testing.T, ctx context.Context, f thinNativeFixture, action string) thinNativeFacts {
	t.Helper()
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	args := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UpdateHostKeys=no", "-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ConnectTimeout=10", "--", f.Target, "python3 " + quote(f.Root+"/fixture.py") + " " + quote(f.Root) + " " + quote(action)}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stderr = io.Discard
	b, err := cmd.Output()
	if err != nil {
		t.Fatal("owned synthetic fixture control unavailable")
	}
	var facts thinNativeFacts
	if json.Unmarshal(b, &facts) != nil {
		t.Fatal("owned synthetic fixture control incompatible")
	}
	return facts
}

func waitThinNative(t *testing.T, ctx context.Context, a *Application, ready func(api.Snapshot) bool) api.Snapshot {
	t.Helper()
	s := a.Backend.Snapshot()
	for !ready(s) {
		var err error
		s, err = a.product.WaitSnapshot(ctx, s.Revision)
		if err != nil {
			t.Fatal("native thin APP facts were not observed")
		}
	}
	return s
}

// This opt-in assembles the real APP over its real SSH product adapter. It uses
// only a newly created synthetic fixture, never an existing user's Host/Store.
func TestNativeThinApplicationOverSSH(t *testing.T) {
	path := os.Getenv("CAELIS_BOT_THIN_SSH_FIXTURE")
	if path == "" {
		t.Skip("set fresh isolated synthetic headless fixture metadata")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("private fixture metadata unavailable")
	}
	var f thinNativeFixture
	if json.Unmarshal(b, &f) != nil || !f.Synthetic || f.NativeVersion != "0.65.0" || !regexp.MustCompile(`^/tmp/caelis-bot-issue47-product-[a-f0-9]{16}$`).MatchString(f.Root) {
		t.Fatal("unsafe synthetic fixture metadata")
	}
	pairing := backend.ProductPairing{Mode: "remote", Label: "Synthetic Native Bot", SSH: f.Target, Helper: f.Root + "/caelis-node", Endpoint: f.Endpoint, AuthFile: f.Root + "/product.auth", NodeID: f.Identity.NodeID, BotID: f.Identity.BotID}
	if validateProductPairing(pairing) != nil {
		t.Fatal("unsafe synthetic product pairing")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	root := t.TempDir()
	t.Setenv("CODEX_BIN", filepath.Join(root, "missing-codex"))
	if err := localstate.Write(filepath.Join(root, "product-connection.json"), productPairingDocument{Version: 1, Pairing: pairing}); err != nil {
		t.Fatal(err)
	}
	// app.New would fail parsing these if it entered local provider discovery.
	for _, name := range []string{"runtime.json", "bot.json", "bot-initialization.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("invalid-local-fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(root, Host{})
	if err != nil || a.product == nil || a.personal != nil || a.companion != nil || a.tasks != nil || a.workerNodes != nil {
		t.Fatal("real APP selected a local Runtime or resident services")
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Start(); err != nil {
		t.Fatal("real thin APP start failed")
	}
	waitThinNative(t, ctx, a, func(s api.Snapshot) bool { return s.Connection == "ready" })
	if a.Backend.ProductConnection().ActiveMode != "remote" || a.Backend.ProductConnection().State != "ready" {
		t.Fatal("public APP connection state did not select remote product")
	}
	if a.Backend.BotInitialization().Required {
		if _, err := a.Backend.InitializeBot(ctx, api.BotIntroduction{Name: "Synthetic Thin Fixture", Description: "Temporary loopback provider only"}); err != nil {
			t.Fatal("public APP initialization failed")
		}
	}
	waitThinNative(t, ctx, a, func(s api.Snapshot) bool { return s.CanSend && a.Backend.BotInitialization().Status == "accepted" })
	managed, err := a.Backend.RemoteRuntime(ctx)
	if err != nil || !managed.Available || !managed.Capabilities.Configuration {
		t.Fatal("public APP typed management unavailable")
	}
	configuration, err := a.Backend.RemoteRuntimeConfiguration(ctx, managed.Binding)
	if err != nil || configuration.Main.Model == "" {
		t.Fatal("public APP nonsecret native configuration unavailable")
	}
	changed, err := a.Backend.ChangeRemoteRuntimeConfiguration(ctx, backend.RemoteConfigurationRequest{ID: "thin-native-public-config", Binding: managed.Binding, Change: api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: configuration.Revision, Selection: configuration.Main}})
	if err != nil || changed.Outcome != "accepted" || changed.Configuration == nil || changed.Configuration.Outcome != "committed" || changed.Configuration.OperationID != "" {
		t.Fatal("public APP native configuration receipt invalid")
	}
	input := api.Submission{ID: "native-lost", Text: "CASE_PRODUCT_LOST"}
	receipt, err := a.Backend.Submit(ctx, input)
	if err == nil || receipt.ID != input.ID || receipt.Outcome != "unknown" {
		t.Fatal("public APP lost original submission reply did not remain unknown")
	}
	if err := a.Backend.DisconnectProduct(ctx); err != nil {
		t.Fatal("public APP detach failed")
	}
	facts := thinNativeControl(t, ctx, f, "status")
	if !facts.NodeAlive || !facts.HostAlive || !facts.ListenerAlive {
		t.Fatal("APP detach stopped target owner")
	}
	if err := a.Backend.ReconnectProduct(ctx); err != nil {
		t.Fatal("public APP original receipt reconnect failed")
	}
	waitThinNative(t, ctx, a, func(s api.Snapshot) bool { return s.CanSend })
	a.product.op.Lock()
	unresolved := len(a.product.receipts.Pending)
	a.product.op.Unlock()
	if unresolved != 0 {
		t.Fatal("public APP did not reconcile the original product receipt")
	}
	facts = thinNativeControl(t, ctx, f, "status")
	if facts.Counts["CASE_PRODUCT_LOST"] != 1 || !facts.Dropped {
		t.Fatal("APP reconnect replayed native input or loss fixture did not execute")
	}
	hold := api.Submission{ID: "thin-native-detach", Text: "CASE_PRODUCT_DETACH"}
	receipt, err = a.Backend.Submit(ctx, hold)
	if err != nil || receipt.Outcome != "accepted" || receipt.ID != hold.ID {
		t.Fatal("public APP hold admission failed")
	}
	waitThinNative(t, ctx, a, func(s api.Snapshot) bool { return s.CanInterrupt })
	if err := a.Close(); err != nil {
		t.Fatal("real APP close failed to detach")
	}
	facts = thinNativeControl(t, ctx, f, "status")
	if !facts.NodeAlive || !facts.HostAlive || !facts.ListenerAlive || facts.Counts["CASE_PRODUCT_DETACH"] != 1 {
		t.Fatal("APP.Close stopped owned remote work")
	}
	// Explicit owner stop is a separate native product command after APP closes.
	client, transport, err := newSSHProductClient(pairing)
	if err != nil {
		t.Fatal("explicit owner connection unavailable")
	}
	defer transport.Close()
	defer client.Close()
	if _, err := client.Connect(ctx); err != nil {
		t.Fatal("explicit owner identity unavailable")
	}
	recovered, err := client.Receipt(ctx, input.ID)
	if err != nil || recovered.ID != input.ID || recovered.Outcome != "accepted" {
		t.Fatal("native original product receipt not retained after APP.Close")
	}
	_ = thinNativeControl(t, ctx, f, "release")
	result, err := client.Command(ctx, productrpc.Command{ID: "thin-native-owner-stop", Kind: "stop-bot"})
	if err != nil || result.Outcome != "accepted" {
		t.Fatal("explicit native owner stop failed")
	}
	for {
		facts = thinNativeControl(t, ctx, f, "status")
		if !facts.NodeAlive && !facts.ListenerAlive {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("explicit owner Stop retained owned listener")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !facts.HostAlive {
		t.Fatal("Bot Stop incorrectly terminated separate fixture Core Host")
	}
	for _, name := range []string{"personal", "Notebook", "tasks.json", "Codex", "Caelis"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("thin APP wrote local resident state")
		}
	}
	t.Log("actual app.New/Start + public backend Submit/settings + strict SSH + official native Caelis0.65 passed: no local Runtime, original unknown receipt/reconnect without replay, APP.Close detach, explicit owner Stop; synthetic provider, no GUI or real model claim")
}
