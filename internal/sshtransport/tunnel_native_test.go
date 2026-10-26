//go:build !windows

package sshtransport

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// Manual key login uses an isolated generated identity and never changes the
	// user's config, agent, account password or known-hosts file.
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("manual-key-encrypted-%t", encrypted), func(t *testing.T) {
			identityPath := key
			if encrypted {
				identityPath = filepath.Join(root, "encrypted-identity")
				if b, err := exec.Command("/usr/bin/ssh-keygen", "-q", "-t", "ed25519", "-N", "fixture-passphrase", "-f", identityPath).CombinedOutput(); err != nil {
					t.Fatalf("fixture key: %v %s", err, b)
				}
				pub, err := os.ReadFile(identityPath + ".pub")
				if err != nil {
					t.Fatal(err)
				}
				f, err := os.OpenFile(key+".pub", os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.Write(pub)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			identity, err := v.RegisterIdentity(identityPath)
			if err != nil {
				t.Fatal(err)
			}
			manual := api.SSHTarget{Host: "127.0.0.1", SSHPort: sshport, User: user, Authentication: "key", Identity: identity.ID, Port: botport}
			if _, err = v.Start(manual); err != nil {
				t.Fatal(err)
			}
			defer v.Stop(manual)
			answeredPassphrase := false
			until := time.Now().Add(12 * time.Second)
			for time.Now().Before(until) {
				status := v.Status(manual)
				if status.Prompt != nil {
					answer := "yes"
					if !status.Prompt.Confirm {
						answer = "fixture-passphrase"
						answeredPassphrase = true
					}
					if err = v.Answer(api.SSHAnswer{Target: &manual, ID: status.Prompt.ID, Value: answer}); err != nil {
						t.Fatal(err)
					}
				}
				if status.State == "ready" {
					break
				}
				if status.State == "failed" {
					t.Fatal(status)
				}
				time.Sleep(40 * time.Millisecond)
			}
			if v.Status(manual).State != "ready" || encrypted && !answeredPassphrase {
				t.Fatalf("manual key did not finish native authentication: %#v", v.Status(manual))
			}
			uri, err := Address(manual)
			if err != nil {
				t.Fatal(err)
			}
			endpoint, err := v.Resolve(t.Context(), uri)
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.Get(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal(response.StatusCode)
			}
			if !strings.Contains(uri, "sshPort="+strconv.Itoa(sshport)) || !strings.Contains(uri, ":"+p) {
				t.Fatal("ports lost")
			}
			manifest, err := os.ReadFile(filepath.Join(v.root, "identities.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(manifest), "fixture-passphrase") {
				t.Fatal("secret persisted")
			}
		})
	}

}

// Optional read-only check using an existing, already trusted user Host. It never
// accepts a new fingerprint, supplies a credential, pairs, or creates work.
func TestExistingHostReadonlyBotIdentity(t *testing.T) {
	host := os.Getenv("CAELIS_BOT_READONLY_SSH_HOST")
	if host == "" {
		t.Skip("explicit external read-only Host acceptance")
	}
	v := New(t.TempDir())
	defer v.Close()
	target := api.SSHTarget{Host: host, Port: 9317}
	if _, err := v.Start(target); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(15 * time.Second)
	for time.Now().Before(until) {
		status := v.Status(target)
		if status.Prompt != nil {
			t.Fatal("existing Host requested new trust or credentials; not answered")
		}
		if status.State == "ready" {
			break
		}
		if status.State == "failed" {
			t.Fatal(status.Issue)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if v.Status(target).State != "ready" {
		t.Fatal("existing Host tunnel not ready")
	}
	address, err := Address(target)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := v.Resolve(t.Context(), address)
	if err != nil {
		t.Fatal(err)
	}
	client := http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(endpoint + "/worker/v1/identity")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Bot identity GET status %d", response.StatusCode)
	}
	var identity struct {
		Peer     string `json:"peer"`
		Contract string `json:"contract"`
	}
	if err = json.NewDecoder(response.Body).Decode(&identity); err != nil || identity.Peer == "" {
		t.Fatal("Bot identity was not readable", err)
	}
	t.Log("Existing user Host authenticated through system OpenSSH; remote Bot identity GET returned 200. No pairing or Worker write.")
}
