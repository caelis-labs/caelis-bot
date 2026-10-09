package remotework

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type owner interface {
	api.WorkRuntime
	api.WorkRetirer
	api.WorkTerminalProvider
	Connect(context.Context) error
	SetModel(context.Context, api.WorkExecutionSettings, func() error) error
	Models(context.Context) ([]api.ModelOption, error)
	RuntimeDefault(context.Context) (api.WorkExecutionSettings, error)
}
type Owner struct {
	mu         sync.Mutex
	root       string
	codexSetup codex.Setup
	workers    map[string]owner
	identity   string
}

func New(root string) (*Owner, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("private owner directory required")
	}
	if err := privateDir(root); err != nil {
		return nil, err
	}
	b, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return nil, errors.New("target identity unavailable")
	}
	home, _ := os.UserHomeDir()
	sum := sha256.Sum256(append(b, []byte(home)...))
	return &Owner{root: root, workers: map[string]owner{}, identity: hex.EncodeToString(sum[:])}, nil
}
func privateDir(p string) error {
	if err := os.MkdirAll(p, 0700); err != nil {
		return err
	}
	i, e := os.Lstat(p)
	if e != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 || i.Mode().Perm()&0077 != 0 {
		return errors.New("owner directory is redirected or not private")
	}
	return nil
}
func find(name string) string {
	if p, e := exec.LookPath(name); e == nil {
		p, _ = filepath.Abs(p)
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".local/bin", name), filepath.Join(home, ".npm-global/bin", name), filepath.Join(home, ".cargo/bin", name), "/usr/local/bin/" + name, "/usr/bin/" + name} {
		if i, e := os.Stat(p); e == nil && i.Mode().IsRegular() && i.Mode()&0111 != 0 {
			return p
		}
	}
	return ""
}
func (o *Owner) settings(runtime string) api.RuntimeSettings {
	return api.RuntimeSettings{Runtime: runtime, CLIPath: find(runtime)}
}
func (o *Owner) model(runtime string) (api.WorkExecutionSettings, error) {
	var v api.WorkExecutionSettings
	b, e := os.ReadFile(filepath.Join(o.root, runtime, "model.json"))
	if os.IsNotExist(e) {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	if strings.TrimSpace(string(b)) == "null" {
		return v, errors.New("remote_model_invalid")
	}
	if e = Decode(b, &v); e != nil || api.ValidateExecutionSettings(v.Execution()) != nil {
		return v, errors.New("remote_model_invalid")
	}
	return v, nil
}
func (o *Owner) open(ctx context.Context, runtime string) (owner, error) {
	if w := o.workers[runtime]; w != nil {
		return w, nil
	}
	d := filepath.Join(o.root, runtime)
	if err := privateDir(d); err != nil {
		return nil, err
	}
	settings := o.settings(runtime)
	if settings.CLIPath == "" {
		return nil, errors.New("runtime missing")
	}
	model, err := o.model(runtime)
	if err != nil {
		return nil, err
	}
	var w owner
	if runtime == "codex" {
		socket := ""
		b, e := os.ReadFile(filepath.Join(d, "native-endpoint.json"))
		if e == nil {
			var t api.TerminalTarget
			err = Decode(b, &t)
			if err != nil {
				return nil, err
			}
			socket = strings.TrimPrefix(t.Endpoint, "unix://")
		} else if !os.IsNotExist(e) {
			return nil, e
		}
		w, err = codex.NewWorkOwner(codex.SessionOptions{Binary: settings.CLIPath, Socket: socket, Directory: d, StateFile: filepath.Join(d, "worker-bindings.json"), WorkRoot: filepath.Join(d, "Tasks"), WorkExecution: model, Execution: api.ExecutionSettings{ApprovalMode: "auto"}})
	} else if runtime == "caelis" {
		w, err = caelis.NewWorkOwner(caelis.Options{Directory: d, Settings: settings, WorkExecution: model})
	} else {
		return nil, errors.New("unsupported runtime")
	}
	if err != nil {
		return nil, err
	}
	if err = w.Connect(ctx); err != nil {
		return nil, err
	}
	if runtime == "codex" {
		endpoint, e := w.(*codex.WorkOwner).NativeEndpoint()
		if e != nil {
			return nil, e
		}
		if e = localstate.Write(filepath.Join(d, "native-endpoint.json"), api.TerminalTarget{Endpoint: endpoint}); e != nil {
			return nil, e
		}
	}
	o.workers[runtime] = w
	return w, nil
}
func (o *Owner) Handle(ctx context.Context, r Request) Response {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := Response{Version: Version, Identity: o.identity, Available: []string{}, States: []api.WorkState{}}
	for _, runtime := range []string{"codex", "caelis"} {
		if find(runtime) != "" {
			out.Available = append(out.Available, runtime)
		}
	}
	if r.Version != Version {
		out.Error = "incompatible helper"
		return out
	}
	if r.Action == "detect" {
		return out
	}
	if r.Runtime != "codex" && r.Runtime != "caelis" {
		out.Error = "unsupported runtime"
		return out
	}
	settings := o.settings(r.Runtime)
	var err error
	out.Model, err = o.model(r.Runtime)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if r.Action == "inspect" {
		if settings.CLIPath == "" {
			out.Setup = api.SetupState{State: "missing", Models: []api.SetupChoice{}}
			return out
		}
		if r.Runtime == "codex" {
			out.Setup, err = o.codexSetup.Inspect(ctx, settings.CLIPath)
		} else {
			out.Setup, err = caelis.InspectSetup(ctx, settings)
		}
		out.Setup.Settings = settings
		if err == nil && out.Setup.State == "ready" {
			// Model authentication is independent of optional Team configuration.
			if out.Model.Model != "" {
				found := false
				for _, m := range out.Setup.Models {
					if m.Value == out.Model.Model && !m.NoAuth {
						found = true
					}
				}
				if !found {
					out.Setup.State = "models"
				}
			}
			if out.Setup.State == "ready" {
				var w owner
				w, err = o.open(ctx, r.Runtime)
				if err == nil {
					if admission := w.WorkAdmission(ctx); admission != nil {
						out.Setup.State = "models"
					}
				}
			}
		}
		// Read capabilities even when an old selection needs repair. This path
		// does not read optional Team configuration or weaken basic readiness.
		if err == nil && (out.Setup.State == "ready" || out.Setup.State == "models") {
			if w, openErr := o.open(ctx, r.Runtime); openErr == nil {
				out.Models, _ = w.Models(ctx)
				if v, defaultErr := w.RuntimeDefault(ctx); defaultErr == nil && v.Model != "" {
					out.RuntimeDefault = &v
				}
			}
		}
		if err != nil {
			out.Error = err.Error()
		}
		return out
	}
	if r.Action == "configuration" || r.Action == "team" {
		if r.Runtime != "caelis" {
			out.Error = "optional configuration belongs to Caelis"
			return out
		}
		if r.Action == "team" {
			out.Mutation, err = caelis.ChangeRuntimeConfiguration(ctx, settings, r.Team)
		} else {
			var c api.RuntimeConfiguration
			c, err = caelis.ReadRuntimeConfiguration(ctx, settings)
			if err == nil {
				out.Configuration = &c
			} else {
				out.AdvancedIssue = err.Error()
			}
		}
		if r.Action == "team" && err != nil {
			out.Error = err.Error()
		}
		return out
	}
	if r.Action == "tui" {
		home, _ := os.UserHomeDir()
		out.Terminal = api.TerminalTarget{Runtime: "setup", Binary: settings.CLIPath, Directory: home}
		return out
	}
	w, err := o.open(ctx, r.Runtime)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	switch r.Action {
	case "prepare":
		root := filepath.Join(o.root, r.Runtime, "Tasks")
		if err = privateDir(root); err == nil {
			p := filepath.Join(root, r.ID)
			if !strings.HasPrefix(r.ID, "task-") || strings.ContainsAny(r.ID, "/\\\x00") {
				err = errors.New("invalid task identity")
			} else if r.Start.TaskStart.Workspace != "" {
				p, err = api.ResolveTaskWorkspace(r.Start.TaskStart.Workspace)
			} else {
				err = privateDir(p)
			}
			out.Task = api.Task{ID: r.ID, Workspace: p}
		}
	case "start":
		out.Task, err = w.StartWork(ctx, r.Start)
	case "read":
		out.Task, err = w.ReadWork(ctx, r.ID)
	case "retire":
		out.Task, err = w.RetireWork(ctx, r.ID)
	case "send":
		out.Task, err = w.SendWork(ctx, r.Message)
	case "stop":
		out.Task, err = w.StopWork(ctx, r.ID)
	case "states":
		out.States = w.WorkStates()
	case "terminal":
		out.Terminal, err = w.WorkTerminal(ctx, r.ID)
	case "model":
		// The compact picker chooses the native model's default reasoning level.
		// Explicitly supplied options still undergo the adapter's validation.
		if r.Model.Model != "" && r.Model.Effort == "" {
			var models []api.ModelOption
			models, err = w.Models(ctx)
			if err != nil {
				break
			}
			for _, m := range models {
				if m.Model == r.Model.Model {
					r.Model.Effort = m.DefaultEffort
					break
				}
			}
		}
		err = w.SetModel(ctx, r.Model, func() error { return localstate.Write(filepath.Join(o.root, r.Runtime, "model.json"), r.Model) })
		out.Model, _ = o.model(r.Runtime)
	default:
		err = errors.New("unsupported owner operation")
	}
	if err != nil {
		out.Error = err.Error()
	}
	return out
}
