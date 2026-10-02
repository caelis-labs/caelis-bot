// Package notebooksync copies ordinary Notebook files. It owns no Runtime,
// credentials, session state, leases or persistent service.
package notebooksync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Endpoint is native-only. Profile is the actual APP profile, never an agent
// directory or a renderer-selected path. Remote uses an existing SSH pairing.
type Endpoint struct {
	Profile, Target string
	Shell           []string
}
type Runner func(context.Context, string, ...string) error

type Rsync struct {
	Binary       string
	Run          Runner
	Version      func(context.Context, string) ([]byte, error)
	secludedArgs bool
}

var targetPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._:-]{0,253}$`)

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func shell(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = quote(a)
	}
	return strings.Join(out, " ")
}
func (e Endpoint) notebook() (string, error) {
	if !filepath.IsAbs(e.Profile) || filepath.Clean(e.Profile) != e.Profile || e.Profile == "/" || strings.ContainsAny(e.Profile, ":\x00\r\n") {
		return "", errors.New("invalid APP profile")
	}
	if e.Target != "" && (!targetPattern.MatchString(e.Target) || strings.Contains(e.Target, "::") || len(e.Shell) == 0) {
		return "", errors.New("existing SSH pairing required")
	}
	return filepath.Join(e.Profile, "Notebook"), nil
}

// Modern rsync passes secluded filenames through its protocol, so shell quotes
// must not become literal path bytes. Openrsync/rsync 2 retain shell quoting.
func (r Rsync) remoteArgumentMode(ctx context.Context) (bool, error) {
	binary := r.Binary
	if binary == "" {
		binary = "rsync"
	}
	read := r.Version
	if read == nil {
		read = func(ctx context.Context, binary string) ([]byte, error) {
			return exec.CommandContext(ctx, binary, "--version").Output()
		}
	}
	output, err := read(ctx, binary)
	if err != nil {
		return false, errors.New("Notebook rsync version unavailable")
	}
	text := string(output)
	if strings.HasPrefix(text, "openrsync:") {
		return false, nil
	}
	version := regexp.MustCompile(`(?m)^rsync\s+version\s+([0-9]+)\.`).FindStringSubmatch(text)
	if len(version) != 2 {
		return false, errors.New("Notebook rsync argument mode unavailable")
	}
	major, err := strconv.Atoi(version[1])
	if err != nil || major < 2 {
		return false, errors.New("Notebook rsync argument mode unavailable")
	}
	return major >= 3, nil
}
func (r Rsync) remotePath(target, path string) string {
	if r.secludedArgs {
		// Secluded arguments bypass the shell but rsync still expands patterns.
		// Escape its pattern syntax so only the original profile can be selected.
		path = strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(path)
	} else {
		path = quote(path)
	}
	return target + ":" + path
}

func command(ctx context.Context, binary string, args ...string) error {
	c := exec.CommandContext(ctx, binary, args...)
	c.Stdout, c.Stderr = io.Discard, io.Discard
	if err := c.Run(); err != nil {
		var code *exec.ExitError
		if errors.As(err, &code) {
			return fmt.Errorf("command failed (exit %d)", code.ExitCode())
		}
		return errors.New("command unavailable or cancelled")
	}
	return nil
}
func (r Rsync) run(ctx context.Context, binary string, args ...string) error {
	if r.Run != nil {
		return r.Run(ctx, binary, args...)
	}
	return command(ctx, binary, args...)
}
func (r Rsync) inspect(ctx context.Context, e Endpoint, requireMemory, final bool) error {
	dir, err := e.notebook()
	if err != nil {
		return err
	}
	if e.Target != "" {
		// Refuse symlinks, special files, and an alias in any ancestor. No repair or
		// deletion of user data is performed. Standby ownership is checked separately.
		script := "test -d " + quote(dir) + " && test \"$(cd " + quote(dir) + " && pwd -P)\" = " + quote(dir) + " && test -z \"$(find " + quote(dir) + " ! -type d ! -type f -print -quit)\""
		if requireMemory {
			script += " && test -f " + quote(filepath.Join(dir, "MEMORY.md"))
		}
		if final && !requireMemory {
			script += " && test ! -e " + quote(filepath.Join(dir, "HANDOFF.md"))
		}
		if err = r.run(ctx, e.Shell[0], append(append([]string{}, e.Shell[1:]...), "--", e.Target, script)...); err != nil {
			return errors.New("remote Notebook unavailable, unsafe, or contains an unresolved handoff")
		}
		return nil
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil || canonical != dir {
		return errors.New("Notebook must be an existing canonical directory")
	}
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return errors.New("Notebook contains a link or special file")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if requireMemory {
		info, err := os.Lstat(filepath.Join(dir, "MEMORY.md"))
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("source MEMORY.md unavailable")
		}
	}
	if final && !requireMemory {
		if _, err := os.Lstat(filepath.Join(dir, "HANDOFF.md")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("standby HANDOFF.md must be resolved before switching; files were preserved")
		}
	}
	return nil
}

// Hidden paths (including rsync conflict copies), indexes, runtime artifacts,
// common credential names and temporary files are never portable Notebook data.
var excludes = []string{".*", "INDEX.md", "HANDOFF.md", "*.db", "*.db-*", "*.sqlite", "*.sqlite-*", "*.sqlite3", "*.sqlite3-*", "*.lock", "*.sock", "*.tmp", "*.temp", "*~", "auth.json", "credentials*", "*token*", "*.pem", "*.key", "personal/", "providers/", "sessions/", "tasks/", "history/", "cache/", "CODEX_HOME/"}

func caseFoldPattern(p string) string {
	var b strings.Builder
	for _, c := range p {
		if c >= 'a' && c <= 'z' {
			b.WriteRune('[')
			b.WriteRune(c)
			b.WriteRune(c - 'a' + 'A')
			b.WriteRune(']')
		} else if c >= 'A' && c <= 'Z' {
			b.WriteRune('[')
			b.WriteRune(c + 'a' - 'A')
			b.WriteRune(c)
			b.WriteRune(']')
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func (r Rsync) copy(ctx context.Context, from, to Endpoint, fromDir, toDir, attempt string, backup bool) error {
	args := []string{"-rt", "--timeout=60", "--checksum", "--no-links", "--no-devices", "--no-specials"}
	for _, p := range excludes {
		args = append(args, "--exclude="+caseFoldPattern(p))
	}
	if backup {
		args = append(args, "--backup", "--backup-dir=.caelis-sync-conflicts/"+attempt)
	}
	remote := from
	if to.Target != "" {
		remote = to
	}
	if remote.Target != "" {
		if r.secludedArgs {
			args = append(args, "-s")
		}
		args = append(args, "-e", shell(remote.Shell))
	}
	src, dst := fromDir+"/", toDir+"/"
	if from.Target != "" {
		src = r.remotePath(from.Target, src)
	}
	if to.Target != "" {
		dst = r.remotePath(to.Target, dst)
	}
	binary := r.Binary
	if binary == "" {
		binary = "rsync"
	}
	if err := r.run(ctx, binary, append(args, "--", src, dst)...); err != nil {
		return fmt.Errorf("Notebook rsync failed: %w", err)
	}
	return nil
}

// Sync stages only files in a private disposable directory, so rsync never
// needs a remote-to-remote invocation or source-side credentials. There is no
// --delete and replaced target files remain in a hidden conflict directory.
// Handoff is supplied only by the host after a confirmed stop and validation
// of its completed Dream; the periodic path cannot transfer HANDOFF.md.
func (r Rsync) Sync(ctx context.Context, source, destination Endpoint, attempt string, final bool, handoff []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if !regexp.MustCompile(`^[a-zA-Z0-9-]{1,100}$`).MatchString(attempt) {
		return errors.New("invalid sync attempt")
	}
	if len(handoff) > 0 {
		first, body, ok := strings.Cut(string(handoff), "\n")
		if !final || len(handoff) > 16<<10 || !utf8.Valid(handoff) || !ok || !strings.HasPrefix(first, "<!-- caelis-dream: ") || !strings.HasSuffix(strings.TrimSuffix(first, "\r"), " -->") || strings.TrimSpace(body) == "" {
			return errors.New("invalid completed handoff")
		}
	}
	src, err := source.notebook()
	if err != nil {
		return err
	}
	dst, err := destination.notebook()
	if err != nil {
		return err
	}
	if source.Target != "" || destination.Target != "" {
		r.secludedArgs, err = r.remoteArgumentMode(ctx)
		if err != nil {
			return err
		}
	}
	if src == dst && source.Target == destination.Target {
		return errors.New("source and backup must differ")
	}
	if err = r.inspect(ctx, source, true, false); err != nil {
		return err
	}
	if err = r.inspect(ctx, destination, false, final); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "caelis-notebook-sync-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = r.copy(ctx, source, Endpoint{}, src, stage, attempt, false); err != nil {
		return err
	}
	if final {
		// A non-destructive backup retains source deletions. Refuse to activate a
		// target with extra old notes rather than silently restoring forgotten data.
		previous, err := os.MkdirTemp("", "caelis-notebook-compare-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(previous)
		if err = r.copy(ctx, destination, Endpoint{}, dst, previous, attempt, false); err != nil {
			return err
		}
		err = filepath.WalkDir(previous, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			rel, e := filepath.Rel(previous, p)
			if e != nil {
				return e
			}
			if _, e = os.Stat(filepath.Join(stage, rel)); errors.Is(e, os.ErrNotExist) {
				return errors.New("backup contains files absent from the active Notebook; preserved for review before switching")
			}
			return e
		})
		if err != nil {
			return err
		}
	}
	if err = r.copy(ctx, Endpoint{}, destination, stage, dst, attempt, true); err != nil {
		return err
	}
	if len(handoff) > 0 {
		if !final || len(handoff) > 16<<10 {
			return errors.New("handoff requires a confirmed stopped final sync")
		}
		// A separate file-only transfer excludes every other path. Receiver's old
		// handoff was rejected above; it is never deleted, archived, or replayed.
		if err = os.WriteFile(filepath.Join(stage, "HANDOFF.md"), handoff, 0600); err != nil {
			return err
		}
		binary := r.Binary
		if binary == "" {
			binary = "rsync"
		}
		args := []string{"-rt", "--ignore-existing", "--no-links"}
		target := dst + "/"
		if destination.Target != "" {
			if r.secludedArgs {
				args = append(args, "-s")
			}
			args = append(args, "-e", shell(destination.Shell))
			target = r.remotePath(destination.Target, target)
		}
		if err = r.run(ctx, binary, append(args, "--", filepath.Join(stage, "HANDOFF.md"), target)...); err != nil {
			return errors.New("final handoff transfer failed")
		}
	}
	return nil
}
