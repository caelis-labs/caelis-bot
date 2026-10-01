package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// Outgoing products are observed through the existing paired agent route. Their
// listener and bearer stay on the owning node, never on the SSH coordinator.
func outgoingRoamingProductFactory(parent context.Context, node roamingNativeNode, lease nodeplane.Lease, endpoint nodeagent.ManagedProductEndpoint, pairing backend.ProductPairing) productClientFactory {
	return func(requested backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		if parent == nil || requested != pairing || validateProductPairing(requested) != nil || node.Registration.Join != api.NodeOutgoing || lease.Validate() != nil || endpoint.Identity.NodeID != node.Registration.ID || endpoint.Identity.BotID != pairing.BotID || !sameRoamingLease(endpoint.Lease, lease) {
			return nil, nil, errors.New("exact outgoing product observer required")
		}
		life, cancel := context.WithCancel(parent)
		peer, err := dialRoamingNode(life, node)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		detach := &outgoingProductObserver{peer: peer, cancel: cancel}
		target := api.WorkTarget{NodeID: lease.NodeID, Backend: string(lease.Backend), Role: api.RoleBot}
		check, stop := context.WithTimeout(life, 15*time.Second)
		current, err := peer.ReadManagedProduct(check, target)
		stop()
		if err != nil || current.Lease.Validate() != nil || !sameRoamingLease(current.Lease, lease) || current.Identity != endpoint.Identity || current.Endpoint != endpoint.Endpoint || current.AuthFile != endpoint.AuthFile {
			_ = detach.Close()
			return nil, nil, errors.New("outgoing product generation changed")
		}
		transport, err := nodeagent.ManagedProductTransport(peer, target, endpoint.Identity)
		if err != nil {
			_ = detach.Close()
			return nil, nil, err
		}
		client, err := productrpc.NewClient(productrpc.ClientOptions{URL: "http://127.0.0.1:1", ExpectedNode: target.NodeID, ExpectedBot: endpoint.Identity.BotID, Token: nodeagent.ProductProxyBearer, HTTP: &http.Client{Transport: transport}})
		if err != nil {
			_ = detach.Close()
			return nil, nil, err
		}
		return client, detach, nil
	}
}

// This dedicated connection owns observation only. It never closes the shared
// management peer or sends a stop/disable command to the independent owner.
type outgoingProductObserver struct {
	peer   *nodeagent.Client
	cancel context.CancelFunc
	once   sync.Once
}

func (o *outgoingProductObserver) Close() error {
	o.once.Do(func() { o.cancel(); _ = o.peer.Close() })
	return nil
}
