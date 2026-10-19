package eventplan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxInterpreterBytes = 256 << 20

func PinScript(v Event) (Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return PinScriptContext(ctx, v)
}
func PinScriptContext(ctx context.Context, v Event) (Event, error) {
	if err := ctx.Err(); err != nil {
		return v, err
	}
	if !filepath.IsAbs(v.Interpreter) {
		return v, errors.New("interpreter must be an absolute executable path")
	}
	resolved, err := filepath.EvalSymlinks(v.Interpreter)
	if err != nil {
		return v, errors.New("interpreter unavailable")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > maxInterpreterBytes {
		return v, errors.New("interpreter must be a bounded regular executable")
	}
	f, err := openInterpreter(resolved)
	if err != nil {
		return v, errors.New("interpreter unreadable")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > maxInterpreterBytes {
		return v, errors.New("interpreter must be a bounded regular executable")
	}
	h := sha256.New()
	buffer := make([]byte, 64<<10)
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return v, err
		}
		n, err := f.Read(buffer)
		total += n
		if total > maxInterpreterBytes {
			return v, errors.New("interpreter size limit")
		}
		_, _ = h.Write(buffer[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return v, errors.New("interpreter unreadable")
		}
	}
	v.Interpreter = resolved
	v.ExecutableDigest = hex.EncodeToString(h.Sum(nil))
	return v, nil
}

type boundedOutput struct{ buffer bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 16384 {
		return 0, errors.New("collector output limit")
	}
	return b.buffer.Write(p)
}
func RunScript(ctx context.Context, v Event, directory string) (json.RawMessage, error) {
	pinned, err := PinScriptContext(ctx, v)
	if err != nil || pinned.ExecutableDigest != v.ExecutableDigest {
		return nil, collectorFailure("collector interpreter changed")
	}
	cmd := exec.CommandContext(ctx, v.Interpreter)
	cmd.Dir = directory
	cmd.Stdin = strings.NewReader(v.Script)
	// Host-private bridge tokens are not inherited by collectors. Scripts are
	// reviewed native collectors, not a filesystem or network security sandbox.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "CAELIS_BOT_") || strings.HasPrefix(key, "CODEX_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 250 * time.Millisecond
	stop := boundCollector(cmd)
	defer stop()
	if err := cmd.Run(); err != nil {
		if ctx.Err() == nil {
			return nil, collectorFailure("collector exited without valid output")
		}
		return nil, errors.New("collector execution did not return a confirmed result")
	}
	return append(json.RawMessage(nil), out.buffer.Bytes()...), nil
}
