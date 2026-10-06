package codex

import (
	"context"
	"time"
)

// These are content-free observations of the shared native process. Loaded
// threads include other clients; the owned subset is diagnostic, not authority
// to unload a thread or change another connection's subscription.
type nativeResourceSnapshot struct {
	ObservedAt             time.Time
	LoadedThreads          int
	LoadedOwnedWorkers     int
	LiveThreads            *uint64
	MCPConnections         *uint64
	PhysicalFootprintBytes *uint64
}

func (s *Session) observeNativeResources(c *Client, epoch uint64) {
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.life, 3*time.Second)
	defer cancel()
	var loaded struct {
		Data []string `json:"data"`
	}
	if err := callDecode(ctx, c, "thread/loaded/list", map[string]any{}, &loaded); err != nil {
		return // Optional native diagnostics must never affect work admission.
	}
	var native struct {
		Process struct {
			PhysicalFootprintBytes *uint64 `json:"physicalFootprintBytes"`
		} `json:"process"`
		Gauges []struct {
			Name  string `json:"name"`
			Value uint64 `json:"value"`
		} `json:"gauges"`
	}
	_ = callDecode(ctx, c, "server/diagnostics", map[string]any{}, &native)
	snapshot := nativeResourceSnapshot{ObservedAt: time.Now().UTC(), LoadedThreads: len(loaded.Data), PhysicalFootprintBytes: native.Process.PhysicalFootprintBytes}
	for _, gauge := range native.Gauges {
		switch gauge.Name {
		case "core.threads.live":
			value := gauge.Value
			snapshot.LiveThreads = &value
		case "mcp.connections.live":
			value := gauge.Value
			snapshot.MCPConnections = &value
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != c || s.epoch != epoch || s.closed {
		return
	}
	for _, id := range loaded.Data {
		if s.children[id] {
			snapshot.LoadedOwnedWorkers++
		}
	}
	s.nativeResources = snapshot
}
