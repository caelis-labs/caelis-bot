package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type runtimeError string

func (e runtimeError) Error() string { return string(e) }

const errRuntimeMissing runtimeError = "runtime_missing"

// Inspect structured protocol failures, never CLI versions or error prose.
// Used only to explain failed connection/validation, not to retry a mutation,
// relax the requested approval policy or invent unsupported capabilities.
func incompatibleProtocol(err error) bool {
	var native *NativeError
	return errors.Is(err, ErrProtocol) || (errors.As(err, &native) && (native.Code == -32601 || native.Code == -32602))
}

// Discover the user's installation, never copy or install a runtime. Finder
// does not inherit the interactive shell's PATH.
func runtimeBinary(explicit string) (string, error) {
	if explicit != "" {
		p, e := exec.LookPath(explicit)
		if e != nil {
			return "", errRuntimeMissing
		}
		return installedBinary(p), nil
	}
	if p, e := exec.LookPath("codex"); e == nil {
		return installedBinary(p), nil
	}
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{filepath.Join(home, ".local", "bin", "codex"), "/opt/homebrew/bin/codex", "/usr/local/bin/codex"} {
		if p, e := exec.LookPath(candidate); e == nil {
			return installedBinary(p), nil
		}
	}
	return "", errRuntimeMissing
}

// The npm entrypoint needs Node on PATH. Prefer that same installation's native
// binary using its published package layout, so Finder launches also work.
func installedBinary(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Base(real) != "codex.js" {
		return path
	}
	root := filepath.Dir(filepath.Dir(real))
	var pkg struct {
		Name string `json:"name"`
	}
	b, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil || json.Unmarshal(b, &pkg) != nil || pkg.Name != "@openai/codex" {
		return path
	}
	target := map[string]string{"arm64": "aarch64-apple-darwin", "amd64": "x86_64-apple-darwin"}[runtime.GOARCH]
	arch := map[string]string{"arm64": "arm64", "amd64": "x64"}[runtime.GOARCH]
	if runtime.GOOS != "darwin" || target == "" {
		return path
	}
	for _, base := range []string{filepath.Join(root, "node_modules", "@openai", "codex-darwin-"+arch, "vendor"), filepath.Join(root, "vendor")} {
		if p, err := exec.LookPath(filepath.Join(base, target, "bin", "codex")); err == nil {
			return p
		}
	}
	return path
}

type turnError struct {
	Message string          `json:"message"`
	Info    json.RawMessage `json:"codexErrorInfo"`
}

// Codes come from the pinned TurnError schema. Do not classify generated prose.
func (s *Session) applyFailure(e turnError) {
	s.state.Message = e.Message
	var code string
	_ = json.Unmarshal(e.Info, &code)
	var details map[string]struct {
		HTTPStatusCode int `json:"httpStatusCode"`
	}
	if json.Unmarshal(e.Info, &details) == nil {
		for _, variant := range []string{"httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts"} {
			if details[variant].HTTPStatusCode == 401 {
				code = "unauthorized"
			}
		}
	}
	if code == "unauthorized" {
		s.state.Connection = "login"
		s.state.ConnectionIssue = "authentication"
		s.state.Message = "Codex 登录已失效，请重新登录。已发送的工作不会自动重发。"
	}
}

func (s *Session) completeLogin(epoch uint64) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	current := epoch == s.epoch && s.state.Connection == "login" && !s.state.LoginPending && !s.closed && !s.closing
	s.mu.Unlock()
	if current {
		_ = s.connect(context.Background())
	}
}

// WaitSnapshot lets native observers sleep between changes, even when all web
// surfaces are hidden. It does not dispatch work or poll the model.
func (s *Session) WaitSnapshot(ctx context.Context, revision uint64) (apiSnapshot api.Snapshot, err error) {
	for {
		s.mu.Lock()
		changed, newer, closed := s.changed, s.state.Revision != revision, s.closed
		s.mu.Unlock()
		if closed {
			return apiSnapshot, errors.New("closed")
		}
		if newer {
			return s.Snapshot(), nil
		}
		select {
		case <-ctx.Done():
			return apiSnapshot, ctx.Err()
		case <-changed:
		}
	}
}
