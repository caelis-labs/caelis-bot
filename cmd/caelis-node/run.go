package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "deploy-joined-roaming":
			return runJoinedRoamingDeploy(ctx, args, out)
		case "supervise-roaming", "control-roaming", "inspect-roaming":
			return runRoamingDeploy(ctx, args, out)
		case "serve-roaming":
			return runRoaming(ctx, args, out)
		case "owned-runtime-watchdog":
			return runOwnedWatchdog(ctx, args[1:], out)
		case "serve-broker", "proxy-broker":
			return runBroker(ctx, args, out)
		case "serve-agent", "proxy-agent", "join-agent", "verify-join-directory", "prepare-owned-caelis-store":
			return runAgent(ctx, args, out)
		}
	}
	if len(args) > 0 && (args[0] == "serve-worker" || args[0] == "proxy-worker") {
		return runWorker(ctx, args, out)
	}
	if len(args) > 0 && args[0] == "proxy-product" {
		f := flag.NewFlagSet("proxy-product", flag.ContinueOnError)
		f.SetOutput(out)
		endpoint := f.String("endpoint", "", "target-local product loopback endpoint")
		auth := f.String("auth-file", "", "target-local private application product token file")
		if err := f.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if f.NArg() != 0 || !filepath.IsAbs(*auth) {
			return errors.New("explicit target-local product auth file required")
		}
		token, err := readProductToken(*auth)
		if err != nil {
			return err
		}
		return productrpc.ProxyStdio(ctx, os.Stdin, out, *endpoint, token)
	}
	if len(args) == 0 || args[0] != "serve-bot" {
		return errors.New("usage: caelis-node serve-bot --profile ABS --listen 127.0.0.1:0 --auth-file PRIVATE")
	}
	f := flag.NewFlagSet("serve-bot", flag.ContinueOnError)
	f.SetOutput(out)
	profile := f.String("profile", "", "explicit isolated application profile")
	listen := f.String("listen", "127.0.0.1:0", "literal loopback listener")
	auth := f.String("auth-file", "", "private application-local product token file (not Runtime credentials)")
	var selected selectedWorkers
	f.Var(&selected, "connect-worker", "explicit configured NODE/BACKEND Worker; repeat for each target")
	runtimeDirectory := f.String("runtime-directory", "", "optional target-local managed Runtime directory inside user HOME; no automatic installation")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 || !filepath.IsAbs(*profile) || !filepath.IsAbs(*auth) {
		return errors.New("explicit absolute profile and product auth file required")
	}
	addr, err := net.ResolveTCPAddr("tcp", *listen)
	if err != nil || !addr.IP.IsLoopback() {
		return errors.New("product listener must use a literal loopback address")
	}
	// Reject DNS names even when they happen to resolve to loopback.
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil {
		return errors.New("literal loopback listener required")
	}
	if err = app.ValidateConfiguredWorkerTargets(*profile, selected); err != nil {
		return err
	}
	token, err := readProductToken(*auth)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(*profile, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(*profile)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("application profile must be a private directory")
	}
	unlock, err := lockProfile(filepath.Join(*profile, ".product-owner.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if err = rejectRemoteProfile(*profile); err != nil {
		return err
	}
	files, err := productrpc.OpenUploads(filepath.Join(*profile, "Product", "Uploads"))
	if err != nil {
		return err
	}
	var server atomic.Pointer[productrpc.Server]
	application, err := app.New(*profile, app.Host{
		ResolveFiles: files.Resolve, ConsumeFiles: func([]string) {},
		Observe: func(api.Snapshot) {
			if s := server.Load(); s != nil {
				s.NotifySnapshot()
			}
		},
		ObserveTasks: func([]api.TaskPreview) {
			if s := server.Load(); s != nil {
				s.NotifySnapshot()
			}
		},
		OpenURL: func(string) error { return productrpc.ErrUnsupported }, RevealFile: func(string) error { return productrpc.ErrUnsupported },
	})
	if err != nil {
		return err
	}
	return runResidentOwner(ctx, application, application.Backend, selected, func() (func() error, error) {
		nodeID, botID, err := profileIdentity(*profile)
		if err != nil {
			return nil, err
		}
		port := productrpc.ServicePort{Service: application.Backend, Stop: func(context.Context) error { return application.Close() }}
		files.Artifacts = func(ctx context.Context, id string) (productrpc.Resource, io.ReadCloser, error) {
			artifact, err := application.Backend.ReadProductArtifact(ctx, id)
			if err != nil {
				return productrpc.Resource{}, nil, err
			}
			return productrpc.Resource{ID: id, Name: artifact.Name, Size: artifact.Size, SHA256: artifact.SHA256}, io.NopCloser(bytes.NewReader(artifact.Bytes)), nil
		}
		var management func(productmanagement.Scope) (productmanagement.Port, error)
		if *runtimeDirectory != "" {
			if !filepath.IsAbs(*runtimeDirectory) {
				return nil, errors.New("managed Runtime directory must be absolute")
			}
			management = func(scope productmanagement.Scope) (productmanagement.Port, error) {
				var configuration productmanagement.Configuration
				if application.Backend.RuntimeSettings().Runtime == "caelis" {
					configuration = application.Backend
				}
				return productmanagement.New(scope, *runtimeDirectory, configuration)
			}
		}
		var execution func(productmanagement.Scope) (productmanagement.ExecutionPort, error)
		if application.Backend.ModelSettingsAvailable() {
			execution = func(scope productmanagement.Scope) (productmanagement.ExecutionPort, error) {
				return productmanagement.NewExecution(scope, application.Backend)
			}
		}
		stopped := make(chan productrpc.Result, 1)
		s, err := productrpc.NewServer(port, productrpc.Options{Context: ctx, NodeID: nodeID, BotID: botID, Token: token, JournalFile: filepath.Join(*profile, "Product", "receipts.json"), Resources: files, Capabilities: productrpc.Capabilities{Files: true, Interrupt: port.ExactInterruptAvailable()}, Management: management, Execution: execution, OnStopped: func(r productrpc.Result) { stopped <- r }})
		if err != nil {
			return nil, err
		}
		server.Store(s)
		return func() error {
			l, err := net.ListenTCP("tcp", addr)
			if err != nil {
				return err
			}
			defer l.Close()
			if err = ctx.Err(); err != nil {
				return err
			}
			// Pairing metadata contains no credential/native session binding. The token
			// stays in its target-local file; SSH authentication/forwarding is external.
			if err = json.NewEncoder(out).Encode(struct {
				Endpoint string              `json:"endpoint"`
				Identity productrpc.Identity `json:"identity"`
			}{"http://" + l.Addr().String(), s.Identity()}); err != nil {
				return err
			}
			done := make(chan error, 1)
			go func() { done <- s.Serve(l) }()
			select {
			case result := <-stopped:
				_ = l.Close()
				if result.Outcome != "accepted" {
					return errors.New("product stop outcome unconfirmed: " + result.Code)
				}
				return nil
			case err = <-done:
				return err
			case <-ctx.Done():
				// Signal shutdown is an owner action. Observer EOF never reaches this.
				_ = l.Close()
				return application.Close()
			}
		}, nil
	})
}

