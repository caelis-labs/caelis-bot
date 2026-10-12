package plugins

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func RuntimeName(packageID, server string) string {
	sum := sha256Name(packageID + "/" + server)
	name := strings.Map(func(char rune) rune {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' {
			return char
		}
		return '_'
	}, server)
	if len(name) > 20 {
		name = name[:20]
	}
	return "p_" + sum[:8] + "_" + name
}
func sha256Name(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

// RunStdio is invoked by the Bot executable selected in a Core public MCP
// profile. Only a byte-identical, build-reviewed package may be launched. Its
// portable environment stays in this Bot process, never in Core's profile.
func RunStdio(storeRoot, packageID, serverName string, in io.Reader, out, diagnostics io.Writer, generation ...string) error {
	m, err := Open(storeRoot)
	if err != nil {
		return err
	}
	return runStdio(context.Background(), m, packageID, serverName, in, out, diagnostics, generation...)
}

func runStdio(ctx context.Context, m *Manager, packageID, serverName string, in io.Reader, out, diagnostics io.Writer, generation ...string) error {
	e, ok := m.entry(packageID)
	if !ok {
		return errors.New("unreviewed plugin")
	}
	if len(generation) > 2 {
		return errors.New("invalid plugin generation")
	}
	rootName := ""
	if len(generation) >= 1 {
		rootName = generation[0]
	}
	// A Runtime may retain an old thread configuration after a reload. Never
	// start a new child from that configuration once Bot has confirmed a
	// disable, update, or uninstall. Already running children drain normally.
	installed, active := m.state.Installed[packageID]
	if !active || !installed.Enabled || rootName == "" || rootName != installed.Root {
		return errors.New("plugin service is no longer active")
	}
	p, err := m.readInstalledAt(e, rootName)
	if err != nil {
		return err
	}
	var selected *Server
	for i := range p.Servers {
		if p.Servers[i].Name == serverName {
			selected = &p.Servers[i]
			break
		}
	}
	if selected == nil {
		return errors.New("plugin service unavailable")
	}
	// Runtime sessions can retain old MCP schemas after a reload. Fence every
	// incoming JSON-RPC message against the committed plugin state and stop an
	// already running child when its generation is disabled or superseded.
	stillActive := func() bool { return activeGeneration(m.root, packageID, rootName) }
	guardedCtx, stop := context.WithCancel(ctx)
	defer stop()
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()
	defer pipeWriter.Close()
	go func() {
		reader := bufio.NewReader(in)
		for {
			line, readErr := reader.ReadBytes('\n')
			if len(line) > 0 {
				if !stillActive() {
					_ = pipeWriter.CloseWithError(errors.New("plugin service is no longer active"))
					stop()
					return
				}
				if _, err := pipeWriter.Write(line); err != nil {
					return
				}
			}
			if readErr != nil {
				_ = pipeWriter.CloseWithError(readErr)
				return
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-guardedCtx.Done():
				return
			case <-ticker.C:
				if !stillActive() {
					stop()
					_ = pipeReader.CloseWithError(errors.New("plugin service is no longer active"))
					return
				}
			}
		}
	}()
	secret := ""
	caPEM := ""
	var bearer func(context.Context, string) (string, error)
	if e.Connection != nil && e.Connection.Server == serverName {
		if len(generation) != 2 {
			return errors.New("connection revision missing")
		}
		revision, parseErr := strconv.ParseUint(generation[1], 10, 64)
		if parseErr != nil || revision == 0 {
			return errors.New("invalid connection revision")
		}
		connection := m.state.Connections[packageID]
		if !connection.Configured || connection.Revision != revision {
			return errors.New("plugin connection is no longer active")
		}
		if e.Connection.Kind == "oauth" || connection.Mode == "oauth" {
			bearer = func(callCtx context.Context, rejected string) (string, error) {
				return m.oauthBearer(callCtx, packageID, revision, rejected)
			}
		} else {
			secret, err = m.secrets.Load(secretKey(m.root, packageID, revision))
			if err != nil || !validCredential(secret) {
				return errors.New("plugin credential unavailable")
			}
			if e.Connection.TrustCA {
				caPEM, _ = m.secrets.Load(secretKey(m.root, packageID, revision) + "-ca")
			}
		}
	}
	if selected.Type == "streamable-http" {
		return relayRemoteWithOAuth(guardedCtx, *selected, e.Connection, secret, caPEM, bearer, pipeReader, out)
	}
	if selected.Type != "stdio" {
		return errors.New("plugin service unavailable")
	}
	data := filepath.Join(m.root, "data", packageID)
	if err := ensureStoreDirectory(m.root, data); err != nil {
		return err
	}
	s, err := selected.Resolve(p.Root, data)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(guardedCtx, s.Command, s.Args...)
	cmd.Dir = s.CWD
	cmd.Stdin, cmd.Stdout = pipeReader, out
	cmd.Stderr = diagnostics
	if e.Connection != nil {
		// The upstream process sees its own credential. Its diagnostics are not
		// trusted to redact that value, so keep them out of Bot/Core logs.
		cmd.Stderr = io.Discard
	}
	base := cmd.Environ()
	for k, v := range s.Env {
		base = replaceEnvironment(base, k, v)
	}
	if e.Connection != nil && e.Connection.Placement == "env" {
		base = replaceEnvironment(base, e.Connection.Name, e.Connection.Prefix+secret)
	}
	cmd.Env = base
	return cmd.Run()
}

func activeGeneration(storeRoot, packageID, generation string) bool {
	body, err := os.ReadFile(filepath.Join(storeRoot, "state.json"))
	if err != nil {
		return false
	}
	var saved state
	if json.Unmarshal(body, &saved) != nil {
		return false
	}
	item, ok := saved.Installed[packageID]
	return ok && item.Enabled && item.Root == generation
}

func replaceEnvironment(base []string, key, value string) []string {
	for i := 0; i < len(base); i++ {
		name, _, _ := strings.Cut(base[i], "=")
		if name == key || runtime.GOOS == "windows" && strings.EqualFold(name, key) {
			base = append(base[:i], base[i+1:]...)
			i--
		}
	}
	return append(base, fmt.Sprintf("%s=%s", key, value))
}
