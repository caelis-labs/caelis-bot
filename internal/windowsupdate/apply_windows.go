package windowsupdate

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"github.com/caelis-labs/caelis-bot/internal/updates"
	"golang.org/x/sys/windows"
)

type Plan struct {
	Schema    int    `json:"schema"`
	Root      string `json:"root"`
	Target    string `json:"target"`
	Identity  string `json:"identity"`
	Current   string `json:"current"`
	Format    string `json:"format"`
	Manifest  []byte `json:"manifest"`
	Signature []byte `json:"signature"`
	Archive   string `json:"archive"`
	Relaunch  bool   `json:"relaunch"`
}
type ApplyResult struct {
	State   string `json:"state"`
	Version string `json:"version"`
	Message string `json:"message,omitempty"`
}

func Installation(target, identity string) (string, error) {
	if e := safeDirectory(target); e != nil {
		return "", e
	}
	if p, e := readPortable(filepath.Join(target, "portable.json")); e == nil && p.Identity == identity {
		return "zip", nil
	}
	data, e := os.ReadFile(filepath.Join(target, "installation.ini"))
	if e != nil {
		return "", errors.New("packaged Windows installation required")
	}
	marker := installationINI(data)
	if marker["Identity"] != identity {
		return "", errors.New("NSIS installation identity mismatch")
	}
	name := "Caelis Bot Preview"
	if identity == "dev.caelis.bot.dev" {
		name = "Caelis Bot Dev"
	} else if identity == "dev.caelis.bot" {
		name = "Caelis Bot"
	} else if identity != "dev.caelis.bot.preview" {
		return "", errors.New("unknown NSIS identity")
	}
	expected := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", name)
	if location := marker["Location"]; location != "" {
		if !filepath.IsAbs(location) || filepath.Dir(filepath.Clean(location)) == filepath.Clean(location) {
			return "", errors.New("invalid NSIS installation location")
		}
		expected = filepath.Clean(location)
	}
	if !strings.EqualFold(filepath.Clean(target), expected) {
		return "", errors.New("NSIS installation location mismatch")
	}
	return "nsis", nil
}
func installationINI(data []byte) map[string]string {
	out := map[string]string{}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		if len(data)%2 != 0 {
			return out
		}
		words := make([]uint16, (len(data)-2)/2)
		for i := range words {
			words[i] = binary.LittleEndian.Uint16(data[2+i*2:])
		}
		data = []byte(string(utf16.Decode(words)))
	}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section == "[CaelisBot]" {
			if k, v, ok := strings.Cut(line, "="); ok {
				out[k] = v
			}
		}
	}
	return out
}
func installationVersion(target, format string) (string, error) {
	if format == "zip" {
		p, e := readPortable(filepath.Join(target, "portable.json"))
		return p.Version, e
	}
	b, e := os.ReadFile(filepath.Join(target, "installation.ini"))
	if e != nil {
		return "", e
	}
	return installationINI(b)["Version"], nil
}
func validatePlan(path string, p Plan) (Manifest, Artifact, error) {
	var m Manifest
	var artifact Artifact
	if p.Schema != 1 || !filepath.IsAbs(path) || !filepath.IsAbs(p.Root) || !filepath.IsAbs(p.Target) || filepath.Base(path) != "plan.json" || filepath.Dir(filepath.Dir(filepath.Dir(path))) != p.Root || filepath.Base(filepath.Dir(filepath.Dir(path))) != "Updates" {
		return m, artifact, errors.New("invalid update owner/plan path")
	}
	if e := safeDirectory(filepath.Dir(path)); e != nil {
		return m, artifact, e
	}
	if e := safeDirectory(p.Target); e != nil {
		return m, artifact, e
	}
	key, e := Key()
	if e != nil {
		return m, artifact, e
	}
	m, e = Verify(p.Manifest, p.Signature, key)
	if e != nil {
		return m, artifact, e
	}
	channel := "stable"
	if strings.HasSuffix(p.Identity, ".preview") {
		channel = "preview"
	}
	if m.Channel != channel {
		return m, artifact, errors.New("update identity/channel mismatch")
	}
	cmp, e := updates.CompareVersions(m.Version, p.Current)
	if e != nil || cmp <= 0 {
		return m, artifact, errors.New("update must advance the installed version")
	}
	format, e := Installation(p.Target, p.Identity)
	if e != nil || format != p.Format {
		return m, artifact, errors.New("update format mismatch")
	}
	installed, e := installationVersion(p.Target, format)
	if e != nil || installed != p.Current {
		return m, artifact, errors.New("installed version changed; inspect the original update receipt")
	}
	for _, a := range m.Artifacts {
		if a.Format == p.Format {
			artifact = a
		}
	}
	if artifact.File == "" || p.Archive != filepath.Join(filepath.Dir(path), artifact.File) {
		return m, artifact, errors.New("update archive owner mismatch")
	}
	return m, artifact, VerifyArchive(p.Archive, artifact)
}
func Start(root, target, identity, current string, offer *Offer, archive string) (string, error) {
	return start(root, target, identity, current, offer, archive, true)
}
func start(root, target, identity, current string, offer *Offer, archive string, relaunch bool) (string, error) {
	if offer == nil {
		return "", errors.New("no downloaded update")
	}
	if err := TerminalIdle(target); err != nil {
		return "", err
	}
	dir := filepath.Dir(archive)
	planPath := filepath.Join(dir, "plan.json")
	p := Plan{1, root, target, identity, current, offer.Artifact.Format, offer.Bytes, offer.Signature, archive, relaunch}
	for _, name := range []string{"plan.json", "ready", "commit.json", "result.json"} {
		if _, e := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(e) {
			return "", errors.New("original update transaction already exists; inspect its receipt before retrying")
		}
	}
	if _, _, e := validatePlan(planPath, p); e != nil {
		return "", e
	}
	if e := localstate.Write(planPath, p); e != nil {
		return "", e
	}
	helper := filepath.Join(dir, "apply.exe")
	exe, e := os.Executable()
	if e != nil {
		return "", e
	}
	b, e := os.ReadFile(exe)
	if e != nil {
		return "", e
	}
	if e = os.WriteFile(helper, b, 0600); e != nil {
		return "", e
	}
	h, e := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, true, uint32(os.Getpid()))
	if e != nil {
		return "", e
	}
	defer windows.CloseHandle(h)
	command := exec.Command(helper, "--apply-windows-update", planPath, strconv.Itoa(os.Getpid()), strconv.FormatUint(uint64(h), 10))
	command.Env = append(runtimeenv.Clean(os.Environ()), "CAELIS_BOT_DATA_DIR="+root)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(h)}}
	if e = command.Start(); e != nil {
		return "", e
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		if data, e := os.ReadFile(filepath.Join(dir, "ready")); e == nil {
			var ready struct {
				PID int `json:"pid"`
			}
			if json.Unmarshal(data, &ready) == nil && ready.PID == command.Process.Pid {
				return dir, nil
			}
		}
		select {
		case e = <-done:
			return "", fmt.Errorf("update helper exited before handoff: %w", e)
		case <-timer.C:
			_ = command.Process.Kill()
			return "", errors.New("update helper did not acknowledge handoff")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func Run(args []string) (bool, error) {
	if len(args) == 1 && args[0] == "--close-terminal-helpers-for-update" {
		exe, err := os.Executable()
		if err != nil {
			return true, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return true, CloseTerminalHelpers(ctx, filepath.Dir(exe))
	}
	if len(args) == 0 || args[0] != "--apply-windows-update" {
		return false, nil
	}
	if len(args) != 4 {
		return true, errors.New("invalid update helper arguments")
	}
	pid, e := strconv.ParseUint(args[2], 10, 32)
	if e != nil {
		return true, e
	}
	value, e := strconv.ParseUint(args[3], 10, 64)
	if e != nil || value == 0 {
		return true, errors.New("invalid owner handle")
	}
	handle := windows.Handle(value)
	defer windows.CloseHandle(handle)
	actual, e := windows.GetProcessId(handle)
	if e != nil || uint64(actual) != pid {
		return true, errors.New("exact update process owner not confirmed")
	}
	path := args[1]
	data, e := os.ReadFile(path)
	if e != nil || len(data) > 1<<20 {
		return true, errors.New("update plan unreadable")
	}
	var p Plan
	if json.Unmarshal(data, &p) != nil {
		return true, errors.New("invalid update plan")
	}
	executable, e := os.Executable()
	if e != nil || filepath.Clean(executable) != filepath.Join(filepath.Dir(path), "apply.exe") {
		return true, errors.New("update helper location mismatch")
	}
	m, _, e := validatePlan(path, p)
	if e != nil {
		return true, e
	}
	if e = TerminalIdle(p.Target); e != nil {
		return true, e
	}
	// An unresolved transaction is never replayed merely because a helper exits.
	receipt := filepath.Join(filepath.Dir(path), "result.json")
	f, e := os.OpenFile(receipt, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return true, errors.New("original update receipt already exists")
	}
	e = json.NewEncoder(f).Encode(ApplyResult{State: "waiting", Version: m.Version})
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return true, e
	}
	if e = localstate.WriteConfirmed(filepath.Join(filepath.Dir(path), "ready"), map[string]int{"pid": os.Getpid()}); e != nil {
		return true, e
	}
	status, e := windows.WaitForSingleObject(handle, 120000)
	if e != nil || status != windows.WAIT_OBJECT_0 {
		return true, errors.Join(errors.New("update owner still running"), localstate.WriteConfirmed(receipt, ApplyResult{"failed", m.Version, "Bot did not exit; no installation was attempted"}))
	}
	// Parent exit alone is not authorization: it may have crashed or failed
	// while preserving its task owners. Only a successful native shutdown
	// commits this exact transaction before the original process exits.
	var commit struct {
		PID     int    `json:"pid"`
		Version string `json:"version"`
	}
	committed, readErr := os.ReadFile(filepath.Join(filepath.Dir(path), "commit.json"))
	if readErr != nil || json.Unmarshal(committed, &commit) != nil || uint64(commit.PID) != pid || commit.Version != m.Version {
		return true, errors.Join(errors.New("update shutdown was not committed"), localstate.WriteConfirmed(receipt, ApplyResult{"failed", m.Version, "Bot did not commit a successful shutdown; no installation was attempted"}))
	}
	if e = localstate.WriteConfirmed(receipt, ApplyResult{State: "installing", Version: m.Version}); e != nil {
		return true, fmt.Errorf("persist installing receipt before replacement: %w", e)
	}
	if _, _, e = validatePlan(path, p); e == nil {
		e = apply(p, m, filepath.Dir(path))
	}
	if e != nil {
		return true, errors.Join(e, localstate.WriteConfirmed(receipt, ApplyResult{"failed", m.Version, e.Error()}))
	}
	if e = localstate.WriteConfirmed(receipt, ApplyResult{State: "installed", Version: m.Version}); e != nil {
		return true, fmt.Errorf("installed version could not persist completion; inspect the original transaction: %w", e)
	}
	if p.Relaunch {
		command := exec.Command(filepath.Join(p.Target, "caelis-bot.exe"))
		command.Env = append(runtimeenv.Clean(os.Environ()), "CAELIS_BOT_DATA_DIR="+p.Root)
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
		if e = command.Start(); e != nil {
			return true, errors.Join(e, localstate.WriteConfirmed(receipt, ApplyResult{"failed", m.Version, "Update installed; restart Caelis Bot manually"}))
		}
		return true, command.Process.Release()
	}
	return true, nil
}

// Commit follows successful PrepareUpdate and core.Close. The helper checks
// this acknowledgement only after its inherited original-process handle exits.
func Commit(directory, version string) error {
	if !filepath.IsAbs(directory) || filepath.Base(filepath.Dir(directory)) != "Updates" {
		return errors.New("invalid update transaction directory")
	}
	if err := safeDirectory(directory); err != nil {
		return err
	}
	return localstate.WriteConfirmed(filepath.Join(directory, "commit.json"), struct {
		PID     int    `json:"pid"`
		Version string `json:"version"`
	}{os.Getpid(), version})
}
func apply(p Plan, m Manifest, work string) error {
	if err := TerminalIdle(p.Target); err != nil {
		return err
	}
	if p.Format == "nsis" {
		command := exec.Command(p.Archive)
		command.Env = runtimeenv.Clean(os.Environ())
		// NSIS requires /D last and unquoted, including a path containing spaces.
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW,
			CmdLine: windows.ComposeCommandLine([]string{p.Archive, "/S"}) + " /D=" + p.Target}
		if e := command.Run(); e != nil {
			return e
		}
	} else {
		localWork, e := stagePortableArchive(p.Target, p.Archive, p.Identity, m.Version)
		if e != nil {
			return e
		}
		if e = localstate.WriteConfirmed(filepath.Join(work, "portable-work.json"), map[string]string{"directory": localWork}); e != nil {
			return e
		}
		lease, e := stoppedLease(filepath.Join(p.Target, "caelis-bot.exe"))
		if e != nil {
			return e
		}
		defer lease()
		if e = ReplacePortable(p.Target, filepath.Join(localWork, "extracted"), filepath.Join(localWork, "backup"), p.Identity, m.Version); e != nil {
			return e
		}
		// Successful replacement leaves the durable receipt in AppData. Keep
		// backups until post-install verification below has also succeeded.
		work = localWork
	}
	data, e := os.ReadFile(filepath.Join(p.Target, "build.json"))
	var build struct{ Version string }
	if e != nil || json.Unmarshal(data, &build) != nil || build.Version != m.Version {
		return errors.New("installed version verification failed")
	}
	if p.Format == "zip" {
		_ = os.RemoveAll(work)
	}
	return nil
}
func stoppedLease(executable string) (func(), error) {
	name, e := windows.UTF16PtrFromString(executable)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(name, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
	if e != nil {
		return nil, errors.New("another Bot or MCP process is using the program; no processes were stopped")
	}
	return func() { windows.CloseHandle(h) }, nil
}

// Called after the authorized observer-close step. A remaining mapped helper
// indicates a new or unconfirmed owner, so it must not be overwritten blindly.
func TerminalIdle(target string) error {
	executable := filepath.Join(target, "caelis-terminal.exe")
	if _, err := os.Lstat(executable); os.IsNotExist(err) {
		return nil
	}
	release, err := stoppedLease(executable)
	if err != nil {
		return errors.New("close local task terminals before installing the update")
	}
	release()
	return nil
}
