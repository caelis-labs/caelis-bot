package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// ProbeServer reads the tool directory through the same reviewed Bot relay
// used by both Runtimes. It is invoked only from an explicit enabled-service
// detail view. No tools/call is sent and no model session is created.
func (m *Manager) ProbeServer(ctx context.Context, packageID, name string) ServerDetail {
	selected := SelectedServer{}
	for _, server := range m.Selection().Servers {
		if server.PackageID == packageID && server.Name == name {
			selected = server
			break
		}
	}
	if selected.Root == "" {
		return ServerDetail{State: "not_configured", Tools: []Tool{}}
	}
	if selected.Server.Type != "streamable-http" && selected.Server.Type != "stdio" {
		return ServerDetail{State: "not_started", Tools: []Tool{}}
	}
	// Mutations replace m.state maps. The relay gets a fixed generation so a
	// concurrent update cannot race its in-memory state reads.
	m.mu.Lock()
	copyState := m.state
	copyState.Installed = make(map[string]installed, len(m.state.Installed))
	for key, value := range m.state.Installed {
		copyState.Installed[key] = value
	}
	copyState.Connections = make(map[string]connectionRecord, len(m.state.Connections))
	for key, value := range m.state.Connections {
		copyState.Connections[key] = value
	}
	m.mu.Unlock()
	probe := &Manager{root: m.root, catalog: m.catalog, state: copyState, secrets: m.secrets}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	closePipes := func(err error) {
		inWriter.CloseWithError(err)
		inReader.CloseWithError(err)
		outReader.CloseWithError(err)
		outWriter.CloseWithError(err)
	}
	stopClose := context.AfterFunc(probeCtx, func() { closePipes(probeCtx.Err()) })
	done := make(chan error, 1)
	go func() {
		err := runStdio(probeCtx, probe, packageID, name, inReader, outWriter, io.Discard,
			filepath.Base(selected.Root), strconv.FormatUint(selected.ConnectionRevision, 10))
		outWriter.CloseWithError(err)
		done <- err
	}()
	defer func() {
		cancel()
		closePipes(io.EOF)
		<-done
		stopClose()
	}()
	encoder := json.NewEncoder(inWriter)
	reader := bufio.NewScanner(outReader)
	reader.Buffer(make([]byte, 64<<10), 16<<20)
	exchange := func(id int, method string, params any) (json.RawMessage, string, error) {
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			return nil, "", err
		}
		for reader.Scan() {
			var response struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(reader.Bytes(), &response) != nil || response.ID != id {
				continue
			}
			if response.Error != nil {
				return nil, response.Error.Message, errors.New("MCP request failed")
			}
			return response.Result, "", nil
		}
		if err := probeCtx.Err(); err != nil {
			return nil, "", err
		}
		if err := reader.Err(); err != nil {
			return nil, "", err
		}
		return nil, "", errors.New("MCP response unavailable")
	}
	if _, message, err := exchange(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "Caelis Bot", "version": "1"}}); err != nil {
		return probeFailure(message)
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return probeFailure("")
	}
	detail := ServerDetail{State: "connected", Tools: []Tool{}}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 8 && len(detail.Tools) < 512; page++ {
		params := map[string]string{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, message, err := exchange(page+2, "tools/list", params)
		if err != nil {
			return probeFailure(message)
		}
		var listing struct {
			Tools []struct {
				Name, Title, Description string
				Annotations              struct {
					ReadOnlyHint    *bool `json:"readOnlyHint"`
					DestructiveHint *bool `json:"destructiveHint"`
					IdempotentHint  *bool `json:"idempotentHint"`
					OpenWorldHint   *bool `json:"openWorldHint"`
				}
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if json.Unmarshal(result, &listing) != nil {
			return probeFailure("")
		}
		if len(listing.Tools) > 512-len(detail.Tools) {
			detail.Truncated = true
		}
		for _, raw := range listing.Tools {
			if len(detail.Tools) >= 512 {
				break
			}
			if name := SafeDisplayText(raw.Name); name != "" && !seen[name] {
				seen[name] = true
				detail.Tools = append(detail.Tools, Tool{Name: name, Title: SafeDisplayText(raw.Title), Description: SafeDisplayDescription(raw.Description), ReadOnlyHint: raw.Annotations.ReadOnlyHint, DestructiveHint: raw.Annotations.DestructiveHint, IdempotentHint: raw.Annotations.IdempotentHint, OpenWorldHint: raw.Annotations.OpenWorldHint})
			}
		}
		if listing.NextCursor == "" {
			return finishProbe(detail)
		}
		if len(listing.NextCursor) > 1024 || listing.NextCursor == cursor {
			return probeFailure("")
		}
		cursor = listing.NextCursor
	}
	detail.Truncated = true
	return finishProbe(detail)
}

func finishProbe(detail ServerDetail) ServerDetail {
	sort.Slice(detail.Tools, func(i, j int) bool { return detail.Tools[i].Name < detail.Tools[j].Name })
	if len(detail.Tools) > 128 {
		detail.Truncated = true
		detail.Tools = detail.Tools[:128]
	}
	return detail
}

func probeFailure(message string) ServerDetail {
	state := "failed"
	if message == "MCP authentication failed" {
		state = "authentication_required"
	}
	return ServerDetail{State: state, Tools: []Tool{}}
}
