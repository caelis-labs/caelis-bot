//go:build workerpeere2e

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type nativeMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *nativeError    `json:"error,omitempty"`
}
type nativeError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type nativePeer struct {
	mu      sync.Mutex
	encode  *json.Encoder
	threads map[string]bool // protected by nativeServer.mu
}

func (p *nativePeer) send(m nativeMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	m.JSONRPC = "2.0"
	return p.encode.Encode(m)
}
func raw(v any) json.RawMessage { data, _ := json.Marshal(v); return data }

func (s *nativeServer) serve(in io.Reader, out io.Writer) error {
	p := &nativePeer{encode: json.NewEncoder(out), threads: map[string]bool{}}
	s.mu.Lock()
	s.peers[p] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.peers, p); s.mu.Unlock() }()
	dec := json.NewDecoder(in)
	for {
		var m nativeMessage
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if m.Method == "" {
			s.answer(m)
			continue
		}
		if len(m.ID) == 0 {
			continue
		}
		value, after, err := s.request(p, m)
		reply := nativeMessage{ID: m.ID, Result: raw(value)}
		if err != nil {
			reply.Result = nil
			reply.Error = &nativeError{Code: -32602, Message: err.Error()}
		}
		if err := p.send(reply); err != nil {
			return err
		}
		if after != nil && err == nil {
			go after()
		}
	}
}

