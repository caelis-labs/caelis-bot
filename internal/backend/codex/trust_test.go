package codex

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func trustSessionFixture(t *testing.T, initial string) (*Session, func() (int, []string)) {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "Notebook")
	s := NewSession(SessionOptions{Directory: directory, StateFile: filepath.Join(root, "binding.json")})
	if err := s.ConfigureBotTools(&api.ToolConnection{Command: "/fixture/bot", NotebookDirectory: directory}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	level := initial
	writes := 0
	methods := []string{}
	var peers []net.Conn
	s.start = func(context.Context, Options) (*Client, error) {
		a, b := net.Pipe()
		mu.Lock()
		peers = append(peers, b)
		mu.Unlock()
		go func() {
			defer b.Close()
			dec, enc := json.NewDecoder(b), json.NewEncoder(b)
			for {
				var m wireMessage
				if dec.Decode(&m) != nil {
					return
				}
				if m.Method == "" {
					continue
				}
				mu.Lock()
				methods = append(methods, m.Method)
				var result any = map[string]any{}
				var nativeErr *NativeError
				disconnect := false
				switch m.Method {
				case "account/read":
					result = map[string]any{"account": map[string]string{"type": "apiKey"}, "requiresOpenaiAuth": true}
				case "config/read":
					projects := map[string]any{}
					if level != "" {
						trust := level
						if level == "trusted_policy" {
							trust = "trusted"
						}
						path := directory
						if level == "ancestor_trusted" {
							path, trust = root, "trusted"
						}
						projects[path] = map[string]string{"trust_level": trust}
					}
					reason := ""
					if level == "trusted_policy" {
						reason = "disabled by organization policy"
					} else if level != "trusted" {
						reason = "untrusted"
					}
					result = map[string]any{"config": map[string]any{"projects": projects}, "layers": []any{map[string]any{"name": map[string]string{"type": "project"}, "disabledReason": reason}}}
				case "config/batchWrite":
					if level == "write_unknown" {
						writes++
						disconnect = true
						break
					}
					if level == "write_policy" {
						nativeErr = &NativeError{Code: -32603, Message: "organization policy denies this project"}
						break
					}
					var params struct {
						Edits []struct {
							KeyPath       string `json:"keyPath"`
							Value         string `json:"value"`
							MergeStrategy string `json:"mergeStrategy"`
						} `json:"edits"`
						Reload bool `json:"reloadUserConfig"`
					}
					_ = json.Unmarshal(m.Params, &params)
					if len(params.Edits) != 1 || params.Edits[0].KeyPath != "projects."+strconvQuoteConfigKey(directory)+".trust_level" || params.Edits[0].Value != "trusted" || params.Edits[0].MergeStrategy != "replace" || !params.Reload {
						t.Errorf("unsafe trust edit: %+v", params)
					}
					writes++
					level = "trusted"
				case "thread/start", "thread/resume", "thread/read":
					result = map[string]any{"thread": nativeThread{ID: "resident"}, "model": "fixture"}
				}
				mu.Unlock()
				if disconnect {
					return // Dispatched mutation, no response: the original outcome is unknown.
				}
				if nativeErr != nil {
					_ = enc.Encode(wireMessage{ID: m.ID, Error: nativeErr})
				} else {
					_ = enc.Encode(wireMessage{ID: m.ID, Result: raw(result)})
				}
			}
		}()
		return &Client{rpc: newTransportOptions(a, nil, true)}, nil
	}
	t.Cleanup(func() {
		_ = s.Close(context.Background())
		mu.Lock()
		defer mu.Unlock()
		for _, peer := range peers {
			_ = peer.Close()
		}
	})
	return s, func() (int, []string) {
		mu.Lock()
		defer mu.Unlock()
		return writes, append([]string(nil), methods...)
	}
}

