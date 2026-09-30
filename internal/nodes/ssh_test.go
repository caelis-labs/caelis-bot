package nodes

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSSHBootstrapKeepsCredentialsOffArgvAndUsesStrictHostKey(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "ssh-fixture")
	argsFile := filepath.Join(directory, "args")
	inputFile := filepath.Join(directory, "stdin")
	script := "#!/bin/sh\nfor arg do printf '%s\\n' \"$arg\"; done > " + sshQuote(argsFile) + "\ncat > " + sshQuote(inputFile) + "\nprintf '%s' '{\"Endpoint\":\"http://127.0.0.1:12345\",\"StoreID\":\"store\",\"InstanceID\":\"instance\",\"PrincipalID\":\"owner\",\"OS\":\"linux\",\"Arch\":\"arm64\"}'\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ssh, err := NewSSHWorker(SSHConfig{Target: "fixture-host", Helper: "/fixture/worker helper", Binary: binary})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ssh.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	input, _ := os.ReadFile(inputFile)
	if strings.Contains(string(input), "appCredential") {
		t.Fatal("read-only probe contained credentials")
	}
	secret := "SCOPED_FIXTURE_SECRET"
	if _, err = ssh.helper(context.Background(), caelis.WorkerBootstrapRequest{Action: "enroll", OperationID: "op", AppCredential: secret}); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsFile)
	if strings.Contains(string(args), secret) || !strings.Contains(string(args), "StrictHostKeyChecking=yes") || !strings.Contains(string(args), "ClearAllForwardings=yes") || !strings.Contains(string(args), "PermitLocalCommand=no") {
		t.Fatal("unsafe SSH arguments", string(args))
	}
	input, _ = os.ReadFile(inputFile)
	if !strings.Contains(string(input), secret) {
		t.Fatal("scoped enrollment not sent through stdin")
	}
	for _, endpoint := range []string{"http://10.0.0.1:9000", "http://localhost:9000", "https://127.0.0.1:9000", "http://127.0.0.1:9000/private"} {
		if tunnel, err := ssh.openTunnel(endpoint); err == nil {
			tunnel.Close()
			t.Fatal("non-loopback endpoint admitted", endpoint)
		}
	}
	for _, target := range []string{"-oProxyCommand=bad", "host;command", "host\nother"} {
		if _, err := NewSSHWorker(SSHConfig{Target: target}); err == nil {
			t.Fatal("SSH target injection admitted")
		}
	}
}

// The forwarding fixture uses a local nc stdio connection, never real SSH or a
// remote Host. Closing the owned tunnel must leave that shared Host available.
func TestSSHDirectTunnelDetachLeavesSharedHostAlive(t *testing.T) {
	nc := "/usr/bin/nc"
	if _, err := os.Stat(nc); err != nil {
		t.Skip("local stdio forwarding fixture requires nc")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("shared-host")) }))
	defer server.Close()
	binary := filepath.Join(t.TempDir(), "ssh-forward-fixture")
	script := "#!/bin/sh\nwhile [ $# -gt 0 ]; do\n if [ \"$1\" = '-W' ]; then shift; address=$1; fi\n shift\ndone\nexec /usr/bin/nc \"${address%:*}\" \"${address##*:}\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ssh, err := NewSSHWorker(SSHConfig{Target: "fixture", Binary: binary})
	if err != nil {
		t.Fatal(err)
	}
	tunnel, err := ssh.openTunnel(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(tunnel.Origin())
	if err != nil {
		tunnel.Close()
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(data) != "shared-host" {
		t.Fatal("forwarding changed bytes")
	}
	if err = tunnel.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Get(tunnel.Origin()); err == nil {
		t.Fatal("detached owned tunnel remained open")
	}
	response, err = client.Get(server.URL)
	if err != nil {
		t.Fatal("detach stopped shared Host", err)
	}
	response.Body.Close()
}
