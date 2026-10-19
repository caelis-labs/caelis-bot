package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strconv"
)

// EventBindings returns stable selected-server identities, never credentials.
// Registration review grants only the exact tool/arguments pinned to this key.
func (m *Manager) EventBindings() map[string]string {
	out := map[string]string{}
	for _, s := range m.Selection().Servers {
		// Desktop input is not a background collector.
		if s.PackageID == "desktop-world" {
			continue
		}
		out[s.PackageID+"/"+s.Name] = filepath.Base(s.Root) + ":" + strconv.FormatUint(s.ConnectionRevision, 10)
	}
	return out
}

// CollectEvent uses the existing generation-fenced relay; no new credential or
// MCP configuration authority is created. One invocation, no transport replay.
func (m *Manager) CollectEvent(ctx context.Context, server, binding, tool string, args map[string]any) (json.RawMessage, error) {
	var selected SelectedServer
	for _, s := range m.Selection().Servers {
		if s.PackageID+"/"+s.Name == server && s.PackageID != "desktop-world" {
			selected = s
			break
		}
	}
	if selected.Root == "" || m.EventBindings()[server] != binding {
		return nil, eventCollectionFailure("event tool connection changed or unavailable")
	}
	m.mu.Lock()
	snapshot := cloneState(m.state)
	m.mu.Unlock()
	fixed := &Manager{root: m.root, catalog: m.catalog, state: snapshot, secrets: m.secrets}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	runCtx, cancel := context.WithCancel(ctx)
	closePipes := func() { _ = inW.Close(); _ = inR.Close(); _ = outR.Close(); _ = outW.Close() }
	stop := context.AfterFunc(runCtx, closePipes)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := runStdio(runCtx, fixed, selected.PackageID, selected.Name, inR, outW, io.Discard, filepath.Base(selected.Root), strconv.FormatUint(selected.ConnectionRevision, 10))
		_ = outW.CloseWithError(err)
	}()
	defer func() { cancel(); closePipes(); <-done; stop() }()
	enc := json.NewEncoder(inW)
	scan := bufio.NewScanner(outR)
	scan.Buffer(make([]byte, 4096), 128<<10)
	exchange := func(id int, method string, params any) (json.RawMessage, error) {
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			return nil, err
		}
		for scan.Scan() {
			var r struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(scan.Bytes(), &r) != nil {
				return nil, errors.New("invalid collector response")
			}
			if r.ID != id {
				continue
			}
			if len(r.Error) > 0 {
				return nil, eventCollectionFailure("collector request failed")
			}
			return r.Result, nil
		}
		return nil, errors.New("collector result unconfirmed")
	}
	if _, err := exchange(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "Caelis Bot event collector", "version": "1"}}); err != nil {
		return nil, err
	}
	if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return nil, err
	}
	raw, err := exchange(2, "tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return nil, err
	}
	var result struct {
		IsError    bool            `json:"isError"`
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &result) != nil || result.IsError {
		return nil, eventCollectionFailure("collector returned an error")
	}
	if len(result.Structured) > 0 && string(result.Structured) != "null" {
		return result.Structured, nil
	}
	if len(result.Content) == 1 && result.Content[0].Type == "text" && json.Valid([]byte(result.Content[0].Text)) {
		return json.RawMessage(result.Content[0].Text), nil
	}
	return nil, eventCollectionFailure("collector needs structured JSON output; use a script adapter for text results")
}

type eventCollectionFailure string

func (e eventCollectionFailure) Error() string        { return string(e) }
func (eventCollectionFailure) ConfirmedFailure() bool { return true }
