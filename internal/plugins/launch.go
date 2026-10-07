package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

func RuntimeName(packageID, server string) string {
	sum := sha256Name(packageID + "/" + server)
	name := strings.ReplaceAll(server, "-", "_")
	if len(name) > 20 {
		name = name[:20]
	}
	return "p_" + sum[:8] + "_" + name
}
func sha256Name(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

// RunStdio is invoked by the Bot executable selected in a Core public MCP
// profile. Only a byte-identical, build-reviewed package may be launched. Its
// portable environment stays in this Bot process, never in Core's profile.
func RunStdio(storeRoot, packageID, serverName string, in io.Reader, out, diagnostics io.Writer) error {
	m, err := Open(storeRoot)
	if err != nil {
		return err
	}
	e, ok := m.entry(packageID)
	if !ok {
		return errors.New("unreviewed plugin")
	}
	p, err := m.readInstalled(e)
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
	if selected == nil || selected.Type != "stdio" {
		return errors.New("plugin service unavailable")
	}
	data := filepath.Join(storeRoot, "data", packageID)
	if err := ensureStoreDirectory(storeRoot, data); err != nil {
		return err
	}
	s, err := selected.Resolve(p.Root, data)
	if err != nil {
		return err
	}
	cmd := exec.Command(s.Command, s.Args...)
	cmd.Dir = s.CWD
	cmd.Stdin, cmd.Stdout = in, out
	cmd.Stderr = diagnostics
	base := cmd.Environ()
	for k, v := range s.Env {
		base = append(base, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = base
	return cmd.Run()
}
