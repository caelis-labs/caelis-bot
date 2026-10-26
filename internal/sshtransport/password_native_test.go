//go:build !windows

package sshtransport

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestNativeOpenSSHPasswordAgainstIsolatedServer(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_SSH_AUTH_FIXTURE")
	if binary == "" {
		t.Skip("opt-in isolated SSH server fixture")
	}
	command := exec.CommandContext(t.Context(), binary)
	out, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	var ports struct {
		SSHPort int `json:"sshPort"`
		BotPort int `json:"botPort"`
	}
	if err = json.NewDecoder(out).Decode(&ports); err != nil {
		t.Fatal(err)
	}
	v := fixtureTransport(t)
	target := api.SSHTarget{Host: "127.0.0.1", SSHPort: ports.SSHPort, Port: ports.BotPort, User: "fixture", Authentication: "password"}
	if _, err = v.Start(target); err != nil {
		t.Fatal(err)
	}
	passwordPrompts := 0
	until := time.Now().Add(12 * time.Second)
	for time.Now().Before(until) {
		status := v.Status(target)
		if status.Prompt != nil {
			value := "yes"
			if !status.Prompt.Confirm {
				passwordPrompts++
				value = "fixture-password"
			}
			if err = v.Answer(api.SSHAnswer{Target: &target, ID: status.Prompt.ID, Value: value}); err != nil {
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
	if v.Status(target).State != "ready" || passwordPrompts != 1 {
		t.Fatalf("password authentication did not complete: %#v, prompts=%d", v.Status(target), passwordPrompts)
	}
	address, err := Address(target)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := v.Resolve(t.Context(), address)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "isolated-password-bot" {
		t.Fatal("forward did not reach fixture Bot")
	}
	public, _ := json.Marshal(v.Status(target))
	if strings.Contains(string(public), "fixture-password") || strings.Contains(address, "fixture-password") {
		t.Fatal("password leaked to public state")
	}
}
