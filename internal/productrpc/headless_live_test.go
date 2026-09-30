package productrpc

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

type headlessFixture struct {
	Target, Root, Endpoint string
	Identity               Identity
	Synthetic              bool
	NativeVersion          string `json:"native_version"`
}

func fixtureSSHArgs(target, command string) []string {
	return []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UpdateHostKeys=no", "-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "ForwardX11Trusted=no", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ForkAfterAuthentication=no", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ConnectTimeout=15", "--", target, command}
}

func quoteFixture(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }

type fixtureSSHStream struct {
	in   io.WriteCloser
	out  io.ReadCloser
	cmd  *exec.Cmd
	once sync.Once
}

func (s *fixtureSSHStream) Read(b []byte) (int, error)  { return s.out.Read(b) }
func (s *fixtureSSHStream) Write(b []byte) (int, error) { return s.in.Write(b) }
func (s *fixtureSSHStream) Close() error {
	s.once.Do(func() { _ = s.in.Close(); _ = s.out.Close(); _ = s.cmd.Process.Kill(); _ = s.cmd.Wait() })
	return nil
}

func connectHeadlessFixture(t *testing.T, ctx context.Context, f headlessFixture) *Client {
	t.Helper()
	command := quoteFixture(f.Root+"/caelis-node") + " proxy-product --endpoint " + quoteFixture(f.Endpoint) + " --auth-file " + quoteFixture(f.Root+"/product.auth")
	cmd := exec.CommandContext(ctx, "ssh", fixtureSSHArgs(f.Target, command)...)
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal("strict SSH fixture proxy unavailable")
	}
	c, err := NewStdioClient(StdioOptions{ExpectedNode: f.Identity.NodeID, ExpectedBot: f.Identity.BotID}, &fixtureSSHStream{in: in, out: out, cmd: cmd})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	identity, err := c.Connect(ctx)
	if err != nil || identity.Scope != f.Identity.Scope {
		t.Fatal("headless paired identity failed", err)
	}
	return c
}

type nativeFixtureState struct {
	Counts        map[string]int `json:"counts"`
	NodeAlive     bool           `json:"node_alive"`
	HostAlive     bool           `json:"host_alive"`
	ListenerAlive bool           `json:"listener_alive"`
	Dropped       bool           `json:"response_dropped"`
}

func nativeFixtureControl(t *testing.T, ctx context.Context, f headlessFixture, action string) nativeFixtureState {
	t.Helper()
	command := "python3 " + quoteFixture(f.Root+"/fixture.py") + " " + quoteFixture(f.Root) + " " + quoteFixture(action)
	cmd := exec.CommandContext(ctx, "ssh", fixtureSSHArgs(f.Target, command)...)
	cmd.Stderr = io.Discard
	b, err := cmd.Output()
	if err != nil {
		t.Fatal("synthetic target control unavailable")
	}
	var state nativeFixtureState
	if json.Unmarshal(b, &state) != nil {
		t.Fatal("synthetic target control incompatible")
	}
	return state
}

func waitHeadlessState(t *testing.T, ctx context.Context, c *Client, ready func(State) bool) State {
	t.Helper()
	state, err := c.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for !ready(state) {
		state, err = c.Watch(ctx, state.Cursor)
		if err != nil {
			t.Fatal("headless product state not observed", err)
		}
	}
	return state
}

