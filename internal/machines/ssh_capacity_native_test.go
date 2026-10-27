//go:build !windows

package machines

import (
	"context"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real system OpenSSH; all keys, hosts, peer records and tasks are isolated.
func TestNativeDeletedSSHCapacityKeepsInflightAndSharedChannels(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_SSHD") != "1" {
		t.Skip("opt-in isolated OpenSSH acceptance")
	}
	root := t.TempDir()
	key := filepath.Join(root, "identity")
	hostKey := filepath.Join(root, "hostkey")
	for _, p := range []string{key, hostKey} {
		if out, err := exec.Command("/usr/bin/ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", p).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	put := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(root, "sshd_config")
	q := func(s string) string { return `"` + s + `"` }
	put(config, fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s\nPidFile %s\nAuthorizedKeysFile %s\nStrictModes no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nAllowUsers %s\n", port, q(hostKey), q(filepath.Join(root, "pid")), q(key+".pub"), os.Getenv("USER")))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/sbin/sshd", "-D", "-e", "-f", config)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); cmd.Wait() }()
	public, err := os.ReadFile(hostKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(root, "known_hosts")
	put(known, fmt.Sprintf("[127.0.0.1]:%d %s", port, public))
	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "fixture Bot") }))
	defer bot.Close()
	_, botPortText, _ := net.SplitHostPort(strings.TrimPrefix(bot.URL, "http://"))
	var botPort int
	fmt.Sscan(botPortText, &botPort)
	s, err := Open(filepath.Join(root, "machines"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = os.MkdirAll(filepath.Join(s.root, "SSH"), 0700); err != nil {
		t.Fatal(err)
	}
	var cfg strings.Builder
	for i := 0; i < 18; i++ {
		fmt.Fprintf(&cfg, "Host capacity-%d\n HostName 127.0.0.1\n Port %d\n User %s\n IdentityFile %s\n IdentitiesOnly yes\n BatchMode yes\n StrictHostKeyChecking yes\n UserKnownHostsFile %s\n", i, port, os.Getenv("USER"), q(key), q(known))
	}
	put(filepath.Join(s.root, "SSH", "config"), cfg.String())
	target := func(i int) api.SSHTarget { return api.SSHTarget{Host: fmt.Sprintf("capacity-%d", i), Port: botPort} }
	check := func(i int) {
		t.Helper()
		address, err := s.peerAddress(t.Context(), fmt.Sprintf("ssh://capacity-%d:%d", i, botPort))
		if err != nil {
			t.Fatal(i, err)
		}
		r, err := http.Get(address)
		if err != nil {
			t.Fatal(i, err)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal(i, r.StatusCode)
		}
	}
	var releaseInflight func()
	for i := 0; i < 18; i++ {
		if _, err = s.StartSSHConnection(target(i)); err != nil {
			t.Fatalf("channel %d after local removals: %v", i+1, err)
		}
		until := time.Now().Add(8 * time.Second)
		for s.SSHConnectionStatus(target(i)).State != "ready" && time.Now().Before(until) {
			time.Sleep(40 * time.Millisecond)
		}
		check(i)
		id := fmt.Sprintf("profile-%d", i)
		address := fmt.Sprintf("ssh://capacity-%d:%d", i, botPort)
		s.state.Profiles[id] = profile{Peer: workerpeer.Connection{Address: address, Peer: "retained-peer", Token: "fixture-proof"}, View: api.Machine{ID: id, Name: id, Address: address, ConnectionType: "bot"}}
		if i == 0 {
			s.state.Routes["original-task"] = id
			s.state.Runtimes["original-task"] = "codex"
			releaseInflight, err = s.holdPeerConnection(s.state.Profiles[id].Peer)
			if err != nil {
				t.Fatal(err)
			}
		}
		if i == 1 {
			shared := s.state.Profiles[id]
			shared.View.ID = "shared-active"
			s.state.Profiles["shared-active"] = shared
		}
		if r := s.ManageMachine(api.MachineMutation{ID: fmt.Sprintf("archive-%d", i), MachineID: id, Action: "remove"}); r.Status != "succeeded" {
			t.Fatal(r)
		}
	}
	check(0)
	check(1)
	releaseInflight()
	if s.SSHConnectionStatus(target(0)).State == "ready" {
		t.Fatal("idle deleted channel retained after final request")
	}
	for i := 0; i < 18; i++ {
		_, exists := s.state.Profiles[fmt.Sprintf("profile-%d", i)]
		if exists {
			t.Fatal("deleted profile retained", i)
		}
	}
	if s.state.Routes["original-task"] != "profile-0" || s.state.Runtimes["original-task"] != "codex" {
		t.Fatal("task route changed")
	}
	t.Log("18 real SSH Hosts connected/locally removed; inflight and shared channels usable; last-use release verified; deleted profiles and proofs absent")
}