func TestBotWorkspaceTrustGate(t *testing.T) {
	for _, tc := range []struct {
		name, initial, decision string
		wantWrites, wantThreads int
	}{
		{"absent", "", "", 1, 1},
		{"ancestor_trusted_but_project_disabled", "ancestor_trusted", "", 1, 1},
		{"trusted", "trusted", "", 0, 1},
		{"untrusted_accept", "untrusted", "accept", 1, 1},
		{"untrusted_decline", "untrusted", "decline", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, observe := trustSessionFixture(t, tc.initial)
			if err := s.Connect(t.Context()); err != nil {
				t.Fatal(err)
			}
			if tc.decision != "" {
				state := s.Snapshot()
				writes, _ := observe()
				if state.Connection == "ready" || len(state.Approvals) != 1 || state.Approvals[0].Status != "pending" || writes != 0 {
					t.Fatal("untrusted project initialized before native decision", state.Connection, state.Approvals, writes)
				}
				if err := s.Decide(t.Context(), api.Decision{ID: state.Approvals[0].ID, Choice: tc.decision}); err != nil {
					t.Fatal(err)
				}
			}
			writes, methods := observe()
			if writes != tc.wantWrites {
				t.Fatal("trust writes", writes, tc.wantWrites)
			}
			threads := 0
			for _, method := range methods {
				if method == "thread/start" || method == "thread/resume" {
					threads++
				}
			}
			if threads != tc.wantThreads {
				t.Fatal("resident thread initialization", threads, tc.wantThreads, methods)
			}
			if tc.decision == "decline" {
				if s.Snapshot().Connection == "ready" || s.Snapshot().Message == "" {
					t.Fatal("declined trust appeared ready", s.Snapshot())
				}
				oldID := s.Snapshot().Approvals[0].ID
				if err := s.Connect(t.Context()); err != nil {
					t.Fatal("explicit reconnect did not recheck trust", err)
				}
				pending := s.Snapshot().Approvals
				if len(pending) < 2 || pending[len(pending)-1].ID == oldID || pending[len(pending)-1].Status != "pending" {
					t.Fatal("new consent reused the declined approval identity", pending)
				}
			} else if s.Snapshot().Connection != "ready" {
				t.Fatal("trusted Bot did not become ready", s.Snapshot())
			}
		})
	}
}

func TestBotWorkspacePolicyBlocksBeforeThread(t *testing.T) {
	s, observe := trustSessionFixture(t, "trusted_policy")
	if err := s.Connect(t.Context()); err == nil {
		t.Fatal("policy-disabled trusted project was accepted")
	}
	writes, methods := observe()
	if writes != 0 || s.Snapshot().Connection == "ready" || s.Snapshot().ConnectionIssue != "workspace_trust" || s.Snapshot().Message == "" {
		t.Fatal("policy block not shown", writes, s.Snapshot())
	}
	for _, method := range methods {
		if method == "thread/start" || method == "thread/resume" {
			t.Fatal("resident thread loaded disabled project config", methods)
		}
	}
}

func TestBotWorkspaceWritePolicyReasonIsShown(t *testing.T) {
	s, _ := trustSessionFixture(t, "write_policy")
	if err := s.Connect(t.Context()); err == nil {
		t.Fatal("policy-rejected trust write was accepted")
	}
	if state := s.Snapshot(); state.ConnectionIssue != "workspace_trust" || !strings.Contains(state.Message, "organization policy denies this project") {
		t.Fatal("native policy reason was hidden", state.ConnectionIssue, state.Message)
	}
}

func TestUnknownBotTrustWriteIsNotReplayed(t *testing.T) {
	s, observe := trustSessionFixture(t, "write_unknown")
	if err := s.Connect(t.Context()); err == nil {
		t.Fatal("lost trust write response was accepted")
	}
	if err := s.Connect(t.Context()); err == nil {
		t.Fatal("uncertain trust write was automatically retried")
	}
	writes, methods := observe()
	if writes != 1 || !strings.Contains(s.Snapshot().Message, "不会重复写入") {
		t.Fatal("uncertain trust write not fenced", writes, s.Snapshot().Message)
	}
	var saved binding
	data, err := os.ReadFile(s.opts.StateFile)
	if err != nil || json.Unmarshal(data, &saved) != nil || !saved.TrustWriteUnknown {
		t.Fatal("unknown trust receipt did not survive restart", err)
	}
	for _, method := range methods {
		if method == "thread/start" || method == "thread/resume" {
			t.Fatal("resident initialized with uncertain trust", methods)
		}
	}
}
