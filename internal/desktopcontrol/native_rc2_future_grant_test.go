package desktopcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
)

func TestPackagedHelperNativeRC2FutureGrantExpiry(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_FUTURE_TITLE")
	logPath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_FUTURE_LOG")
	once := os.Getenv("CAELIS_BOT_TEST_DESKTOP_FUTURE_ONCE")
	if helper == "" || title == "" || logPath == "" || once == "" {
		t.Skip("requires unique future-app fixture, packaged helper and once marker")
	}
	if _, err := os.Stat(logPath); err == nil {
		t.Fatal("future fixture already launched; no repeated instance")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-future-app"), 40*time.Second)
	defer cancel()
	// The Bot contract requires an initial desktop observation before input or
	// authority changes, even for an application that has not launched yet.
	preflight, _ := json.Marshal(map[string]any{"requestId": "future-desktop-before", "args": map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}})
	if out := c.CallTool(ctx, Prefix+"observe", preflight); out.IsError {
		t.Fatal("future-app desktop preflight failed", out.StructuredContent)
	}
	declare, _ := json.Marshal(map[string]string{"operation": "declare", "windowTitle": title, "purpose": "disposable future-app acceptance"})
	if out := c.CallTool(ctx, Prefix+"authorize", declare); out.IsError {
		t.Fatal("future declaration rejected", out.StructuredContent)
	}
	h := c.client
	status, err := h.Grants(ctx, "rc2-future-app")
	if err != nil || len(status.Grants) != 1 || status.Grants[0].State != "pending" || status.Grants[0].Application != "" {
		t.Fatalf("unlaunched app not pending without authority: %+v %v", status, err)
	}
	grantID := status.Grants[0].ID
	marker, err := os.OpenFile(once, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("future-app fixture launch already attempted", err)
	}
	_, writeErr := marker.WriteString(title + "\n")
	closeErr := marker.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("future-app marker failed", writeErr, closeErr)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "../.."))
	launch := exec.CommandContext(ctx, "bash", "script/build_and_run.sh", "--desktop-control-preview")
	launch.Dir = root
	launch.Env = append(os.Environ(), "BOT_DESKTOP_FIXTURE_MENU_RC2=1", "BOT_DESKTOP_FIXTURE_TITLE="+title, "BOT_DESKTOP_FIXTURE_LOG="+logPath)
	if output, err := launch.CombinedOutput(); err != nil {
		t.Fatalf("standard fixture launch failed: %v %s", err, output)
	}
	var pid int
	for i := 0; i < 60; i++ {
		data, err := os.ReadFile(logPath)
		if err == nil {
			s := bufio.NewScanner(bytes.NewReader(data))
			var ready bool
			for s.Scan() {
				var event struct {
					Event string `json:"event"`
					Value string `json:"value"`
				}
				if json.Unmarshal(s.Bytes(), &event) != nil {
					continue
				}
				if event.Event == "ready" && event.Value == title {
					ready = true
				}
				if event.Event == "pid" {
					pid, _ = strconv.Atoi(event.Value)
				}
			}
			if ready && pid > 1 {
				break
			}
		}
		if i == 59 {
			t.Fatal("new fixture did not report ready/PID; do not relaunch without inspecting instance")
		}
		time.Sleep(100 * time.Millisecond)
	}
	for i := 0; i < 30; i++ {
		status, err = h.Grants(ctx, "rc2-future-app")
		if err == nil && len(status.Grants) == 1 && status.Grants[0].State == "active" && status.Grants[0].Application != "" {
			break
		}
		if i == 29 {
			t.Fatalf("pending declaration did not bind the new app: %+v %v", status, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status.Grants[0].ID != grantID {
		t.Fatal("future declaration changed grant ID")
	}
	raw, _ := json.Marshal(map[string]any{"requestId": "future-window", "args": map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}})
	out := c.CallTool(ctx, Prefix+"observe", raw)
	if out.IsError {
		t.Fatal("bound app observation failed", out.StructuredContent)
	}
	var inventory dw.Observation
	if err := protocol.Decode(c.requests["future-window"].reply.Result, &inventory); err != nil {
		t.Fatal(err)
	}
	var matches int
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title && o.App == status.Grants[0].Application {
			matches++
		}
	}
	if matches != 1 {
		t.Fatal("active declaration did not bind exactly the launched window")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal("app-owned PID missing", err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal("could not stop exact app-owned PID", err)
	}
	for i := 0; i < 40; i++ {
		status, err = h.Grants(ctx, "rc2-future-app")
		if err == nil && len(status.Grants) == 1 && status.Grants[0].State == "expired" && status.Grants[0].Reason == "application_exited" {
			break
		}
		if i == 39 {
			t.Fatalf("exited app did not expire grant: %+v %v", status, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status.Grants[0].ID != grantID {
		t.Fatal("expired declaration changed grant ID")
	}
	c.EndTurn("rc2-future-app")
	t.Log("published helper: exact unlaunched window pending without authority, launched app bound once, app exit expired same grant ID")
}
