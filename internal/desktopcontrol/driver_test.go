package desktopcontrol

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the real process/pipe lifecycle without loading the platform SDK.
// In particular a cancelled dispatch must close its owner, never silently replay.
func TestDriverOwnershipAndUncertainCancellation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the experimental driver host")
	}
	node, err = filepath.Abs(node)
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(t.TempDir(), "host.mjs")
	source := `import {createInterface} from 'node:readline';
for await (const line of createInterface({input:process.stdin})) {
  const request=JSON.parse(line);
  if(request.name==='bot_desktop_perform') await new Promise(resolve=>setTimeout(resolve,10000));
  const state={credentialInherited:Boolean(process.env.CAELIS_BOT_TOKEN||process.env.OPENAI_API_KEY)};
  process.stdout.write(JSON.stringify({content:[{type:'text',text:'fixture'}],structuredContent:state})+'\n');
}`
	if err = os.WriteFile(host, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAELIS_BOT_TOKEN", "fixture-only-secret")
	t.Setenv("OPENAI_API_KEY", "fixture-only-secret")
	driver, err := StartDriver(node, host)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if result := driver.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`)); result.IsError || result.StructuredContent["credentialInherited"] != false {
		t.Fatal("driver did not isolate inherited credentials or round trip structured output")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if result := driver.CallTool(ctx, "bot_desktop_perform", json.RawMessage(`{}`)); !result.IsError || !strings.Contains(result.Content[0]["text"], "unknown") {
		t.Fatal("cancelled dispatch was not uncertain")
	}
	select {
	case <-driver.exited:
	default:
		t.Fatal("driver child survived cancellation")
	}
	if result := driver.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`)); !result.IsError || !strings.Contains(result.Content[0]["text"], "unavailable") {
		t.Fatal("cancelled child was restarted implicitly")
	}
}
