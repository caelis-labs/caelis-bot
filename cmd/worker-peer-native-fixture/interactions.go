//go:build workerpeere2e

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *nativeServer) startURL() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.urlBase = "http://" + ln.Addr().String()
	server := &http.Server{Handler: http.HandlerFunc(s.challenge), ReadHeaderTimeout: 3 * time.Second}
	s.urlClose = func() { _ = server.Close() }
	go func() { _ = server.Serve(ln) }()
	return nil
}

var challengeHTML = template.Must(template.New("challenge").Parse(`<!doctype html><meta charset="utf-8"><title>Synthetic native challenge</title><h1>Synthetic native challenge</h1><p>Viewing this page does not authorize or complete anything.</p><form method="post"><input type="hidden" name="nonce" value="{{.}}"><button type="submit">Complete synthetic challenge</button></form>`))

func (s *nativeServer) challenge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Cache-Control", "no-store")
	// Keep same-origin browser form POSTs attributable to this challenge.
	// no-referrer serializes their Origin as null in Chromium.
	w.Header().Set("Referrer-Policy", "same-origin")
	if r.Host != strings.TrimPrefix(s.urlBase, "http://") {
		http.Error(w, "fixture native Host mismatch", 403)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/challenge/")
	s.mu.Lock()
	p := s.state.Pending[id]
	if p == nil || p.Nonce == "" || !s.activeLocked(p.Thread, p.Turn) || p.Action == "decline" || p.Action == "cancel" {
		s.mu.Unlock()
		http.Error(w, "original native challenge unavailable", 410)
		return
	}
	nonce := p.Nonce
	s.mu.Unlock()
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = challengeHTML.Execute(w, nonce) // GET has no state/effect transition.
		return
	}
	if r.Method != http.MethodPost || r.Header.Get("Origin") != s.urlBase {
		http.Error(w, "fixture native Origin mismatch", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil || r.Form.Get("nonce") != nonce {
		http.Error(w, "original challenge nonce mismatch", 403)
		return
	}
	s.mu.Lock()
	p = s.state.Pending[id]
	if p == nil || !s.activeLocked(p.Thread, p.Turn) || p.Action == "decline" || p.Action == "cancel" {
		s.mu.Unlock()
		http.Error(w, "original native challenge unavailable", 410)
		return
	}
	p.PostCount++
	if !p.Completed {
		p.Completed = true
		s.state.Effects["challenge-post:"+id]++
	}
	_ = s.saveLocked()
	s.changedLocked()
	s.mu.Unlock()
	_, _ = fmt.Fprintln(w, "Synthetic challenge recorded by original native owner. Native consent reply is checked separately.")
}

func (s *nativeServer) elicit(thread, turn, kind string) error {
	s.mu.Lock()
	if !s.activeLocked(thread, turn) {
		s.mu.Unlock()
		return errors.New("original fixture turn is no longer active")
	}
	s.state.Next++
	id := fmt.Sprintf("fixture-request-%d", s.state.Next)
	params := map[string]any{"threadId": thread, "turnId": turn, "itemId": "fixture-native-interaction-" + id}
	method := "mcpServer/elicitation/request"
	request := &pending{ID: id, Thread: thread, Turn: turn, Kind: kind}
	switch kind {
	case "question":
		method = "item/tool/requestUserInput"
		params["questions"] = []any{map[string]any{"id": "fixture-answer", "header": "Synthetic question", "question": "Choose a synthetic fixture answer", "isOther": false, "isSecret": false, "options": []any{map[string]string{"label": "Fixture A", "description": "First synthetic option"}, map[string]string{"label": "Fixture B", "description": "Second synthetic option"}}}}
	case "secret", "form":
		params["mode"], params["serverName"], params["message"] = "form", "synthetic-native-mcp", "Synthetic input only; do not enter real credentials"
		property := map[string]any{"type": "string", "title": "Synthetic value"}
		if kind == "secret" {
			property["format"], property["writeOnly"] = "password", true
		} else {
			property["enum"] = []string{"fixture-one", "fixture-two"}
		}
		params["requestedSchema"] = map[string]any{"type": "object", "required": []string{"fixtureValue"}, "properties": map[string]any{"fixtureValue": property}}
	case "opaque":
		method = "item/commandExecution/requestApproval"
		params["command"], params["cwd"] = "echo synthetic-only", s.state.Threads[thread].Cwd
		params["availableDecisions"] = []any{map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []string{"echo", "synthetic-only"}}}, "decline", "cancel"}
	case "url":
		request.Nonce = rand.Text()
		params["mode"], params["serverName"], params["message"] = "url", "synthetic-native-mcp", "Open the synthetic native challenge; accept is consent, not completion"
		params["elicitationId"], params["url"] = "elicitation-"+id, s.urlBase+"/challenge/"+url.PathEscape(id)
	default:
		s.mu.Unlock()
		return errors.New("unsupported synthetic interaction")
	}
	request.Method, request.Params = method, raw(params)
	s.state.Pending[id] = request
	_ = s.saveLocked()
	s.mu.Unlock()
	s.emit(thread, "item/started", map[string]any{"threadId": thread, "turnId": turn, "item": nativeItem{ID: "native-tool-" + id, Type: "mcpToolCall", Server: "synthetic-native-mcp", Tool: "wait_for_original_interaction", Status: "inProgress"}})
	s.mu.Lock()
	var peers []*nativePeer
	for peer := range s.peers {
		if peer.threads[thread] {
			peers = append(peers, peer)
		}
	}
	s.mu.Unlock()
	for _, peer := range peers {
		_ = peer.send(nativeMessage{ID: raw(id), Method: method, Params: raw(params)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for {
		s.mu.Lock()
		p := s.state.Pending[id]
		if !s.activeLocked(thread, turn) {
			s.mu.Unlock()
			return errors.New("original fixture turn interrupted")
		}
		if p.Action == "decline" || p.Action == "cancel" {
			s.mu.Unlock()
			return errors.New("synthetic interaction declined or cancelled; no effect")
		}
		if p.Action == "accept" && (kind != "url" || p.Completed) {
			s.state.Effects["interaction:"+id]++
			_ = s.saveLocked()
			s.mu.Unlock()
			s.item(thread, turn, nativeItem{ID: "native-tool-" + id, Type: "mcpToolCall", Server: "synthetic-native-mcp", Tool: "wait_for_original_interaction", Status: "completed", Result: raw(map[string]any{"originalRequestId": id, "challengeVerified": kind == "url", "syntheticEffect": 1})}, true)
			return nil
		}
		changed := s.changes
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (s *nativeServer) answer(m nativeMessage) {
	var id string
	if json.Unmarshal(m.ID, &id) != nil {
		return
	}
	s.mu.Lock()
	p := s.state.Pending[id]
	if p == nil || p.Action != "" || !s.activeLocked(p.Thread, p.Turn) || m.Error != nil {
		s.mu.Unlock()
		return
	}
	var result struct {
		Action   string                     `json:"action"`
		Decision json.RawMessage            `json:"decision"`
		Answers  map[string]json.RawMessage `json:"answers"`
		Content  map[string]any             `json:"content"`
	}
	if json.Unmarshal(m.Result, &result) != nil {
		s.mu.Unlock()
		return
	}
	action := result.Action
	if p.Method == "item/tool/requestUserInput" {
		var answer struct {
			Answers []string `json:"answers"`
		}
		if json.Unmarshal(result.Answers["fixture-answer"], &answer) == nil && len(answer.Answers) == 1 && (answer.Answers[0] == "Fixture A" || answer.Answers[0] == "Fixture B") {
			action = "accept"
		}
	}
	if action == "accept" && (p.Kind == "form" || p.Kind == "secret") {
		value, _ := result.Content["fixtureValue"].(string)
		if p.Kind == "form" && value != "fixture-one" && value != "fixture-two" || p.Kind == "secret" && value != "fixture-secret" {
			s.mu.Unlock()
			return
		}
	}
	if p.Method == "item/commandExecution/requestApproval" {
		var decision string
		_ = json.Unmarshal(result.Decision, &decision)
		if decision == "decline" || decision == "cancel" {
			action = decision
		} else if string(result.Decision) == string(raw(map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []string{"echo", "synthetic-only"}}})) {
			action = "accept"
		}
	}
	if action != "accept" && action != "decline" && action != "cancel" {
		s.mu.Unlock()
		return
	}
	// A response is consent only. URL completion is an independent native HTTP
	// state transition; its wait never closes just because this reply arrives.
	p.Action = action
	sum := sha256.Sum256(m.Result)
	p.Reply = raw(map[string]string{"sha256": hex.EncodeToString(sum[:])})
	s.state.Effects["native-reply:"+id]++
	_ = s.saveLocked()
	s.changedLocked()
	thread := p.Thread
	s.mu.Unlock()
	s.emit(thread, "serverRequest/resolved", map[string]any{"threadId": thread, "requestId": id})
}
