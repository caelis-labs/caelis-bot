package bot

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// A private, per-launch Unix endpoint connects the owned stdio MCP process to
// the resident host. No HTTP port, user-wide config edit or shell execution.
type Bridge struct {
	listener   net.Listener
	dir, token string
	done       chan struct{}
	wg         sync.WaitGroup
}
type toolRequest struct {
	Token     string          `json:"token"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
type toolResult struct {
	Content []map[string]string `json:"content"`
	IsError bool                `json:"isError"`
}

func result(value any, err error) toolResult {
	text := ""
	if err != nil {
		text = err.Error()
	} else {
		b, _ := json.Marshal(value)
		text = string(b)
	}
	return toolResult{[]map[string]string{{"type": "text", "text": text}}, err != nil}
}
func (r *Runtime) call(name string, args json.RawMessage) toolResult {
	switch name {
	case "bot_clock":
		return result(r.Clock(), nil)
	case "bot_reminders":
		var in struct {
			Operation string `json:"operation"`
			Schedule
		}
		if json.Unmarshal(args, &in) != nil {
			return result(nil, errors.New("无效的提醒参数"))
		}
		switch in.Operation {
		case "list":
			return result(r.State(), nil)
		case "save":
			s, e := r.Upsert(in.Schedule)
			return result(s, e)
		case "remove":
			return result("已移除", r.Remove(in.ID))
		}
	case "bot_gesture":
		var in struct {
			Action string `json:"action"`
		}
		if json.Unmarshal(args, &in) != nil {
			return result(nil, errors.New("无效的动作参数"))
		}
		return result("已交给角色；隐藏时不强制显示", r.Perform(in.Action))
	}
	return result(nil, errors.New("不支持的 Bot 操作"))
}
func Serve(r *Runtime) (*Bridge, error) {
	dir, e := os.MkdirTemp("/tmp", "caelis-bot-")
	if e != nil {
		return nil, e
	}
	listener, e := net.Listen("unix", filepath.Join(dir, "tools.sock"))
	if e != nil {
		os.RemoveAll(dir)
		return nil, e
	}
	if e = os.Chmod(filepath.Join(dir, "tools.sock"), 0600); e != nil {
		listener.Close()
		os.RemoveAll(dir)
		return nil, e
	}
	b := &Bridge{listener: listener, dir: dir, token: rand.Text(), done: make(chan struct{})}
	go func() {
		defer close(b.done)
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			b.wg.Add(1)
			go func() {
				defer b.wg.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				var req toolRequest
				if json.NewDecoder(io.LimitReader(conn, 128*1024)).Decode(&req) != nil {
					return
				}
				if subtle.ConstantTimeCompare([]byte(req.Token), []byte(b.token)) != 1 {
					_ = json.NewEncoder(conn).Encode(result(nil, errors.New("拒绝未授权调用")))
					return
				}
				_ = json.NewEncoder(conn).Encode(r.call(req.Name, req.Arguments))
			}()
		}
	}()
	return b, nil
}
func (b *Bridge) Config(executable string) map[string]any {
	return map[string]any{"command": executable, "args": []string{"--bot-tools"}, "env": map[string]string{"CAELIS_BOT_ENDPOINT": filepath.Join(b.dir, "tools.sock"), "CAELIS_BOT_TOKEN": b.token}, "tools": map[string]any{"bot_clock": map[string]string{"approval_mode": "approve"}, "bot_reminders": map[string]string{"approval_mode": "approve"}, "bot_gesture": map[string]string{"approval_mode": "approve"}}, "startup_timeout_sec": 10, "tool_timeout_sec": 15}
}
func (b *Bridge) Close() { _ = b.listener.Close(); <-b.done; b.wg.Wait(); _ = os.RemoveAll(b.dir) }
func toolSpecs() []any {
	str := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	schema := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	return []any{
		map[string]any{"name": "bot_clock", "description": "Read local time and the resident scheduling boundary before creating reminders.", "inputSchema": schema(map[string]any{})},
		map[string]any{"name": "bot_reminders", "description": "List, save or remove user-requested reminders. Save uses a stable id (letters, digits, hyphen, underscore), making identical retries idempotent. Choose one of at (RFC3339), everyMinutes, or daily (HH:MM) with an IANA timeZone. App must remain running. Sleeping occurrences coalesce; quit pauses missed execution. Do not use shell sleep or external schedulers.", "inputSchema": schema(map[string]any{"operation": map[string]any{"type": "string", "enum": []string{"list", "save", "remove"}}, "id": str("Stable reminder identifier"), "label": str("Short user-facing title"), "prompt": str("Self-contained instruction to execute on activation"), "at": str("One-off timestamp with UTC offset"), "everyMinutes": map[string]any{"type": "integer", "minimum": 1, "maximum": 10080}, "daily": str("Daily local HH:MM"), "timeZone": str("IANA time zone, such as Asia/Shanghai")}, "operation")},
		map[string]any{"name": "bot_gesture", "description": "Briefly animate the desktop companion for feedback. Respects hidden state and reduced motion. Does not grant approval, move windows, steal focus, or execute other actions.", "inputSchema": schema(map[string]any{"action": map[string]any{"type": "string", "enum": []string{"attention", "nod", "celebrate"}}}, "action")},
	}
}

// RunStdio implements the small MCP 2025-06-18 tools subset used by the pinned
// Codex client. stdout is protocol-only; invalid/oversized input fails closed.
func RunStdio(in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 128*1024)
	enc := json.NewEncoder(out)
	initialized := false
	for scan.Scan() {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &req) != nil || req.JSONRPC != "2.0" {
			return errors.New("invalid MCP frame")
		}
		if len(req.ID) == 0 {
			if req.Method == "notifications/initialized" {
				initialized = true
			}
			continue
		}
		var value any
		var rpcError any
		switch req.Method {
		case "initialize":
			value = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "caelis-bot", "version": "0.1.0"}}
		case "ping":
			value = map[string]any{}
		default:
			if !initialized {
				rpcError = map[string]any{"code": -32000, "message": "Initialize first"}
				break
			}
			switch req.Method {
			case "tools/list":
				value = map[string]any{"tools": toolSpecs()}
			case "tools/call":
				var call toolRequest
				if json.Unmarshal(req.Params, &call) != nil {
					rpcError = map[string]any{"code": -32602, "message": "Invalid tool arguments"}
					break
				}
				call.Token = os.Getenv("CAELIS_BOT_TOKEN")
				value = forward(os.Getenv("CAELIS_BOT_ENDPOINT"), call)
			default:
				rpcError = map[string]any{"code": -32601, "message": "Method not supported"}
			}
		}
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcError != nil {
			response["error"] = rpcError
		} else {
			response["result"] = value
		}
		if e := enc.Encode(response); e != nil {
			return e
		}
	}
	return scan.Err()
}
func forward(endpoint string, req toolRequest) toolResult {
	if endpoint == "" || req.Token == "" {
		return result(nil, errors.New("Bot 工具连接未配置"))
	}
	conn, e := net.DialTimeout("unix", endpoint, 3*time.Second)
	if e != nil {
		return result(nil, errors.New("Bot 应用暂不可用"))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if e = json.NewEncoder(conn).Encode(req); e != nil {
		return result(nil, errors.New("Bot 请求未确认"))
	}
	var out toolResult
	if e = json.NewDecoder(io.LimitReader(conn, 128*1024)).Decode(&out); e != nil {
		return result(nil, errors.New("Bot 请求结果未确认，请先查询提醒列表"))
	}
	return out
}
