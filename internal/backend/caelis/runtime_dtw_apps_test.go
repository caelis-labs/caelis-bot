package caelis

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDTWReturnedApp(t *testing.T) {
	// Exact call/output frames from the retained real read-only MCP run. The
	// payload has only the task-owned static probe, not user app/window data.
	raw, err := os.ReadFile("testdata/runtime-dtw-apps.json")
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if err = json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	const app, tool = "dev.caelis.bot.runtimeprobe", "desktop_world__apps"
	cases := []struct {
		name   string
		change func(map[string]any, map[string]any, map[string]any)
		want   bool
	}{
		{name: "preserved real output", want: true},
		{name: "query only", change: func(r, call, output map[string]any) { r["input"] = []any{call} }},
		{name: "empty apps with echoed query", change: func(r, call, output map[string]any) {
			output["output"] = `{"apps":[],"query":"` + app + `"}`
		}},
		{name: "app outside returned app field", change: func(r, call, output map[string]any) {
			output["output"] = `{"apps":[{"name":"` + app + `"}],"app":"` + app + `"}`
		}},
		{name: "different returned app", change: func(r, call, output map[string]any) {
			output["output"] = `{"apps":[{"app":"other.app"}]}`
		}},
		{name: "different call id", change: func(r, call, output map[string]any) { output["call_id"] = "unrelated" }},
		{name: "different actual tool", change: func(r, call, output map[string]any) {
			call["arguments"] = `{"tool_name":"desktop_world__exec","arguments":{"query":"` + app + `"}}`
		}},
		{name: "different native call", change: func(r, call, output map[string]any) { call["name"] = "Skill" }},
		{name: "output quoted in message", change: func(r, call, output map[string]any) {
			r["input"] = []any{call, map[string]any{"type": "message", "content": []any{output}}}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := clone(original)
			items := request["input"].([]any)
			call, output := items[0].(map[string]any), items[1].(map[string]any)
			if test.change != nil {
				test.change(request, call, output)
			}
			got := dtwReturnedApp([]map[string]any{request}, tool, app)
			if (got != "") != test.want || (test.want && got != call["call_id"]) {
				t.Fatalf("paired returned app call = %q, want found=%v", got, test.want)
			}
		})
	}
}

func TestDTWReturnedAppKeepsToolIdentity(t *testing.T) {
	const app = "dev.caelis.bot.runtimeprobe"
	for _, recorded := range []string{"desktop_world__apps", "dtw__apps"} {
		for _, requested := range []string{"desktop_world__apps", "dtw__apps"} {
			t.Run(recorded+"/"+requested, func(t *testing.T) {
				args, err := json.Marshal(map[string]any{"tool_name": recorded, "arguments": map[string]string{"query": app}})
				if err != nil {
					t.Fatal(err)
				}
				request := map[string]any{"input": []any{
					map[string]any{"type": "function_call", "name": "mcp_call", "call_id": "original-call", "arguments": string(args)},
					map[string]any{"type": "function_call_output", "call_id": "original-call", "output": `{"apps":[{"app":"` + app + `"}]}`},
				}}
				got := dtwReturnedApp([]map[string]any{request}, requested, app)
				want := ""
				if recorded == requested {
					want = "original-call"
				}
				if got != want {
					t.Fatalf("recorded %q matched requested %q as %q, want %q", recorded, requested, got, want)
				}
			})
		}
	}
}

func TestDTWReturnedAppSavedRequests(t *testing.T) {
	path := os.Getenv("CAELIS_BOT_TEST_DTW_SAVED_REQUESTS")
	if path == "" {
		t.Skip("set a retained model-requests.json for read-only response validation")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string][]map[string]any
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	requests := saved["CASE_DTW_RUNTIME_RESIDENT"]
	const app, tool = "dev.caelis.bot.runtimeprobe", "desktop_world__apps"
	call := dtwReturnedApp(requests, tool, app)
	if call == "" {
		t.Fatal("original real apps output did not match")
	}
	removed := 0
	for _, request := range requests {
		items, _ := request["input"].([]any)
		var keep []any
		for _, item := range items {
			if message, ok := item.(map[string]any); ok && message["type"] == "function_call_output" {
				removed++
				continue
			}
			keep = append(keep, item)
		}
		request["input"] = keep
	}
	if removed == 0 || dtwReturnedApp(requests, tool, app) != "" {
		t.Fatal("query/history without original function output was accepted")
	}
	t.Logf("original call %s accepted; %d function outputs removed, query-only requests rejected; no requests replayed", call, removed)
}
