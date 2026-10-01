package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/memorytransfer"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodebroker"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type brokerPeer struct {
	NodeID  string          `json:"nodeId"`
	Backend api.NodeBackend `json:"backend"`
	Socket  string          `json:"socket"`
}
type brokerPeers struct {
	Version int          `json:"version"`
	BotID   string       `json:"botId"`
	Peers   []brokerPeer `json:"peers"`
}

// pairedRuntimePeer reconnects only the inspected, configured native endpoint.
// It cannot discover nodes, bootstrap accounts, start a runtime or replay work.
type pairedRuntimePeer struct{ pair brokerPeer }

func (p pairedRuntimePeer) ReadRuntimeProof(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	if target.NodeID != p.pair.NodeID || target.Backend != string(p.pair.Backend) || target.Role != api.RoleBot {
		return nodeplane.RuntimeEligibility{}, nodecoord.ErrIneligible
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, err := nodeagent.Dial(bounded, p.pair.Socket, p.pair.NodeID)
	if err != nil {
		return nodeplane.RuntimeEligibility{}, err
	}
	defer client.Close()
	return client.ReadRuntimeProof(bounded, target)
}
func readBrokerFile(path string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("explicit absolute broker file required")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > limit {
		return nil, errors.New("broker file must be bounded private regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("broker file changed")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("broker file exceeds limit")
	}
	return b, nil
}
func loadBrokerPeers(path, botID string) (*nodecoord.PeerRegistry, error) {
	registry := nodecoord.NewPeerRegistry()
	if path == "" {
		return registry, nil
	}
	b, err := readBrokerFile(path, 64<<10)
	if err != nil {
		return nil, err
	}
	var config brokerPeers
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&config) != nil || dec.Decode(&struct{}{}) != io.EOF || config.Version != 1 || config.BotID != botID || len(config.Peers) > 32 {
		return nil, errors.New("invalid exact broker peer configuration")
	}
	for _, pair := range config.Peers {
		if !filepath.IsAbs(pair.Socket) || filepath.Clean(pair.Socket) != pair.Socket {
			return nil, errors.New("broker peer requires exact target-local private socket")
		}
		if err = registry.Register(api.WorkTarget{NodeID: pair.NodeID, Backend: string(pair.Backend), Role: api.RoleBot}, pairedRuntimePeer{pair}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// runBroker owns a foreground single broker and cold Notebook cache. No flag
// installs a service, changes SSH configuration, or grants an unmanaged Host.
func runBroker(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "serve-broker" {
		return errors.New("usage: caelis-node serve-broker --profile ABS --bot-id ID --socket ABS [--peers-file PRIVATE]")
	}
	f := flag.NewFlagSet("serve-broker", flag.ContinueOnError)
	f.SetOutput(out)
	profile := f.String("profile", "", "explicit isolated private broker state directory")
	botID := f.String("bot-id", "", "stable Bot identity in Notebook snapshot, not product transport hash")
	socket := f.String("socket", "", "explicit same-user private Unix socket")
	peers := f.String("peers-file", "", "optional private versioned exact node/backend/socket pairing file")
	seedSource := f.String("seed-source", "", "exact paired NODE/BACKEND source attesting stopped initial export")
	seed := f.String("seed-snapshot", "", "optional private complete initial Notebook payload (epoch 0, first initialization only)")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 || *botID == "" || !filepath.IsAbs(*profile) || !filepath.IsAbs(*socket) {
		return errors.New("explicit absolute broker profile/socket and stable Bot identity required")
	}
	registry, err := loadBrokerPeers(*peers, *botID)
	if err != nil {
		return err
	}
	owner, err := nodecoord.Open(nodecoord.Options{Directory: *profile, BotID: *botID, Verify: registry.VerifyClaim, VerifyRenew: registry.VerifyRenew, ValidateSnapshot: memorytransfer.ValidateNotebookPayload})
	if err != nil {
		return err
	}
	defer owner.Close()
	if *seed != "" {
		parts := strings.Split(*seedSource, "/")
		if len(parts) != 2 || parts[0] == "" {
			return errors.New("initial snapshot requires exact paired --seed-source NODE/BACKEND")
		}
		b, err := readBrokerFile(*seed, memorytransfer.MaxNotebookPayload)
		if err != nil {
			return err
		}
		ref, err := memorytransfer.ValidateNotebookPayload(ctx, b)
		if err != nil {
			return err
		}
		if err = registry.VerifyBootstrap(ctx, api.WorkTarget{NodeID: parts[0], Backend: parts[1], Role: api.RoleBot}, ref); err != nil {
			return err
		}
		if err = owner.SeedSnapshot(ctx, ref, b); err != nil {
			return err
		}
	}
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var readyErr error
	err = nodebroker.ServeUnix(serveCtx, *socket, owner, func() {
		readyErr = json.NewEncoder(out).Encode(struct {
			Socket string `json:"socket"`
			BotID  string `json:"botId"`
			Mode   string `json:"mode"`
		}{*socket, *botID, "single-broker"})
		if readyErr != nil {
			cancel()
		}
	})
	if readyErr != nil {
		return readyErr
	}
	return err
}
