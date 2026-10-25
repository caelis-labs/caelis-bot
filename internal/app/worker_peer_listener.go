package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

type peerListenerConfig struct {
	Listen    string `json:"listen"`
	Address   string `json:"address,omitempty"`
	Dedicated bool   `json:"dedicated,omitempty"`
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
		if peerListenerAddressInUse(err) {
			return errors.New("worker_listener_in_use")
		}
		return errors.New("worker_listener_unavailable")
	}
	c.Listen = ln.Addr().String()
	if c.Dedicated && c.Address == "" {
		addresses := peerNetworkAddresses(c.Listen)
		c.Address = addresses[0]
	}
	if err = localstate.Write(filepath.Join(a.root, "WorkerPeers", "listener.json"), c); err != nil {
		ln.Close()
		return err
	}
	server := &http.Server{Handler: a.peer.server, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	old := a.peer.http
	a.peer.http, a.peer.listen, a.peer.address = server, c.Listen, "http://"+c.Listen
	a.peer.configuration, a.peer.configurationErr = c, nil
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.peer.mu.Lock()
			if a.peer.http == server {
				a.peer.http = nil
				a.peer.configurationErr = errors.New("worker_listener_unavailable")
			}
			a.peer.mu.Unlock()
		}
	}()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// Local interface enumeration is only publication, not discovery or trust.
// All advertised addresses reach the same independently authenticated listener.
func peerNetworkAddresses(listen string) []string {
	host, port, _ := net.SplitHostPort(listen)
	if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}
	addresses := []string{}
	interfaces, _ := net.InterfaceAddrs()
	for _, address := range interfaces {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && ip.IsGlobalUnicast() && !ip.IsLoopback() {
			addresses = append(addresses, "http://"+net.JoinHostPort(ip.String(), port))
		}
	}
	slices.Sort(addresses)
	addresses = slices.Compact(addresses)
	// No network interface: loopback still works with an externally prepared
	// tunnel. The UI explicitly labels it; it is never claimed cross-machine.
	return append(addresses, "http://127.0.0.1:"+port)
}
