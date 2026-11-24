package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestPrivateHostReadinessFailureCleanup(t *testing.T) {
	t.Setenv("CAELIS_BOT_TEST_EVIDENCE_DIR", "")
	var fixture *nativeHostFixture
	var pid int
	t.Run("missing_discovery", func(t *testing.T) {
		script := filepath.Join(t.TempDir(), "host")
		// A real long-lived subprocess with no discovery record. exec keeps
		// the foreground PID and avoids leaving an unrelated shell child.
		if err := os.WriteFile(script, []byte("#!/bin/sh\nexec /bin/sleep 60\n"), 0700); err != nil {
			t.Fatal(err)
		}
		fixture = newNativeHostFixture(t, script)
		p, err := fixture.StartForeground(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		pid = p.PID
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
		defer cancel()
		if err := fixture.waitReady(ctx, p); !errors.Is(err, context.DeadlineExceeded) || p.Exited() {
			t.Fatalf("expected a live foreground Host at readiness timeout: PID %d, %v", pid, err)
		}
		t.Logf("readiness deadline expired after 150ms with foreground PID %d still running", pid)
	})
	requirePrivateFixtureGone(t, fixture)
}

func TestNativePrivateHostLifecycle(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY to a released Core with native DTW")
	}
	for _, scenario := range []string{"normal", "reconnect_replacement"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("CAELIS_BOT_TEST_EVIDENCE_DIR", "")
			var fixture *nativeHostFixture
			t.Run("host", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
				defer cancel()
				model := newAcceptanceModel()
				provider := httptest.NewServer(http.HandlerFunc(model.serve))
				defer provider.Close()
				fixture = newNativeHostFixture(t, bin)
				foreground, err := fixture.startReady(ctx)
				if err != nil {
					t.Fatal(err)
				}
				client, err := setupClient(ctx, fixture.settings)
				if err != nil {
					t.Fatal(err)
				}
				defer client.http.CloseIdleConnections()
				info, err := initialize(ctx, client)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("foreground PID %d ready in instance %s", foreground, value(info.InstanceId))
				var status wire.StatusSnapshot
				if err = client.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
					t.Fatal(err)
				}
				op := "lifecycle-provider"
				var configured wire.CommandResult
				if err = client.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{
					OperationId: &op, ExpectedRevision: &status.Configuration.Revision,
					Config: wire.ConnectConfig{Provider: "custom-responses", Model: "lifecycle", BaseUrl: pointer(provider.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")},
				}, &configured, op, string(status.Configuration.Revision)); err != nil || !succeeded(configured.Outcome) {
					t.Fatal("configure synthetic provider", err, configured.Outcome)
				}
				s := New(Options{RequireApproval: true, Directory: filepath.Join(fixture.Root, "bot"), Settings: fixture.settings, Execution: api.ExecutionSettings{Model: fixtureModelSelector(t, ctx, client, "lifecycle"), ApprovalMode: "workspace-write"}})
				defer closeNativeFixture(t, s)
				if err = s.ConfigureBotTools(&api.ToolConnection{}); err != nil {
					t.Fatal(err)
				}
				if err = s.Connect(ctx); err != nil {
					t.Fatal(err)
				}
				waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
				submitAcceptance(t, ctx, s, "CASE_LIFECYCLE_FOREGROUND")
				session := s.ConversationState().Session
				waitFixtureDTW(t, ctx, s.client, session)
				reportFixtureProcesses(t, ctx, fixture, foreground, false)
				if scenario == "reconnect_replacement" {
					if err := fixture.StopForeground(ctx); err != nil {
						t.Fatal(err)
					}
					// The fixture never starts another foreground command. The
					// bound Bot must take its real same-Store service start path.
					if err := s.Reconnect(ctx); err != nil {
						t.Fatal("reconnect did not launch managed Host", err)
					}
					d, _, err := Discover(fixture.settings)
					if err != nil || d.InstanceID == value(info.InstanceId) || s.ConversationState().Session != session {
						t.Fatal("managed replacement lost original Store/session identity", err)
					}
					waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
					submitAcceptance(t, ctx, s, "CASE_LIFECYCLE_MANAGED")
					waitFixtureDTW(t, ctx, s.client, session)
					reportFixtureProcesses(t, ctx, fixture, foreground, true)
				}
				cancel() // Cleanup must still stop all observed processes.
			})
			requirePrivateFixtureGone(t, fixture)
		})
	}
}

func waitFixtureDTW(t *testing.T, ctx context.Context, host *client, session string) {
	t.Helper()
	waitAcceptance(t, ctx, func() bool {
		var status wire.SessionMCPStatus
		if host.json(ctx, "GET", "/sessions/"+idPath(session)+"/mcp-status", nil, &status, "", "") != nil {
			return false
		}
		for _, server := range status.Servers {
			if server.Name == "dtw" && server.Status == "running" && len(server.Tools) == 2 {
				return true
			}
		}
		return false
	})
}

func reportFixtureProcesses(t *testing.T, ctx context.Context, fixture *nativeHostFixture, foreground int, replaced bool) {
	t.Helper()
	pids, err := fixture.ActivePIDs(ctx)
	if err != nil || len(pids) < 2 {
		t.Fatalf("expected private Host and DTW OS processes: %v %v", pids, err)
	}
	// The discovery PID separates the Host from its native MCP child.
	raw, err := os.ReadFile(filepath.Join(fixture.Path, "runtime/service/discovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(raw, &record); err != nil || record.PID <= 1 || (record.PID != foreground) != replaced {
		t.Fatalf("unexpected discovery PID %d, original %d: %v", record.PID, foreground, err)
	}
	dtw := 0
	for _, pid := range pids {
		out, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if err == nil && strings.Contains(string(out), "/bin/dtw mcp") {
			dtw = pid
		}
	}
	if dtw == 0 {
		t.Fatalf("DTW process absent from private PIDs %v", pids)
	}
	var files int
	var bytes, dtwBytes int64
	err = filepath.WalkDir(fixture.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.Type().IsRegular() {
			return walkErr
		}
		stat, err := entry.Info()
		if err != nil {
			return err
		}
		files++
		bytes += stat.Size()
		if strings.Contains(path, "/builtin-plugins/desktop-world/") {
			dtwBytes += stat.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("private Host PID %d, DTW MCP PID %d (replacement=%t); %d private files, %d bytes including %d Desktop World package bytes",
		record.PID, dtw, replaced, files, bytes, dtwBytes)
}

func requirePrivateFixtureGone(t *testing.T, fixture *nativeHostFixture) {
	t.Helper()
	if fixture == nil {
		t.Fatal("fixture was not allocated")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if pids, err := fixture.ActivePIDs(ctx); err != nil || len(pids) != 0 {
		t.Fatalf("private processes survived cleanup: %v %v", pids, err)
	}
	if _, err := os.Stat(fixture.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private directory survived cleanup: %s: %v", fixture.Root, err)
	}
}
