package desktopcontrol

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestControllerExplicitRecoveryNeverReplaysInput(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required for process lifecycle test")
	}
	node, _ = filepath.Abs(node)
	host := filepath.Join(t.TempDir(), "host.mjs")
	err = os.WriteFile(host, []byte(`import {createInterface} from 'node:readline';
for await (const line of createInterface({input:process.stdin})) {
 const r=JSON.parse(line);
 if(r.name==='bot_desktop_perform') await new Promise(resolve=>setTimeout(resolve,10000));
 process.stdout.write(JSON.stringify({content:[],structuredContent:{pid:process.pid}})+'\n');
}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	c := &Controller{node: node, script: host}
	defer c.Close()
	call := func(name string) bool { return c.CallTool(t.Context(), name, json.RawMessage(`{}`)).IsError }
	if !call("bot_desktop_perform") || c.driver != nil {
		t.Fatal("action started an unobserved helper")
	}
	first := c.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`))
	if first.IsError {
		t.Fatal("observation did not start helper")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if !c.CallTool(ctx, "bot_desktop_perform", json.RawMessage(`{}`)).IsError {
		t.Fatal("uncertain input reported success")
	}
	if c.driver != nil || !call("bot_desktop_perform") || c.driver != nil {
		t.Fatal("input replay restarted helper")
	}
	fresh := c.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`))
	if fresh.IsError || fresh.StructuredContent["pid"] == first.StructuredContent["pid"] {
		t.Fatal("explicit fresh observation did not recover")
	}
	c.Close()
	if !call("bot_desktop_observe") || c.driver != nil {
		t.Fatal("closed controller restarted")
	}
}
