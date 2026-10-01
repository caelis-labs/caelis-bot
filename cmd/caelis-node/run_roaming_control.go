package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodebroker"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/roaming"
)

type roamingManagedControl struct {
	token       string
	productHTTP *http.Client
	mu          sync.Mutex // serializes all candidate claims with disable
	holder      *roamingProofOwner
	broker      *nodebroker.Client
	runner      *roaming.Runner
	descriptor  atomic.Pointer[nodeagent.ManagedProductEndpoint]
	disabled    atomic.Bool
	journal     *nodeagent.ManagedDisableJournal
}

func sameManagedLease(a, b nodeplane.Lease) bool {
	return a.BotID == b.BotID && a.NodeID == b.NodeID && a.Backend == b.Backend && a.Epoch == b.Epoch
}
func (m *roamingManagedControl) ReadManagedProduct(ctx context.Context, target api.WorkTarget) (nodeagent.ManagedProductEndpoint, error) {
	p := m.descriptor.Load()
	if target != m.holder.target || p == nil || m.disabled.Load() {
		return nodeagent.ManagedProductEndpoint{}, nodecoord.ErrIneligible
	}
	lease, err := m.broker.CurrentLease(ctx, m.holder.botID)
	if err != nil || !sameManagedLease(lease, p.Lease) {
		return nodeagent.ManagedProductEndpoint{}, nodecoord.ErrIneligible
	}
	proof, err := m.holder.ReadRuntimeProof(ctx, target)
	if err != nil || proof.LeaseEpoch != lease.Epoch {
		return nodeagent.ManagedProductEndpoint{}, nodecoord.ErrIneligible
	}
	result := *p
	result.Lease = lease
	return result, nil
}
func (m *roamingManagedControl) PrepareManagedDisable(ctx context.Context, r nodeagent.ManagedDisableRequest) (nodeplane.SnapshotRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Target != m.holder.target || r.Lease.BotID != m.holder.botID || r.OperationID == "" || m.runner == nil {
		return nodeplane.SnapshotRef{}, nodecoord.ErrIneligible
	}
	if m.journal == nil {
		return nodeplane.SnapshotRef{}, nodecoord.ErrIneligible
	}
	receipt, e := m.journal.Lookup(ctx, r)
	if e != nil {
		return nodeplane.SnapshotRef{}, e
	}
	if m.disabled.Load() {
		if receipt.Outcome == "accepted" {
			return receipt.Snapshot, nil
		}
		return nodeplane.SnapshotRef{}, errors.New("original disable outcome remains unknown")
	}
	if m.disabled.Load() {
		return nodeplane.SnapshotRef{}, nodecoord.ErrConflict
	}
	lease, err := m.broker.CurrentLease(ctx, m.holder.botID)
	if err != nil || !sameManagedLease(lease, r.Lease) {
		return nodeplane.SnapshotRef{}, nodecoord.ErrConflict
	}
	if lease.NodeID == r.Target.NodeID {
		proof, err := m.holder.ReadRuntimeProof(ctx, r.Target)
		if err != nil || !proof.SafeIdle || proof.Pending || proof.Unknown || proof.LeaseEpoch != lease.Epoch {
			return nodeplane.SnapshotRef{}, nodecoord.ErrIneligible
		}
	}
	if err = m.journal.Begin(ctx, r); err != nil {
		return nodeplane.SnapshotRef{}, err
	}
	// No further candidate claim may race the owner's final publication/release.
	m.disabled.Store(true)
	var ref nodeplane.SnapshotRef
	if lease.NodeID == r.Target.NodeID {
		err = m.runner.Publish(ctx)
		if err == nil {
			ref, err = m.broker.LatestSnapshot(ctx, m.holder.botID)
		}
		if err == nil {
			err = m.runner.Stop(ctx)
		}
	} else {
		ref, err = m.broker.LatestSnapshot(ctx, m.holder.botID)
	}
	outcome := "accepted"
	if err != nil {
		outcome = "unknown"
	}
	if journalErr := m.journal.Finish(ctx, r, ref, outcome); journalErr != nil {
		return nodeplane.SnapshotRef{}, errors.Join(err, journalErr)
	}
	return ref, err
}

func (m *roamingManagedControl) ProxyManagedProduct(ctx context.Context, r nodeagent.ManagedProductRequest) (nodeagent.ManagedProductResponse, error) {
	if !nodeagent.ValidManagedProductRequest(r) {
		return nodeagent.ManagedProductResponse{}, nodecoord.ErrIneligible
	}
	p, err := m.ReadManagedProduct(ctx, r.Target)
	if err != nil || p.Identity != r.Identity {
		return nodeagent.ManagedProductResponse{}, nodecoord.ErrConflict
	}
	path := r.Path
	if r.ResourceID != "" {
		path += "?id=" + url.QueryEscape(r.ResourceID)
	}
	request, err := http.NewRequestWithContext(ctx, r.Method, p.Endpoint+path, bytes.NewReader(r.Body))
	if err != nil {
		return nodeagent.ManagedProductResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if r.Path == "/v1/resources" {
		request.Header.Set("X-Product-Bot", p.Identity.BotID)
		request.Header.Set("X-Product-Generation", p.Identity.Generation)
	}
	if r.Method == "PUT" {
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("X-Resource-Name", r.ResourceName)
		request.Header.Set("X-Resource-Size", r.ResourceSize)
		request.Header.Set("X-Resource-SHA256", r.ResourceSHA256)
	}
	request.Header.Set("Authorization", "Bearer "+m.token)
	response, err := m.productHTTP.Do(request)
	if err != nil {
		return nodeagent.ManagedProductResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, nodeagent.MaxManagedProductBody+1))
	if err != nil || len(body) > nodeagent.MaxManagedProductBody {
		return nodeagent.ManagedProductResponse{}, errors.New("managed product response exceeds private frame limit")
	}
	kind := nodeagent.CleanManagedProductContentType(response.Header.Get("Content-Type"))
	if kind == "" {
		return nodeagent.ManagedProductResponse{}, errors.New("managed product response unavailable")
	}
	return nodeagent.ManagedProductResponse{Status: response.StatusCode, ContentType: kind, Body: body, ResourceName: response.Header.Get("X-Resource-Name"), ResourceSize: response.Header.Get("X-Resource-Size"), ResourceSHA256: response.Header.Get("X-Resource-SHA256")}, nil
}

func (m *roamingManagedControl) ReconcileManagedDisable(ctx context.Context, r nodeagent.ManagedDisableRequest) (nodeagent.ManagedDisableReceipt, error) {
	if r.Target != m.holder.target || m.journal == nil {
		return nodeagent.ManagedDisableReceipt{}, nodecoord.ErrIneligible
	}
	return m.journal.Lookup(ctx, r)
}
