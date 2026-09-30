package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBrokerConfigurationRejectsForeignOrAmbiguousPeers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "peers.json")
	valid := brokerPeers{Version: 1, BotID: "bot", Peers: []brokerPeer{{NodeID: "node-1", Backend: "codex", Socket: "/absolute/private/agent.sock"}}}
	for _, change := range []func(*brokerPeers){func(c *brokerPeers) { c.BotID = "foreign" }, func(c *brokerPeers) { c.Version = 2 }, func(c *brokerPeers) { c.Peers = append(c.Peers, c.Peers[0]) }, func(c *brokerPeers) { c.Peers[0].Socket = "relative.sock" }, func(c *brokerPeers) { c.Peers[0].Backend = "unsupported" }} {
		c := valid
		c.Peers = append([]brokerPeer(nil), valid.Peers...)
		change(&c)
		b, _ := json.Marshal(c)
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := loadBrokerPeers(path, "bot"); e == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	b, _ := json.Marshal(valid)
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := loadBrokerPeers(path, "bot"); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := loadBrokerPeers(path, "bot"); e == nil {
		t.Fatal("public pairing file accepted")
	}
}
func TestBrokerInvalidArgsDoNotInitializeOwner(t *testing.T) {
	for _, args := range [][]string{{"serve-broker"}, {"serve-broker", "--profile", "relative", "--bot-id", "bot", "--socket", "/absolute/private/socket"}, {"serve-broker", "--help"}} {
		e := runBroker(context.Background(), args, io.Discard)
		if len(args) > 2 && e == nil {
			t.Fatal("invalid profile accepted")
		}
	}
}
