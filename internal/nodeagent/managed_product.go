package nodeagent

import (
	"context"
	"errors"
	"net"
	"net/url"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// ManagedProductEndpoint is private native pairing metadata. It carries no
// credential bytes and is never a renderer DTO or permission to execute work.
// Readers independently confirm Lease with their pinned designated broker.
type ManagedProductEndpoint struct {
	BotID    string              `json:"botId"`
	Lease    nodeplane.Lease     `json:"lease"`
	Identity productrpc.Identity `json:"identity"`
	Endpoint string              `json:"endpoint"`
	AuthFile string              `json:"authFile"`
}
type ManagedDisableRequest struct {
	Target      api.WorkTarget  `json:"target"`
	Lease       nodeplane.Lease `json:"lease"`
	OperationID string          `json:"operationId"`
}
type ManagedProductPort interface {
	ReadManagedProduct(context.Context, api.WorkTarget) (ManagedProductEndpoint, error)
	ProxyManagedProduct(context.Context, ManagedProductRequest) (ManagedProductResponse, error)
	PrepareManagedDisable(context.Context, ManagedDisableRequest) (nodeplane.SnapshotRef, error)
	ReconcileManagedDisable(context.Context, ManagedDisableRequest) (ManagedDisableReceipt, error)
}

func validateManagedProduct(target api.WorkTarget, p ManagedProductEndpoint) error {
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || !net.ParseIP(u.Hostname()).IsLoopback() || !filepath.IsAbs(p.AuthFile) || filepath.Clean(p.AuthFile) != p.AuthFile || target.Role != api.RoleBot || p.Lease.BotID != p.BotID || p.Lease.NodeID != target.NodeID || string(p.Lease.Backend) != target.Backend || p.Lease.Epoch == "" || p.Lease.TTLMs <= 0 || p.Identity.NodeID != target.NodeID || p.Identity.BotID != productrpc.ProfileBotID(p.BotID) || p.Identity.Generation == "" || p.Identity.Version != productrpc.ProtocolVersion {
		return errors.New("managed product pairing unavailable")
	}
	return nil
}
func (s *Service) ReadManagedProduct(ctx context.Context, target api.WorkTarget) (ManagedProductEndpoint, error) {
	if target.Validate() != nil || target.Role != api.RoleBot || target.NodeID != s.options.NodeID || s.options.ManagedProduct == nil {
		return ManagedProductEndpoint{}, errors.New("managed product owner unavailable")
	}
	p, err := s.options.ManagedProduct.ReadManagedProduct(ctx, target)
	if err != nil {
		return ManagedProductEndpoint{}, err
	}
	if err = validateManagedProduct(target, p); err != nil {
		return ManagedProductEndpoint{}, err
	}
	return p, ctx.Err()
}
func (s *Service) PrepareManagedDisable(ctx context.Context, r ManagedDisableRequest) (nodeplane.SnapshotRef, error) {
	if r.Target.Validate() != nil || r.Target.Role != api.RoleBot || r.Target.NodeID != s.options.NodeID || !identifier.MatchString(r.OperationID) || r.Lease.BotID == "" || r.Lease.NodeID == "" || r.Lease.Epoch == "" || s.options.ManagedProduct == nil {
		return nodeplane.SnapshotRef{}, errors.New("managed product control unavailable")
	}
	return s.options.ManagedProduct.PrepareManagedDisable(ctx, r)
}
func (c *Client) ReadManagedProduct(ctx context.Context, target api.WorkTarget) (ManagedProductEndpoint, error) {
	var p ManagedProductEndpoint
	if target.NodeID != c.expected {
		return p, errors.New("managed product node mismatch")
	}
	if err := c.request(ctx, "POST", "/v1/node/managed-product", target, &p); err != nil {
		return p, err
	}
	return p, validateManagedProduct(target, p)
}
func (c *Client) PrepareManagedDisable(ctx context.Context, r ManagedDisableRequest) (nodeplane.SnapshotRef, error) {
	var ref nodeplane.SnapshotRef
	if r.Target.NodeID != c.expected || !identifier.MatchString(r.OperationID) {
		return ref, errors.New("managed control node mismatch")
	}
	err := c.request(ctx, "POST", "/v1/node/managed-disable", r, &ref)
	if err == nil && (ref.BotID != r.Lease.BotID || ref.Digest == "") {
		err = errors.New("managed control snapshot mismatch")
	}
	return ref, err
}

func (s *Service) ReconcileManagedDisable(ctx context.Context, r ManagedDisableRequest) (ManagedDisableReceipt, error) {
	if r.Target.NodeID != s.options.NodeID || s.options.ManagedProduct == nil {
		return ManagedDisableReceipt{}, errors.New("managed receipt owner unavailable")
	}
	return s.options.ManagedProduct.ReconcileManagedDisable(ctx, r)
}
func (c *Client) ReconcileManagedDisable(ctx context.Context, r ManagedDisableRequest) (ManagedDisableReceipt, error) {
	var receipt ManagedDisableReceipt
	if r.Target.NodeID != c.expected {
		return receipt, errors.New("managed receipt node mismatch")
	}
	err := c.request(ctx, "POST", "/v1/node/managed-disable-receipt", r, &receipt)
	if err == nil && receipt.Request != canonicalDisableRequest(r) {
		err = errors.New("managed original receipt mismatch")
	}
	return receipt, err
}
