package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// This probes the installed app-server's real turn/interrupt behavior while an
// MCP tools/call is active. Both the model and MCP service are local fixtures.
func TestNativeMCPInterruptDuringToolCall(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX")
	if binary == "" {
		t.Skip("set CAELIS_BOT_TEST_CODEX to the installed CLI")
	}
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	var mu sync.Mutex
	requests := 0
	var requestBodies []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		mu.Lock()
		requests++
		n := requests
		requestBodies = append(requestBodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
		id := fmt.Sprintf("dream-probe-%d", n)
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		var item map[string]any
		if n == 1 {
			item = map[string]any{"id": "provider-item-dream", "type": "function_call", "namespace": "mcp__caelis_bot", "name": "dream_probe", "call_id": "provider-call-dream", "arguments": `{"handoff":"Original task and receipt."}`}
		} else {
			item = map[string]any{"id": "provider-item-later", "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "unexpected follow-up model step", "annotations": []any{}}}}
		}
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer provider.Close()
	config := fmt.Sprintf("model = \"dream-fixture\"\nmodel_provider = \"dream-fixture\"\n[model_providers.dream-fixture]\nname = \"Local fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", provider.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	var s *Session
	interrupts := make(chan error, 1)
	interruptor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		thread, turn, c := s.binding.ThreadID, s.run, s.client
		s.mu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		err := callDecode(ctx, c, "turn/interrupt", map[string]string{"threadId": thread, "turnId": turn}, nil)
		if err == nil {
			var next threadExecutionResponse
			err = callDecode(ctx, c, "thread/start", s.connectionParams(), &next)
			if err == nil && (next.Thread.ID == "" || next.Thread.ID == thread) {
				err = fmt.Errorf("new native thread was not distinct: %q", next.Thread.ID)
			}
		}
		interrupts <- err
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer interruptor.Close()
	script := filepath.Join(root, "dream-mcp.py")
	const mcp = `import json,sys,urllib.request
for line in sys.stdin:
 try:
  req=json.loads(line); rid=req.get("id")
  if rid is None: continue
  method=req.get("method")
  if method=="initialize": result={"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"dream-fixture","version":"1"}}
  elif method=="tools/list": result={"tools":[{"name":"dream_probe","description":"Fixture","inputSchema":{"type":"object","properties":{"handoff":{"type":"string"}},"required":["handoff"]}}]}
  elif method=="tools/call":
   try: urllib.request.urlopen(urllib.request.Request(sys.argv[1],data=b"dream"),timeout=12).read()
   except Exception as error: print(str(error),file=sys.stderr)
   result={"content":[{"type":"text","text":"dream accepted"}],"isError":False}
  else: result={}
  sys.stdout.write(json.dumps({"jsonrpc":"2.0","id":rid,"result":result})+"\n");sys.stdout.flush()
 except Exception as error: print(str(error),file=sys.stderr)
`
	if err := os.WriteFile(script, []byte(mcp), 0600); err != nil {
		t.Fatal(err)
	}
	s = NewSession(SessionOptions{Binary: binary, Directory: root, StateFile: filepath.Join(root, "binding.json"), BotTools: &api.ToolConnection{Command: "python3", Args: []string{"-u", script, interruptor.URL}, ApprovedTools: []string{"dream_probe"}}})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	defer func() {
		closeCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = s.Close(closeCtx)
	}()
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err, s.lastConnectCause)
	}
	var status any
	if err := callDecode(ctx, s.client, "mcpServerStatus/list", map[string]any{"threadId": s.binding.ThreadID, "serverName": "caelis_bot"}, &status); err != nil {
		t.Fatal("MCP fixture status", err)
	}
	t.Logf("MCP status: %+v", status)
	if receipt, err := s.Submit(ctx, api.Submission{ID: "dream-fixture-input", Text: "Invoke dream probe."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	select {
	case err := <-interrupts:
		if err != nil {
			t.Fatalf("turn/interrupt failed: %v", err)
		}
	case <-time.After(4 * time.Second):
		mu.Lock()
		got := requests
		bodies := append([]string(nil), requestBodies...)
		mu.Unlock()
		snapshot := s.Snapshot()
		thread, _ := readThreadState(context.Background(), s.client, s.binding.ThreadID)
		for i, body := range bodies {
			t.Logf("request %d has MCP schema=%v, tool_search=%v", i, strings.Contains(body, "dream_probe"), strings.Contains(body, "tool_search"))
			if at := strings.Index(body, "dream_probe"); at >= 0 {
				start, end := max(0, at-150), min(len(body), at+250)
				t.Logf("request %d MCP descriptor: %s", i, body[start:end])
			}
		}
		t.Fatalf("native MCP call never reached interruptor: %v, requests=%d phase=%s message=%s items=%+v native=%+v", ctx.Err(), got, snapshot.Phase, snapshot.Message, snapshot.Items, thread.Turns)
	}
	for {
		state := s.ConversationState()
		if terminal(state.Status) {
			mu.Lock()
			got := requests
			mu.Unlock()
			t.Logf("native turn status=%s requests=%d thread=%s turn=%s", state.Status, got, state.Session, state.Turn)
			if got != 1 {
				t.Fatalf("native model continued after interrupt: %d requests", got)
			}
			return
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("native turn did not terminate", ctx.Err())
		}
	}
}
