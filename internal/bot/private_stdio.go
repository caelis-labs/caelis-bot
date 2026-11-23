package bot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type privateToolService struct {
	life  context.Context
	tools []string
	admit api.PrivateToolAdmission
}

func (b *Bridge) openPrivateStdio(config *api.ToolConnection, service api.ToolService, admit api.PrivateToolAdmission) (api.PrivateToolLaunch, error) {
	if b.runtime.provider != "caelis" || admit == nil || len(service.Tools) == 0 {
		return api.PrivateToolLaunch{}, errors.New("invalid private Bot service")
	}
	service.Token = rand.Text()
	scoped := config.ForService(service)
	if scoped == nil {
		return api.PrivateToolLaunch{}, errors.New("private Bot tool catalog is empty")
	}
	scoped.Env["CAELIS_BOT_TOOLS_VERSION"] = "3"
	dir, err := os.MkdirTemp("", "caelis-bot-mcp-")
	if err != nil {
		return api.PrivateToolLaunch{}, err
	}
	path := filepath.Join(dir, "service.json")
	cleanup := func() {
		_ = os.Remove(path)
		_ = os.Remove(dir)
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	if err = localstate.ProtectDirectory(dir); err != nil {
		return api.PrivateToolLaunch{}, err
	}
	if err = localstate.Write(path, scoped.Env); err != nil {
		return api.PrivateToolLaunch{}, err
	}
	life, cancel := context.WithCancel(b.life)
	b.privateMu.Lock()
	if err = life.Err(); err != nil {
		b.privateMu.Unlock()
		cancel()
		return api.PrivateToolLaunch{}, err
	}
	if b.private == nil {
		b.private = map[string]*privateToolService{}
	}
	b.private[service.Token] = &privateToolService{life: life, tools: slices.Clone(service.Tools), admit: admit}
	b.privateMu.Unlock()
	var once sync.Once
	revoke := func() {
		once.Do(func() {
			cancel()
			b.privateMu.Lock()
			delete(b.private, service.Token)
			b.privateMu.Unlock()
			cleanup()
		})
	}
	stop := context.AfterFunc(b.life, revoke)
	return api.PrivateToolLaunch{Args: []string{path}, WorkDir: dir, Revoke: func() { stop(); revoke() }}, nil
}

func (b *Bridge) callPrivate(conn net.Conn, req toolRequest) api.ToolResult {
	b.privateMu.Lock()
	service := b.private[req.Token]
	b.privateMu.Unlock()
	if service == nil || service.life.Err() != nil || !slices.Contains(service.tools, req.Name) {
		return compactError("service_not_authorized", errors.New("This private Bot service cannot accept the call."))
	}
	timeout := 10 * time.Second
	if req.Name == "bot_delegate" {
		timeout = delegationBridgeTimeout(req.Arguments)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	ctx, cancel := context.WithTimeout(service.life, timeout)
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, conn); cancel() }()
	call, err := service.admit(ctx, req.Name, req.Arguments)
	if err != nil {
		return compactError("invocation_unavailable", errors.New("The original reviewed Bot call could not be confirmed."))
	}
	return b.runtime.CallTool(call, req.Name, req.Arguments)
}

// RunStdioArgs loads an owned launch file when the Runtime cannot pass a
// private environment. The persistent Session profile carries only its path.
func RunStdioArgs(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return RunStdio(in, out)
	}
	if len(args) != 1 || !filepath.IsAbs(args[0]) {
		return errors.New("invalid private Bot service launch")
	}
	path := args[0]
	prior, err := os.Lstat(path)
	if err != nil || !prior.Mode().IsRegular() || !localstate.RestrictedAccess(path, prior) {
		return errors.New("invalid private Bot service file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	if info, err := file.Stat(); err != nil || !os.SameFile(prior, info) || !localstate.RestrictedAccess(path, info) {
		_ = file.Close()
		return errors.New("invalid private Bot service file")
	}
	var env map[string]string
	err = json.NewDecoder(io.LimitReader(file, 128<<10)).Decode(&env)
	_ = file.Close()
	if err != nil || env["CAELIS_BOT_TOOLS_VERSION"] != "3" ||
		env["CAELIS_BOT_ENDPOINT"] == "" || env["CAELIS_BOT_TOKEN"] == "" || env["CAELIS_BOT_TOOL_CATALOG"] == "" {
		return errors.New("invalid private Bot service configuration")
	}
	return runStdio(in, out, func(key string) string { return env[key] })
}