// This resident-only entry point cannot recursively assemble a thin APP. Read
// only the native selector before app.New; normal APP owns its full validation.
func rejectRemoteProfile(root string) error {
	path := filepath.Join(root, "product-connection.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("product selector must be private")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var selector struct {
		Version int `json:"version"`
		Pairing struct {
			Mode string `json:"mode"`
		} `json:"pairing"`
	}
	if len(b) > 16<<10 || json.Unmarshal(b, &selector) != nil || selector.Version != 1 {
		return errors.New("invalid native product selector")
	}
	if selector.Pairing.Mode == "remote" {
		return errors.New("serve-bot requires a resident local profile")
	}
	if selector.Pairing.Mode != "" && selector.Pairing.Mode != "local" {
		return errors.New("unknown native product mode")
	}
	return nil
}

func readProductToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("product auth file must be a private regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("product auth file changed")
	}
	b, err := io.ReadAll(io.LimitReader(f, 258))
	if err != nil {
		return "", err
	}
	token := strings.TrimSuffix(string(b), "\n")
	if len(token) < 32 || len(token) > 256 || strings.ContainsAny(token, "\r\n \t") {
		return "", errors.New("invalid application-local product token")
	}
	return token, nil
}

// Read identity only while the profile owner lock is held, before App.Start.
// RuntimeNative is the existing runtime.json/execution profile, not a wire ID.
func profileIdentity(root string) (string, string, error) {
	b, err := os.ReadFile(filepath.Join(root, "bot.json"))
	if err != nil {
		return "", "", err
	}
	var bot struct {
		ID string `json:"id"`
	}
	if len(b) > 1<<20 || json.Unmarshal(b, &bot) != nil || bot.ID == "" {
		return "", "", errors.New("invalid resident identity")
	}
	botID := productrpc.ProfileBotID(bot.ID)
	path := filepath.Join(root, "Product", "node.json")
	var node struct {
		ID string `json:"id"`
	}
	b, err = os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		node.ID = "node-" + rand.Text()
		err = localstate.Write(path, node)
	} else if err == nil {
		if len(b) > 1024 || json.Unmarshal(b, &node) != nil || !strings.HasPrefix(node.ID, "node-") || len(node.ID) > 128 {
			err = errors.New("invalid product node identity")
		}
	}
	return node.ID, botID, err
}
