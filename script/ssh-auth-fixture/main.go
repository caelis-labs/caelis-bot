// Isolated SSH password fixture. Credentials are fixed test sentinels; no OS
// account, production key, configuration or remote service is changed.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"

	"golang.org/x/crypto/ssh"
)

func main() {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		panic(err)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if meta.User() != "fixture" || string(password) != "fixture-password" {
			return nil, fmt.Errorf("fixture authentication rejected")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "isolated-password-bot") }))
	defer bot.Close()
	_, portText, _ := net.SplitHostPort(bot.Listener.Addr().String())
	botPort, _ := strconv.Atoi(portText)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	json.NewEncoder(os.Stdout).Encode(map[string]any{"sshPort": listener.Addr().(*net.TCPAddr).Port, "botPort": botPort})
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go serve(connection, config, botPort)
	}
}
func serve(connection net.Conn, config *ssh.ServerConfig, botPort int) {
	defer connection.Close()
	server, channels, requests, err := ssh.NewServerConn(connection, config)
	if err != nil {
		return
	}
	defer server.Close()
	go ssh.DiscardRequests(requests)
	for next := range channels {
		if next.ChannelType() != "direct-tcpip" {
			next.Reject(ssh.Prohibited, "fixture forwards only")
			continue
		}
		var destination struct {
			Host       string
			Port       uint32
			Origin     string
			OriginPort uint32
		}
		if ssh.Unmarshal(next.ExtraData(), &destination) != nil || destination.Host != "127.0.0.1" || int(destination.Port) != botPort {
			next.Reject(ssh.Prohibited, "fixture destination only")
			continue
		}
		upstream, err := net.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(botPort)))
		if err != nil {
			next.Reject(ssh.ConnectionFailed, "fixture unavailable")
			continue
		}
		channel, request, err := next.Accept()
		if err != nil {
			upstream.Close()
			continue
		}
		go ssh.DiscardRequests(request)
		go func() {
			defer channel.Close()
			defer upstream.Close()
			done := make(chan struct{})
			go func() { io.Copy(upstream, channel); close(done) }()
			io.Copy(channel, upstream)
			channel.Close()
			upstream.Close()
			<-done
		}()
	}
}
