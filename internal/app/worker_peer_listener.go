package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

type peerListenerConfig struct {
	Listen  string `json:"listen"`
	Address string `json:"address,omitempty"`
}

func readPeerListener(path string) (peerListenerConfig, error) {
	c := peerListenerConfig{Listen: "127.0.0.1:0"}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if json.Unmarshal(b, &c) != nil {
		return c, errors.New("worker_listener_invalid")
	}
	return validatePeerListener(c)
}
func validatePeerListener(c peerListenerConfig) (peerListenerConfig, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(c.Listen))
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || n < 0 || n > 65535 || host != "localhost" && host != "" && net.ParseIP(host) == nil {
		return c, errors.New("worker_listener_invalid")
	}
	c.Listen = net.JoinHostPort(host, strconv.Itoa(n))
	if c.Address != "" {
		c.Address, err = workerpeer.Address(c.Address)
		if err != nil {
			return c, err
		}
		u, _ := url.Parse(c.Address)
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsUnspecified() {
			return c, errors.New("worker_reachable_address_required")
		}
	}
	if ip := net.ParseIP(host); (host == "" || ip != nil && ip.IsUnspecified()) && c.Address == "" {
		return c, errors.New("worker_reachable_address_required")
	}
	return c, nil
}

// The App owns this one listener. A successful bind and durable address precede
// replacing the old listener. Native Workers and their original routes survive.
func (a *Application) replacePeerListenerLocked(c peerListenerConfig) error {
	ln, err := net.Listen("tcp", c.Listen)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return errors.New("worker_listener_in_use")
		}
		return errors.New("worker_listener_unavailable")
	}
	c.Listen = ln.Addr().String()
	if err = localstate.Write(filepath.Join(a.root, "WorkerPeers", "listener.json"), c); err != nil {
		ln.Close()
		return err
	}
	server := &http.Server{Handler: a.peer.server, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	old := a.peer.http
	a.peer.http, a.peer.listen, a.peer.address = server, c.Listen, "http://"+c.Listen
	a.peer.configuration, a.peer.configurationErr = c, nil
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && a.host.ReportError != nil {
			a.host.ReportError(err)
		}
	}()
	if old != nil {
		_ = old.Close()
	}
	return nil
}
func (a *Application) SetWorkerEndpoint(in api.WorkerSharingInput) (api.WorkerSharing, error) {
	c, err := validatePeerListener(peerListenerConfig{Listen: in.Listen, Address: in.Address})
	if err != nil {
		return a.WorkerSharing(), err
	}
	enabled := a.peer.server.Status().Enabled
	a.peer.mu.Lock()
	if a.peer.mounted && c.Listen != a.peer.listen {
		err = errors.New("worker_listener_managed_headless")
	} else if !a.peer.mounted && (a.peer.http != nil || enabled) && c.Listen != a.peer.listen {
		err = a.replacePeerListenerLocked(c)
	} else {
		err = localstate.Write(filepath.Join(a.root, "WorkerPeers", "listener.json"), c)
		if err == nil {
			a.peer.configuration, a.peer.configurationErr = c, nil
		}
	}
	a.peer.mu.Unlock()
	return a.WorkerSharing(), err
}
