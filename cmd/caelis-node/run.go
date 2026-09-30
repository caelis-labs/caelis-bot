package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func run(ctx context.Context, args []string, out io.Writer) error {
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
	defer application.Close()
	if err = application.PreparePersonal(); err != nil {
		return err
	}
	nodeID, botID, err := profileIdentity(*profile)
	if err != nil {
		return err
	}
	port := productrpc.ServicePort{Service: application.Backend, Stop: func(context.Context) error { return application.Close() }}
	files.Artifacts = func(ctx context.Context, id string) (productrpc.Resource, io.ReadCloser, error) {
		artifact, err := application.Backend.ReadProductArtifact(ctx, id)
		if err != nil {
			return productrpc.Resource{}, nil, err
		}
		return productrpc.Resource{ID: id, Name: artifact.Name, Size: artifact.Size, SHA256: artifact.SHA256}, io.NopCloser(bytes.NewReader(artifact.Bytes)), nil
	}
	s, err := productrpc.NewServer(port, productrpc.Options{Context: ctx, NodeID: nodeID, BotID: botID, Token: token, JournalFile: filepath.Join(*profile, "Product", "receipts.json"), Resources: files, Capabilities: productrpc.Capabilities{Files: true, Interrupt: port.ExactInterruptAvailable()}})
	if err != nil {
		return err
	}
	server.Store(s)
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return err
	}
	defer l.Close()
	if err = application.Start(); err != nil {
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
	case err = <-done:
		return err
	case <-ctx.Done():
		// Signal shutdown is an owner action. Observer EOF never reaches this.
		_ = l.Close()
		return application.Close()
	}
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
	h := sha256.Sum256([]byte("caelis-product-bot\x00" + bot.ID))
	botID := "bot-" + hex.EncodeToString(h[:])
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
