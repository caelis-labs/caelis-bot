package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localipc"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// Run owns explicit foreground agent commands. There is no autostart/daemon,
// credential provisioning, automatic remote copy or execution lease here.
func Run(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: caelis-agent serve-agent|proxy-agent|join-agent|verify-join-directory")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(out)
	switch args[0] {
	case "serve-agent":
		stdio := f.Bool("stdio", false, "serve framed protocol over this foreground process stdin/stdout")
		directory := f.String("directory", "", "existing private user-owned agent directory")
		runtimeDirectory := f.String("runtime-directory", "", "optional explicit Linux managed installer directory")
		id := f.String("node-id", "", "native enrolled node identity; persisted on first start")
		label := f.String("label", "Runtime node", "human machine label")
		join := f.String("join", "ssh", "native join kind: local, ssh, outgoing")
		codexBinary := f.String("codex-binary", "", "explicit target-local Codex executable")
		caelisBinary := f.String("caelis-binary", "", "explicit target-local Caelis executable")
		caelisStore := f.String("caelis-store", "", "explicit existing target-local Caelis Store; no credential transfer")
		nativeHealth := f.Bool("native-health", true, "inspect native authentication and service health without starting a task")
		if err := f.Parse(args[1:]); err != nil {
			return help(err)
		}
		if f.NArg() != 0 {
			return errors.New("unexpected agent argument")
		}
		if err := CheckPrivateDirectory(*directory); err != nil {
			return err
		}
		unlock, err := lockAgent(*directory)
		if err != nil {
			return err
		}
		defer unlock()
		binaries := map[api.NodeBackend]string{}
		if *codexBinary != "" {
			binaries[api.NodeCodex] = *codexBinary
		}
		if *caelisBinary != "" {
			binaries[api.NodeCaelis] = *caelisBinary
		}
		codexConfig := &CodexConfiguration{Directory: *directory, Binary: *codexBinary}
		configurations := map[api.NodeBackend]NativeConfiguration{api.NodeCodex: codexConfig}
		var caelisConfig *CaelisConfiguration
		if *caelisStore != "" {
			if !filepath.IsAbs(*caelisStore) {
				return errors.New("Caelis Store must be an explicit native absolute path")
			}
			caelisConfig = &CaelisConfiguration{Settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: *caelisBinary, CaelisStore: *caelisStore}}
			configurations[api.NodeCaelis] = caelisConfig
		}
		o := Options{Directory: *directory, RuntimeDirectory: *runtimeDirectory, NodeID: *id, Label: *label, Join: api.NodeJoin(*join), Binaries: binaries, Configurations: configurations}
		if *nativeHealth {
			o.Health = func(ctx context.Context, b api.NodeBackend) (NativeHealth, error) {
				if b == api.NodeCodex {
					return codexConfig.Health(ctx)
				}
				if caelisConfig != nil {
					return caelisConfig.Health(ctx)
				}
				return NativeHealth{}, nil
			}
		}
		service, err := New(o)
		if err != nil {
			return err
		}
		if *stdio {
			return productrpc.ServeNativeStream(ctx, in, out, Handler(service), allowed)
		}
		socket := filepath.Join(*directory, "agent.sock")
		if !validSocket(socket) {
			return errors.New("agent directory exceeds private IPC socket path limit")
		}
		if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
			return errors.New("agent socket already exists; inspect its foreground owner")
		}
		listener, err := net.Listen("unix", socket)
		if err != nil {
			return errors.New("private agent listener unavailable")
		}
		defer listener.Close()
		if err := os.Chmod(socket, 0600); err != nil {
			return err
		}
		if err := json.NewEncoder(out).Encode(struct {
			NodeID string `json:"nodeId"`
			Socket string `json:"socket"`
		}{service.options.NodeID, socket}); err != nil {
			return err
		}
		return Serve(ctx, listener, service)
	case "proxy-agent":
		socket := f.String("socket", "", "private target-local agent socket")
		if err := f.Parse(args[1:]); err != nil {
			return help(err)
		}
		if f.NArg() != 0 || !validSocket(*socket) {
			return errors.New("explicit private agent socket required")
		}
		if err := CheckPrivateDirectory(filepath.Dir(*socket)); err != nil {
			return err
		}
		connection, err := localipc.Dial(*socket, 10*time.Second)
		if err != nil {
			return errors.New("private agent unavailable")
		}
		defer connection.Close()
		stop := context.AfterFunc(ctx, func() {
			connection.Close()
			if closer, ok := in.(io.Closer); ok {
				closer.Close()
			}
		})
		defer stop()
		copied := make(chan struct{})
		go func() {
			_, _ = io.Copy(connection, in)
			if c, ok := connection.(*net.UnixConn); ok {
				_ = c.CloseWrite()
			}
			close(copied)
		}()
		_, err = io.Copy(out, connection)
		connection.Close()
		if closer, ok := in.(io.Closer); ok {
			closer.Close()
		}
		<-copied
		return err
	case "join-agent":
		socket := f.String("socket", "", "existing local private agent socket")
		target := f.String("ssh-target", "", "existing authorized SSH destination")
		helper := f.String("join-helper", "", "absolute native helper executable on the destination")
		directory := f.String("join-directory", "", "existing private destination user-owned directory")
		if err := f.Parse(args[1:]); err != nil {
			return help(err)
		}
		if f.NArg() != 0 {
			return errors.New("unexpected join argument")
		}
		if err := CheckPrivateDirectory(filepath.Dir(*socket)); err != nil {
			return err
		}
		// Read-only private socket connection verifies the local foreground owner.
		connection, err := localipc.Dial(*socket, 3*time.Second)
		if err != nil {
			return errors.New("local private agent unavailable")
		}
		connection.Close()
		return Join(ctx, SSHConfig{Target: *target}, *helper, *directory, *socket)
	case "verify-join-directory":
		directory := f.String("directory", "", "explicit destination user-owned private directory")
		hold := f.Bool("hold", false, "hold foreground reverse SSH forwarding after verification")
		if err := f.Parse(args[1:]); err != nil {
			return help(err)
		}
		if f.NArg() != 0 {
			return errors.New("unexpected verification argument")
		}
		if err := CheckPrivateDirectory(*directory); err != nil {
			return err
		}
		if *hold {
			<-ctx.Done()
		}
		return nil
	default:
		return errors.New("unknown headless agent command")
	}
}
func help(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}
