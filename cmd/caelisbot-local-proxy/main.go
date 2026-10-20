// Host-loopback companion for container/VM port forwards. No Bot execution owner.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localproxy"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	listen := flag.String("listen", "127.0.0.1:9317", "host-loopback browser address")
	upstream := flag.String("upstream", "http://127.0.0.1:9318", "host-loopback published container port")
	keyFile := flag.String("key-file", "", "private proof file shared with headless server")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("relay must bind an explicit loopback IP")
	}
	info, err := os.Lstat(*keyFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("key-file must be a private regular file")
	}
	key, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	handler, err := localproxy.New("http://"+*listen, *upstream, strings.TrimSpace(string(key)))
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	fmt.Println("CaelisBot local browser: http://" + *listen)
	select {
	case err = <-done:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