func (s *nativeServer) request(peer *nativePeer, m nativeMessage) (any, func(), error) {
	var p struct {
		ThreadID string                     `json:"threadId"`
		TurnID   string                     `json:"turnId"`
		Expected string                     `json:"expectedTurnId"`
		Cwd      string                     `json:"cwd"`
		ClientID string                     `json:"clientUserMessageId"`
		Input    []nativeInput              `json:"input"`
		Config   map[string]json.RawMessage `json:"config"`
		Cursor   string                     `json:"cursor"`
		Limit    int                        `json:"limit"`
		Sort     string                     `json:"sortDirection"`
		View     string                     `json:"itemsView"`
	}
	if len(m.Params) > 0 && json.Unmarshal(m.Params, &p) != nil {
		return nil, nil, errors.New("invalid fixture native params")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Methods[m.Method]++
	defer s.saveLocked()
	switch m.Method {
	case "initialize":
		return map[string]any{"userAgent": fixtureBuild, "codexHome": filepath.Dir(s.path)}, nil, nil
	case "account/read":
		return map[string]any{"account": map[string]string{"type": "apiKey"}, "requiresOpenaiAuth": false}, nil, nil
	case "model/list":
		return map[string]any{"data": []any{map[string]any{"model": "worker-peer-synthetic", "displayName": "Synthetic E2E model", "isDefault": true, "inputModalities": []string{"text", "image"}, "defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}, "serviceTiers": []any{}}}, "nextCursor": nil}, nil, nil
	case "skills/list", "thread/backgroundTerminals/list", "thread/list", "thread/loaded/list":
		return map[string]any{"data": []any{}, "nextCursor": nil}, nil, nil
	case "config/read":
		cwd := p.Cwd
		if cwd == "" {
			cwd = s.cwd
		}
		return map[string]any{"config": map[string]any{"model": "worker-peer-synthetic", "model_provider": "fixture", "projects": map[string]any{cwd: map[string]string{"trust_level": "trusted"}}}, "layers": []any{}}, nil, nil
	case "config/batchWrite", "config/value/write", "config/mcpServer/reload", "thread/unsubscribe", "thread/backgroundTerminals/clean":
		return map[string]any{}, nil, nil
	case "thread/start", "thread/resume", "thread/read":
		thread := s.state.Threads[p.ThreadID]
		if m.Method == "thread/start" {
			s.state.Next++
			thread = &nativeThread{ID: fmt.Sprintf("fixture-thread-%s-%d", s.state.Instance[:8], s.state.Next), Cwd: p.Cwd, Turns: []nativeTurn{}}
			thread.Status.Type = "idle"
			s.state.Threads[thread.ID] = thread
		}
		if thread == nil {
			return nil, nil, errors.New("original fixture thread not found")
		}
		if m.Method != "thread/read" {
			peer.threads[thread.ID] = true
			if spec, ok := p.Config["mcp_servers.caelis_tasks"]; ok {
				var config mcpConfig
				if json.Unmarshal(spec, &config) == nil && (config.Enabled == nil || *config.Enabled) && config.Command != "" {
					thread.Resident = true
					s.services[thread.ID] = config
				}
			}
		}
		return map[string]any{"thread": copyJSON(thread), "model": "worker-peer-synthetic", "modelProvider": "fixture", "reasoningEffort": "medium"}, nil, nil
	case "thread/turns/list":
		thread := s.state.Threads[p.ThreadID]
		if thread == nil {
			return nil, nil, errors.New("original fixture thread not found")
		}
		turns := copyJSON(thread.Turns)
		if p.Sort == "desc" {
			for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
				turns[i], turns[j] = turns[j], turns[i]
			}
		}
		start := 0
		if p.Cursor != "" {
			start, _ = strconv.Atoi(p.Cursor)
		}
		if start < 0 || start > len(turns) {
			return nil, nil, errors.New("invalid fixture history cursor")
		}
		limit := p.Limit
		if limit <= 0 {
			limit = 20
		}
		end := min(start+limit, len(turns))
		var next any
		if end < len(turns) {
			next = strconv.Itoa(end)
		}
		return map[string]any{"data": turns[start:end], "nextCursor": next}, nil, nil
	case "turn/start", "turn/steer":
		thread := s.state.Threads[p.ThreadID]
		if thread == nil {
			return nil, nil, errors.New("original fixture thread not found")
		}
		if m.Method == "turn/steer" {
			if len(thread.Turns) == 0 || thread.Turns[len(thread.Turns)-1].ID != p.Expected || thread.Turns[len(thread.Turns)-1].Status != "inProgress" {
				return nil, nil, errors.New("original fixture steer target changed")
			}
			return map[string]any{"turnId": p.Expected}, nil, nil
		}
		if thread.Status.Type == "active" {
			return nil, nil, errors.New("fixture thread already has an active turn")
		}
		s.state.Next++
		turn := nativeTurn{ID: fmt.Sprintf("fixture-turn-%d", s.state.Next), Status: "inProgress", Items: []nativeItem{{ID: "user-" + p.ClientID, Type: "userMessage", ClientID: p.ClientID, Content: p.Input}}}
		thread.Turns = append(thread.Turns, turn)
		thread.Status.Type = "active"
		s.state.ClientInputs[p.ClientID]++ // Count, never silently deduplicate writes.
		s.state.Effects["turn/start:"+thread.ID]++
		id, turnID, input, resident := thread.ID, turn.ID, copyJSON(p.Input), thread.Resident
		return map[string]any{"turn": turn}, func() { s.runTurn(id, turnID, input, resident) }, nil
	case "turn/interrupt":
		thread := s.state.Threads[p.ThreadID]
		if thread == nil || len(thread.Turns) == 0 || thread.Turns[len(thread.Turns)-1].ID != p.TurnID || thread.Turns[len(thread.Turns)-1].Status != "inProgress" {
			return nil, nil, errors.New("original fixture interrupt target changed")
		}
		s.state.Effects["turn/interrupt:"+p.ThreadID+":"+p.TurnID]++
		id, turn := p.ThreadID, p.TurnID
		return map[string]any{}, func() { s.completeTurn(id, turn, "interrupted", "Fixture turn interrupted") }, nil
	}
	return nil, nil, fmt.Errorf("unsupported fixture native method: %s", m.Method)
}

func (s *nativeServer) emit(thread, method string, params any) {
	s.mu.Lock()
	var peers []*nativePeer
	for peer := range s.peers {
		if peer.threads[thread] {
			peers = append(peers, peer)
		}
	}
	s.mu.Unlock()
	for _, peer := range peers {
		_ = peer.send(nativeMessage{Method: method, Params: raw(params)})
	}
}
func (s *nativeServer) activeLocked(thread, turn string) bool {
	t := s.state.Threads[thread]
	return t != nil && len(t.Turns) > 0 && t.Turns[len(t.Turns)-1].ID == turn && t.Turns[len(t.Turns)-1].Status == "inProgress"
}
func (s *nativeServer) item(thread, turn string, item nativeItem, completed bool) {
	s.mu.Lock()
	if !s.activeLocked(thread, turn) {
		s.mu.Unlock()
		return
	}
	t := s.state.Threads[thread]
	items := &t.Turns[len(t.Turns)-1].Items
	found := false
	for i := range *items {
		if (*items)[i].ID == item.ID {
			(*items)[i], found = item, true
			break
		}
	}
	if !found {
		*items = append(*items, item)
	}
	_ = s.saveLocked()
	s.mu.Unlock()
	method := "item/started"
	if completed {
		method = "item/completed"
	}
	s.emit(thread, method, map[string]any{"threadId": thread, "turnId": turn, "item": item})
}
func (s *nativeServer) completeTurn(thread, turn, status, text string) {
	if text != "" {
		s.item(thread, turn, nativeItem{ID: "answer-" + turn, Type: "agentMessage", Text: text, Status: "completed"}, true)
	}
	s.mu.Lock()
	if !s.activeLocked(thread, turn) {
		s.mu.Unlock()
		return
	}
	t := s.state.Threads[thread]
	t.Turns[len(t.Turns)-1].Status, t.Status.Type = status, "idle"
	if cancel := s.turnCancel[turn]; cancel != nil {
		cancel()
	}
	value := copyJSON(t.Turns[len(t.Turns)-1])
	_ = s.saveLocked()
	s.changedLocked()
	s.mu.Unlock()
	s.emit(thread, "turn/completed", map[string]any{"threadId": thread, "turn": value})
}

func inputText(input []nativeInput) string {
	var text []string
	for _, part := range input {
		if part.Type == "text" {
			text = append(text, part.Text)
		}
	}
	return strings.Join(text, "\n")
}