// Explicit opt-in uses only a freshly created target fixture. No login/user
// Store or credentials are discovered by this test; setup/cleanup own its root.
func TestNativeLinuxHeadlessProductStdio(t *testing.T) {
	path := os.Getenv("CAELIS_BOT_PRODUCT_SSH_FIXTURE")
	if path == "" {
		t.Skip("set fresh isolated synthetic headless fixture metadata")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f headlessFixture
	if json.Unmarshal(b, &f) != nil || !f.Synthetic || f.NativeVersion != "0.65.0" || !regexp.MustCompile(`^/tmp/caelis-bot-issue47-product-[a-f0-9]{16}$`).MatchString(f.Root) || !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`).MatchString(f.Target) {
		t.Fatal("unsafe headless fixture metadata")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	c := connectHeadlessFixture(t, ctx, f)
	state := waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.Connection == "ready" })
	caps, err := c.ManagementCapabilities(ctx)
	if err != nil || !caps.Installation || !caps.Configuration || caps.ConfigurationReceiptLookup {
		t.Fatal("native optional management capabilities", caps, err)
	}
	releases, err := c.ReviewedReleases(ctx)
	if err != nil || len(releases) == 0 {
		t.Fatal("native reviewed releases unavailable", err)
	}
	installed, err := c.RuntimeStatus(ctx, "caelis")
	if err != nil || installed.Runtime != "caelis" || installed.Installed {
		t.Fatal("isolated managed runtime status invalid", err)
	}
	configuration, err := c.RuntimeConfiguration(ctx)
	if err != nil || configuration.Main.Model == "" {
		t.Fatal("native public configuration unavailable", err)
	}
	changed, err := c.ChangeRuntimeConfiguration(ctx, productmanagement.ConfigurationCommand{ID: "native-public-config", Change: api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: configuration.Revision, Selection: configuration.Main}})
	if err != nil || changed.Outcome != "accepted" || changed.Native.Outcome != "committed" || changed.Native.OperationID != "" {
		t.Fatal("native public configuration receipt", changed.Outcome, changed.Native.Outcome, err)
	}
	configurationReceipt, err := c.Receipt(ctx, "native-public-config")
	if err != nil || configurationReceipt.Configuration == nil || configurationReceipt.Configuration.Native.Outcome != "committed" {
		t.Fatal("original product configuration receipt unavailable", err)
	}

	if state.Initialization.Required {
		r, err := c.Command(ctx, Command{ID: "native-introduction", Kind: "initialize", Introduction: &api.BotIntroduction{Name: "Synthetic Fixture", Description: "Temporary loopback provider only"}})
		if err != nil || r.Outcome != "accepted" {
			t.Fatal("headless initialization failed", r.Outcome, err)
		}
	}
	waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.CanSend && s.Initialization.Status == "accepted" })
	submit := Command{ID: "native-lost", Kind: "submit", Submission: &api.Submission{ID: "native-lost", Text: "CASE_PRODUCT_LOST"}}
	r, err := c.Command(ctx, submit)
	if err == nil || r.Outcome != "unknown" {
		t.Fatal("lost native response was not preserved as unknown", r.Outcome, err)
	}
	c.Close()
	c = connectHeadlessFixture(t, ctx, f)
	r, err = c.Receipt(ctx, submit.ID)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("original native receipt missing", r.Outcome, err)
	}
	waitHeadlessState(t, ctx, c, func(s State) bool {
		for _, item := range s.Snapshot.Items {
			if item.Kind == "assistant" && strings.Contains(item.Text, "PRODUCT_NATIVE_COMPLETE") {
				return s.Snapshot.CanSend
			}
		}
		return false
	})
	status := nativeFixtureControl(t, ctx, f, "status")
	if status.Counts["CASE_PRODUCT_LOST"] != 1 || !status.Dropped {
		t.Fatal("native unknown intent replayed or loss not injected")
	}
	hold := Command{ID: "native-detach", Kind: "submit", Submission: &api.Submission{ID: "native-detach", Text: "CASE_PRODUCT_DETACH"}}
	r, err = c.Command(ctx, hold)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("native hold admission failed", r.Outcome, err)
	}
	waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.CanInterrupt })
	c.Close()
	status = nativeFixtureControl(t, ctx, f, "status")
	if !status.NodeAlive || !status.HostAlive || !status.ListenerAlive {
		t.Fatal("observer detach stopped native owner")
	}
	c = connectHeadlessFixture(t, ctx, f)
	r, err = c.Receipt(ctx, hold.ID)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("detached native intent lost its original receipt")
	}
	_ = nativeFixtureControl(t, ctx, f, "release")
	waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.CanSend })
	r, err = c.Command(ctx, Command{ID: "native-stop", Kind: "stop-bot"})
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("explicit owner stop receipt failed", r.Outcome, err)
	}
	for {
		status = nativeFixtureControl(t, ctx, f, "status")
		if !status.NodeAlive && !status.ListenerAlive {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("explicit stop left owner/listener alive")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !status.HostAlive || status.Counts["CASE_PRODUCT_DETACH"] != 1 {
		t.Fatal("shared native Host lost ownership or detach replayed work")
	}
	t.Log("public Caelis0.65 native Linux headless product: paired identity/init/native facts, typed management status/config committed receipt, unknown original receipt, detach/reconnect, explicit owner/listener stop passed; synthetic provider, no user credentials")
}
