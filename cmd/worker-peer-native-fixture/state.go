//go:build workerpeere2e

package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const fixtureBuild = "worker-peer-native-fixture/0.1.0"

type nativeItem struct {
	Delivery  string `json:"delivery,omitempty"`
	Questions []struct {
		Title   string   `json:"title"`
		Options []string `json:"options"`
	} `json:"questions,omitempty"`
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ClientID  string          `json:"clientId,omitempty"`
	Content   []nativeInput   `json:"content,omitempty"`
	Status    string          `json:"status,omitempty"`
	Server    string          `json:"server,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Arguments any             `json:"arguments,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}
type nativeInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Path string `json:"path,omitempty"`
}
type nativeTurn struct {
	ID     string       `json:"id"`
	Status string       `json:"status"`
	Items  []nativeItem `json:"items"`
}
type nativeThread struct {
	ID     string       `json:"id"`
	Turns  []nativeTurn `json:"turns"`
	Status struct {
		Type string `json:"type"`
	} `json:"status"`
	Cwd      string `json:"cwd"`
	Resident bool   `json:"fixtureResident"`
	LastTask string `json:"fixtureLastTask,omitempty"`
}
type nativeState struct {
	Version      int                      `json:"version"`
	Synthetic    bool                     `json:"synthetic"`
	PID          int                      `json:"fixturePid"`
	Instance     string                   `json:"instance"`
	Build        string                   `json:"build"`
	Next         uint64                   `json:"next"`
	Threads      map[string]*nativeThread `json:"threads"`
	Methods      map[string]int           `json:"methodCounts"`
	Effects      map[string]int           `json:"effectCounts"`
	ClientInputs map[string]int           `json:"clientInputCounts"`
	Pending      map[string]*pending      `json:"pending"`
}
type mcpConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Enabled *bool             `json:"enabled"`
}
type pending struct {
	ID        string          `json:"id"`
	Thread    string          `json:"thread"`
	Turn      string          `json:"turn"`
	Method    string          `json:"method"`
	Kind      string          `json:"fixtureKind"`
	Params    json.RawMessage `json:"params"`
	Action    string          `json:"action,omitempty"`
	Reply     json.RawMessage `json:"reply,omitempty"`
	Nonce     string          `json:"fixtureNonce,omitempty"`
	Completed bool            `json:"challengeCompleted"`
	PostCount int             `json:"challengePostCount"`
}
type nativeServer struct {
	mu         sync.Mutex
	cwd        string
	path       string
	state      nativeState
	peers      map[*nativePeer]bool
	services   map[string]mcpConfig // Ephemeral Bot MCP endpoint/token; NEVER persisted.
	changes    chan struct{}
	urlBase    string
	urlClose   func()
	turnCancel map[string]context.CancelFunc
}

func openNative(cwd string) (*nativeServer, error) {
	dir := filepath.Join(cwd, ".worker-peer-native-fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &nativeServer{cwd: cwd, path: filepath.Join(dir, "state.json"), peers: map[*nativePeer]bool{}, services: map[string]mcpConfig{}, changes: make(chan struct{}), turnCancel: map[string]context.CancelFunc{}}
	s.state = nativeState{Version: 1, Synthetic: true, Instance: rand.Text(), Build: fixtureBuild, Threads: map[string]*nativeThread{}, Methods: map[string]int{}, Effects: map[string]int{}, ClientInputs: map[string]int{}, Pending: map[string]*pending{}}
	if data, err := os.ReadFile(s.path); err == nil {
		if json.Unmarshal(data, &s.state) != nil || s.state.Version != 1 || !s.state.Synthetic || s.state.Threads == nil || s.state.Pending == nil {
			return nil, errors.New("invalid synthetic native state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := s.startURL(); err != nil {
		return nil, err
	}
	s.state.PID = os.Getpid()
	return s, nil
}
func (s *nativeServer) saveLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path)
	}
	return err
}
func (s *nativeServer) changedLocked() {
	close(s.changes)
	s.changes = make(chan struct{})
}
func (s *nativeServer) close() {
	if s.urlClose != nil {
		s.urlClose()
	}
}
