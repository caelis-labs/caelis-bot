//go:build !windows

package sshtransport

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/testsupport/sshd"
)

func TestNativeOpenSSHReconnectBeyondBudgetAndSixCycles(t *testing.T) {
	server := sshd.New(t)
	v := fixtureTransport(t)
	put(t, v.path(), server.Config("recovery-fixture")+server.Config("independent-fixture"))
	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "fixture Bot") }))
	defer bot.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(bot.URL, "http://"))
	address := "ssh://recovery-fixture:" + port
	check := func(address string) {
		t.Helper()
		a, err := v.Resolve(t.Context(), address)
		if err != nil {
			t.Fatal(err)
		}
		r, err := http.Get(a)
		if err != nil {
			t.Fatal("native forwarded HTTP", err)
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		if err != nil || r.StatusCode != 200 || string(b) != "fixture Bot" {
			t.Fatal("forward did not reach original fixture")
		}
	}
	check(address)
	other := "ssh://independent-fixture:" + port
	check(other)
	// Stop only the fixture listener and this owned client. The established
	// independent channel must continue serving HTTP during recovery.
	server.Stop()
	disconnectChannel(t, v, address)
	for i := range 5 {
		advanceRetry(v)
		if _, err := v.Resolve(t.Context(), address); err == nil {
			t.Fatal("unavailable sshd unexpectedly connected", i)
		}
		waitChannel(t, v, address, "failed")
		check(other)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	advanceRetry(v)
	concurrentResolve(t, v, address)
	check(address)
	for i := range 6 {
		disconnectChannel(t, v, address)
		advanceRetry(v)
		concurrentResolve(t, v, address)
		check(address)
		check(other)
		t.Log("native disconnect/recovery cycle", i+1, "HTTP 200; original independent channel remained usable")
	}
	target, _ := Target(address)
	if err := v.Stop(target); err != nil {
		t.Fatal(err)
	}
	advanceRetry(v)
	if _, err := v.Resolve(t.Context(), address); err == nil {
		t.Fatal("stopped native channel revived")
	}
	check(other)
	botPort, _ := strconv.Atoi(port)
	t.Log(fmt.Sprintf("isolated OpenSSH %d -> Bot %d; 5 failed reconnects, 6 cycles, 24 concurrent readers per recovery, explicit stop fenced", server.Port, botPort))
}

func TestNativeOpenSSHReleaseThenImmediateStartNeverOverlaps(t *testing.T) {
	server := sshd.New(t)
	v := fixtureTransport(t)
	put(t, v.path(), server.Config("exit-barrier-fixture")+server.Config("independent-barrier-fixture"))
	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "fixture Bot") }))
	defer bot.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(bot.URL, "http://"))
	address := "ssh://exit-barrier-fixture:" + port
	if _, err := v.Resolve(t.Context(), address); err != nil {
		t.Fatal(err)
	}
	exerciseExitBarrier(t, v, address, "release")
	a, err := v.Resolve(t.Context(), address)
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.Get(a)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil || r.StatusCode != 200 || string(b) != "fixture Bot" {
		t.Fatal("successor did not reach the same isolated Bot")
	}
	t.Log("real OpenSSH cleanup barrier and successor forward: HTTP 200")
}
