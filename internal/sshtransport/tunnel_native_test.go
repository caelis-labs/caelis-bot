//go:build !windows

package sshtransport

import (
	"context"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
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

func TestMain(m *testing.M) {
	if os.Getenv("CAELIS_SSH_ASKPASS") != "" {
		_, e := RunAskpass(os.Args)
		if e != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestNativeOpenSSHTunnelExplicitHostTrustAndOneOwnedForward(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_SSHD") != "1" {
		t.Skip("opt-in isolated system sshd acceptance")
	}
	daemon := "/usr/sbin/sshd"
	if _, e := os.Stat(daemon); e != nil {
		t.Skip("system sshd not installed")
	}
	v := fixtureTransport(t)
	root := filepath.Dir(v.root)
	key := filepath.Join(root, "identity")
	hostkey := filepath.Join(root, "hostkey")
	for _, p := range []string{key, hostkey} {
		if b, e := exec.Command("/usr/bin/ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", p).CombinedOutput(); e != nil {
			t.Fatalf("generate fixture key: %v %s", e, b)
		}
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	sshport := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	user := os.Getenv("USER")
	config := filepath.Join(root, "sshd_config")
	put(t, config, fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s\nPidFile %s\nAuthorizedKeysFile %s\nStrictModes no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nAllowUsers %s\n", sshport, quote(hostkey), quote(filepath.Join(root, "pid")), quote(key+".pub"), user))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, daemon, "-D", "-e", "-f", config)
	out := &strings.Builder{}
	cmd.Stderr = out
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { cancel(); <-done }()
	time.Sleep(150 * time.Millisecond)
	select {
	case err := <-done:
		done <- err
		t.Fatalf("isolated sshd unavailable: %v %s", err, out.String())
	default:
	}
	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "isolated Bot") }))
	defer bot.Close()
	_, p, _ := net.SplitHostPort(strings.TrimPrefix(bot.URL, "http://"))
	var botport int
	fmt.Sscan(p, &botport)
	put(t, v.path(), fmt.Sprintf("Host fixture\n ProxyJump fixture-jump\n HostName 127.0.0.1\n Port %d\n User %s\n IdentityFile %s\n IdentitiesOnly yes\n UserKnownHostsFile %s\n LocalForward 127.0.0.1:1 127.0.0.1:1\n", sshport, user, quote(key), quote(filepath.Join(root, "known_hosts")))+fmt.Sprintf("Host fixture-jump\n HostName 127.0.0.1\n Port %d\n User %s\n IdentityFile %s\n IdentitiesOnly yes\n UserKnownHostsFile %s\n", sshport, user, quote(key), quote(filepath.Join(root, "known_hosts"))))
	target := api.SSHTarget{Host: "fixture", Port: botport}
	if _, e = v.Start(target); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(12 * time.Second)
	trusted := false
	for time.Now().Before(deadline) {
		s := v.Status(target)
		if s.Prompt != nil {
			if !s.Prompt.Confirm || !strings.Contains(s.Prompt.Text, "SHA256:") {
				t.Fatalf("expected actual native fingerprint, got %#v", s.Prompt)
			}
			trusted = true
			if e = v.Answer(api.SSHAnswer{Host: target.Host, Port: target.Port, ID: s.Prompt.ID, Value: "yes"}); e != nil {
				t.Fatal(e)
			}
		}
		if s.State == "ready" {
			break
		}
		if s.State == "failed" {
			t.Fatal(s)
		}
		time.Sleep(40 * time.Millisecond)
	}
	if !trusted || v.Status(target).State != "ready" {
		t.Fatalf("not ready: %#v", v.Status(target))
	}
	a, e := v.Resolve(t.Context(), "ssh://fixture:"+p)
	if e != nil {
		t.Fatal(e)
	}
	res, e := http.Get(a)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	v.Stop(target)
	deadline = time.Now().Add(3 * time.Second)
	for v.Status(target).State != "failed" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if _, e = v.Resolve(t.Context(), "ssh://fixture:"+p); e == nil {
		t.Fatal("stopped tunnel auto-reconnected")
	}
}
