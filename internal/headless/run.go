// Package headless hosts the existing Bot product without native windows.
package headless

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/desktop"
	"github.com/caelis-labs/caelis-bot/internal/managedupdate"
	"github.com/caelis-labs/caelis-bot/internal/websurface"
)

type origins []string

func (o *origins) String() string     { return fmt.Sprint([]string(*o)) }
func (o *origins) Set(s string) error { *o = append(*o, s); return nil }

func Run(assets fs.FS, args []string, version string) error {
	if len(args) == 0 {
		return errors.New("use serve or pair")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	root := flags.String("data-dir", filepath.Join(base, "caelisbot"), "private Bot data directory")
	listen := flags.String("listen", "127.0.0.1:9317", "HTTP listener (guest loopback is not host loopback)")
	requirePairing := flags.Bool("require-pairing", false, "require pairing even for direct loopback; use for reverse-proxy upstreams")
	proxyFile := flags.String("local-proxy-key-file", "", "private shared proof file for the optional host-loopback relay")
	web := flags.Bool("web", true, "enable browser surface")
	var allowed origins
	flags.Var(&allowed, "origin", "exact browser origin; repeat for additional hosts")
	if e := flags.Parse(args[1:]); e != nil {
		return e
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	directory, e := filepath.Abs(*root)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(directory, 0700); e != nil {
		return e
	}
	info, e := os.Lstat(directory)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("data directory must be private (0700), not a symlink")
	}
	webDir := filepath.Join(directory, "Web")
	if args[0] == "pair" {
		if e = websurface.CreatePairing(webDir); e != nil {
			return e
		}
		fmt.Println("Read the one-time code from:", filepath.Join(webDir, "pairing-code"))
		return nil
	}
	if args[0] != "serve" {
		return errors.New("use serve or pair")
	}
	unlock, e := lock(filepath.Join(directory, "headless.lock"))
	if e != nil {
		return e
	}
	defer unlock()
	var listener net.Listener
	if *web {
		listener, e = net.Listen("tcp", *listen)
		if e != nil {
			return e
		}
		defer listener.Close()
		if len(allowed) == 0 {
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return errors.New("non-loopback listening requires an explicit --origin; use a host loopback port mapping or private HTTPS proxy")
			}
			allowed = append(allowed, "http://"+net.JoinHostPort(host, port))
		}
	}
	core, surface, e := desktop.NewHeadlessProduct(directory)
	if e != nil {
		return e
	}
	defer core.Close()
	managed, e := openManaged(version)
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if !*web {
		if managed != nil {
			return errors.New("managed updates require the web surface")
		}
		core.StartBackground()
		fmt.Println("CaelisBot is running without a browser surface.")
		<-ctx.Done()
		return nil
	}
	host, _, _ := net.SplitHostPort(listener.Addr().String())
	direct := !*requirePairing && net.ParseIP(host).IsLoopback()
	proxyKey := ""
	if *proxyFile != "" {
		info, err := os.Lstat(*proxyFile)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("local proxy key must be a private regular file")
		}
		data, err := os.ReadFile(*proxyFile)
		if err != nil {
			return err
		}
		proxyKey = strings.TrimSpace(string(data))
		if len(proxyKey) != 64 {
			return errors.New("local proxy key must contain a 256-bit hexadecimal key")
		}
		for _, c := range proxyKey {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return errors.New("invalid local proxy key")
			}
		}
	}
	server, e := websurface.New(websurface.Config{Directory: webDir, Origins: allowed, Version: version, DirectLoopback: direct, LocalProxyKey: proxyKey}, websurface.Services{App: core, Backend: core.Backend, Surface: surface}, assets)
	if e != nil {
		return e
	}
	if managed != nil {
		server.RegisterManagedUpdates(managed.client)
	} else {
		external := managedupdate.NewExternal(managedupdate.Identity{Version: version, Source: managedupdate.Source, Kind: managedupdate.BuildKind})
		defer external.Close()
		server.RegisterManagedUpdates(external)
	}
	httpServer := &http.Server{Handler: core.WorkerPeerHandler(managed.handler(server), "http://"+listener.Addr().String()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	if e = managed.markReady(core); e != nil {
		server.Close()
		_ = httpServer.Close()
		return e
	}
	core.StartBackground()
	updateDone := make(chan error, 1)
	if managed != nil {
		go managed.watch(ctx, core, server, updateDone)
	}
	fmt.Println("CaelisBot:", allowed[0])
	fmt.Println("Pairing code is in the private file:", filepath.Join(webDir, "pairing-code"))
	select {
	case <-ctx.Done():
	case e = <-updateDone:
		server.Close()
		_ = httpServer.Close()
		return e
	case e = <-done:
		if !errors.Is(e, http.ErrServerClosed) {
			server.Close()
			return e
		}
	}
	server.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdown)
}
