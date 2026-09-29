package desktopcontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Driver supervises one pinned SDK host over inherited pipes. It
// has no listener, restart or arbitrary tool passthrough. Only its owner gets
// these methods; worker environments never receive the channel or configuration.
type Driver struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Reader
	closed bool
	focus  WindowFocuser
	exited chan struct{}
}

// WindowFocuser is an OS port supplied by the native shell, not by tool input.
type WindowFocuser func(context.Context, int, uint64) error

func StartDriver(node, script string, focus ...WindowFocuser) (*Driver, error) {
	if !filepath.IsAbs(node) || !filepath.IsAbs(script) {
		return nil, errors.New("desktop helper requires explicit absolute Node and host paths")
	}
	cmd := exec.Command(node, "--jitless", script)
	// No Bot MCP credentials, model tokens or Runtime configuration inheritance.
	for _, key := range []string{"HOME", "PATH", "TMPDIR", "SystemRoot", "WINDIR", "USERPROFILE", "LOCALAPPDATA"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	// The private pipe carries the native PNG before lossless optimization or
	// same-size JPEG encoding. The model-visible content remains below 256 KiB.
	d := &Driver{cmd: cmd, input: input, output: bufio.NewReaderSize(output, 8<<20), exited: make(chan struct{})}
	if len(focus) > 0 {
		d.focus = focus[0]
	}
	go func() { _ = cmd.Wait(); close(d.exited) }()
	return d, nil
}

func (d *Driver) Definitions() []api.ToolDefinition { return SemanticDefinitions() }

func driverError(message string) api.ToolResult {
	return api.ToolResult{IsError: true, Content: []map[string]string{{"type": "text", "text": message}}}
}

type turnKey struct{}

// WithTurn is private-channel metadata supplied by the native Bot lifecycle, not
// a model argument. Changing it invalidates every app grant in the helper.
func WithTurn(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, turnKey{}, id)
}

func (d *Driver) CallTool(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
	if name != "bot_desktop_observe" && name != "bot_desktop_authorize" && name != "bot_desktop_perform" {
		return driverError("unsupported desktop tool")
	}
	if len(args) > 65536 || !json.Valid(args) {
		return driverError("invalid desktop arguments")
	}
	if err := ctx.Err(); err != nil {
		return driverError("desktop call cancelled before dispatch")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ctx.Err() != nil {
		return driverError("desktop call cancelled before dispatch")
	}
	if d.closed {
		return driverError("desktop driver unavailable; no automatic restart or replay")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	type response struct {
		result api.ToolResult
		err    error
	}
	done := make(chan response, 1)
	go func() {
		err := json.NewEncoder(d.input).Encode(struct {
			Turn                 string          `json:"turn"`
			Name                 string          `json:"name"`
			Arguments            json.RawMessage `json:"arguments"`
			NativeFocusAvailable bool            `json:"nativeFocusAvailable"`
		}{turnID(ctx), name, args, d.focus != nil})
		var out api.ToolResult
		if err == nil {
			var line []byte
			line, err = d.output.ReadSlice('\n')
			if err == nil {
				var callback struct {
					Focus *struct {
						ID       string `json:"id"`
						PID      int    `json:"pid"`
						WindowID uint64 `json:"windowId,string"`
					} `json:"nativeFocus"`
				}
				err = json.Unmarshal(line, &callback)
				if err == nil && callback.Focus != nil {
					f := callback.Focus
					var input struct{ Steps []struct{ Op, Target string } }
					_ = json.Unmarshal(args, &input)
					if name != "bot_desktop_perform" || len(input.Steps) == 0 || input.Steps[0].Op != "focus" || input.Steps[0].Target != "window" || f.PID <= 0 || f.WindowID == 0 || len(f.ID) != 36 {
						err = errors.New("unexpected desktop focus request")
					} else {
						ok := ctx.Err() == nil && d.focus != nil && d.focus(ctx, f.PID, f.WindowID) == nil
						if ctx.Err() != nil {
							err = ctx.Err()
						} else {
							err = json.NewEncoder(d.input).Encode(map[string]any{"focusResult": f.ID, "ok": ok})
						}
						if err == nil {
							line, err = d.output.ReadSlice('\n')
						}
					}
				}
			}
			if err == nil {
				err = json.Unmarshal(line, &out)
				if err == nil && len(out.Content) == 0 {
					err = errors.New("empty desktop response")
				}
			}
			if err == nil {
				err = boundImages(&out)
			}
		}
		done <- response{out, err}
	}()
	select {
	case value := <-done:
		if value.err == nil && ctx.Err() == nil {
			return value.result
		}
		d.abortLocked()
		return driverError("desktop result unknown; observe through a new explicit run, never repeat the action automatically")
	case <-ctx.Done():
		d.abortLocked()
		<-done
		return driverError("desktop result unknown after timeout or cancellation; do not repeat the action")
	}
}

// Cancellation/transport loss cannot use the graceful EOF path: the JS host
// may still be awaiting a preflight read and dispatch input when it completes.
// Kill before closing stdin so EOF cannot resume that pending request.
func (d *Driver) abortLocked() {
	if d.closed {
		return
	}
	d.closed = true
	_ = d.cmd.Process.Kill()
	_ = d.input.Close()
	<-d.exited
}

func (d *Driver) closeLocked() {
	if d.closed {
		return
	}
	d.closed = true
	_ = d.input.Close()
	select {
	case <-d.exited:
	case <-time.After(time.Second):
		_ = d.cmd.Process.Kill()
		<-d.exited
	}
}
func (d *Driver) Close() { d.mu.Lock(); defer d.mu.Unlock(); d.closeLocked() }

func turnID(ctx context.Context) string { id, _ := ctx.Value(turnKey{}).(string); return id }
