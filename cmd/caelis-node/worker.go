//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/leasepower"
	"github.com/caelis-labs/caelis-bot/internal/nodebroker"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// workerConfiguration is trusted target-native configuration. It is never a
// Worker frame, model input or renderer DTO; credentials have no fields here.
type workerConfiguration struct {
	Version   int                       `json:"version"`
	Lease     *workerLeaseConfiguration `json:"lease,omitempty"`
	Pair      workerwire.Pair           `json:"pair"`
	Directory string                    `json:"directory"`
	Binary    string                    `json:"binary"`
	Socket    string                    `json:"socket"`
	Execution api.WorkExecutionSettings `json:"execution"`
}

type workerLeaseConfiguration struct {
	RawBotID     string `json:"rawBotId"`
	BrokerNodeID string `json:"brokerNodeId"`
	BrokerSocket string `json:"brokerSocket"`
}

var workerIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func workerFileOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

func readWorkerConfiguration(path string) (workerConfiguration, error) {
	var config workerConfiguration
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return config, errors.New("explicit absolute Worker configuration required")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 16<<10 || !workerFileOwner(info) {
		return config, errors.New("Worker configuration must be a bounded private regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return config, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return config, errors.New("Worker configuration changed")
	}
	b, err := io.ReadAll(io.LimitReader(f, 16<<10+1))
	if err != nil || len(b) > 16<<10 {
		return config, errors.New("Worker configuration limit")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&config) != nil || d.Decode(new(any)) != io.EOF || config.Version != 1 || config.Pair.Target.Validate() != nil || config.Pair.Target.Role != api.RoleWorker || config.Pair.Target.Backend != "codex" || !workerIdentifier.MatchString(config.Pair.Target.NodeID) || !workerIdentifier.MatchString(config.Pair.BotID) || !workerIdentifier.MatchString(config.Pair.SourceNode) || !workerIdentifier.MatchString(config.Pair.SourceBackend) {
		return config, errors.New("invalid exact Worker pairing/configuration")
	}
	for _, path := range []string{config.Directory, config.Binary, config.Socket} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, '\x00') {
			return config, errors.New("Worker paths must be explicit absolute native paths")
		}
	}
	if config.Lease != nil {
		if strings.TrimSpace(config.Lease.RawBotID) == "" || len(config.Lease.RawBotID) > 512 || strings.ContainsRune(config.Lease.RawBotID, '\x00') || api.ProfileBotID(config.Lease.RawBotID) != config.Pair.BotID || !workerIdentifier.MatchString(config.Lease.BrokerNodeID) || !filepath.IsAbs(config.Lease.BrokerSocket) || filepath.Clean(config.Lease.BrokerSocket) != config.Lease.BrokerSocket || strings.ContainsRune(config.Lease.BrokerSocket, '\x00') || config.Lease.BrokerSocket == config.Socket || (config.Pair.SourceBackend != "codex" && config.Pair.SourceBackend != "caelis") {
			return config, errors.New("leased Worker requires exact trusted broker identity and private socket")
		}
	}
	if filepath.Dir(config.Socket) != config.Directory {
		return config, errors.New("Worker socket must belong to its private owner directory")
	}
	if err = api.ValidateExecutionSettings(config.Execution.Execution()); err != nil {
		return config, err
	}
	binary, err := os.Stat(config.Binary)
	if err != nil || !binary.Mode().IsRegular() || binary.Mode().Perm()&0111 == 0 {
		return config, errors.New("explicit Worker Runtime executable unavailable")
	}
	return config, nil
}

func prepareWorkerDirectory(path string) error {
	parent := filepath.Dir(path)
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil || canonical != parent {
		return errors.New("Worker owner parent redirected")
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !workerFileOwner(info) {
		return errors.New("Worker owner parent must be private")
	}
	if err = os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	canonical, err = filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return errors.New("Worker owner directory redirected")
	}
	info, err = os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !workerFileOwner(info) {
		return errors.New("Worker owner directory must be private")
	}
	// A Worker cannot turn a stopped resident profile into another role.
	for _, name := range []string{"bot.json", "runtime.json", "product-connection.json"} {
		if _, err = os.Lstat(filepath.Join(path, name)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("Worker requires a dedicated owner directory")
		}
	}
	// Publish the owner-directory entry before native process/pairing authority.
	// Its inner journal sync cannot make a newly created parent entry durable.
	parentFile, err := os.Open(parent)
	if err != nil {
		return err
	}
	return errors.Join(parentFile.Sync(), parentFile.Close())
}

func runWorker(ctx context.Context, args []string, out io.Writer) (returnErr error) {
	if len(args) > 0 && args[0] == "proxy-worker" {
		flags := flag.NewFlagSet("proxy-worker", flag.ContinueOnError)
		flags.SetOutput(out)
		socket := flags.String("socket", "", "existing target-local private Worker socket")
		if err := flags.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if flags.NArg() != 0 || !filepath.IsAbs(*socket) {
			return errors.New("explicit private Worker socket required")
		}
		// No native owner/runtime construction or stop authority on this path.
		return workerwire.ProxyUnix(ctx, os.Stdin, out, *socket)
	}
	flags := flag.NewFlagSet("serve-worker", flag.ContinueOnError)
	flags.SetOutput(out)
	file := flags.String("config-file", "", "explicit private target-native Worker configuration")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected Worker arguments")
	}
	config, err := readWorkerConfiguration(*file)
	if err != nil {
		return err
	}
	if err = prepareWorkerDirectory(config.Directory); err != nil {
		return err
	}
	unlock, err := lockProfile(filepath.Join(config.Directory, ".product-owner.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if _, err = os.Lstat(config.Socket); !errors.Is(err, os.ErrNotExist) {
		return errors.New("Worker wire endpoint already present; original ownership retained")
	}
	var leased *codex.WorkerLeaseOptions
	if config.Lease != nil {
		broker, err := nodebroker.DialUnixForBroker(ctx, config.Lease.BrokerSocket, config.Lease.BrokerNodeID)
		if err != nil {
			return err
		}
		defer broker.Close()
		helper, err := os.Executable()
		if err != nil {
			return err
		}
		leased = &codex.WorkerLeaseOptions{HelperPath: helper, BrokerNodeID: config.Lease.BrokerNodeID, RawBotID: config.Lease.RawBotID, SourceNode: config.Pair.SourceNode, SourceBackend: config.Pair.SourceBackend, Reader: broker, BindPower: leasepower.Bind}
	}
	native := codex.NewWorker(codex.WorkerOptions{Lease: leased, Target: config.Pair.Target, Pair: &config.Pair, Directory: config.Directory, WorkRoot: filepath.Join(config.Directory, "Tasks"), Binary: config.Binary, Execution: config.Execution, Source: workerwire.SourceProvider()})
	owner := nodeworker.New(native)
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		returnErr = errors.Join(returnErr, owner.Stop(shutdown))
	}()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = owner.Start(startup)
	cancel()
	if err != nil {
		return err
	}
	server, err := workerwire.NewServer(owner, config.Pair)
	if err != nil {
		return err
	}
	life, cancelService := context.WithCancel(ctx)
	defer cancelService()
	var outputErr error
	err = server.ServeUnixReady(life, config.Socket, func() {
		outputErr = json.NewEncoder(out).Encode(struct {
			Version int             `json:"version"`
			Socket  string          `json:"socket"`
			Pair    workerwire.Pair `json:"pair"`
		}{1, config.Socket, config.Pair})
		if outputErr != nil {
			cancelService()
		}
	})
	if outputErr != nil {
		return outputErr
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	return err
}
