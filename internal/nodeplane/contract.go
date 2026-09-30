// Package nodeplane defines host-only boundaries for optional node catalog and
// single-user Bot roaming. It has no transport, daemon, lifecycle or UI owner.
package nodeplane

import (
	"context"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const (
	DefaultHeartbeatInterval = 10 * time.Second
	DefaultLeaseExpiry       = 60 * time.Second
	DefaultSnapshotInterval  = 60 * time.Second
)

// Lease is execution authority, independent of the management view. Epoch is
// a broker-issued opaque fence; it is never a native runtime controller epoch.
type Lease struct {
	BotID     string
	NodeID    string
	Backend   api.NodeBackend
	Epoch     string
	ExpiresAt time.Time
	// TTLMs is remaining lifetime (at most 60000), measured conservatively
	// from local request start. ExpiresAt is informational wall-clock metadata.
	TTLMs int64
}

// SnapshotRef binds a full Notebook snapshot to one stable Bot and publisher.
// Version is a canonical positive decimal, monotonically increasing within the
// Bot (including across epochs); Digest is lowercase hex SHA-256 of the bundle.
type SnapshotRef struct {
	BotID   string
	Epoch   string
	Version string
	Digest  string
}

// RuntimeProof is attested by the native owner after installing an admission
// fence. Controllable must cover autonomous/background admission, existing work
// and loss of broker connectivity; a UI-only input gate is insufficient.
type RuntimeProof struct {
	NodeID       string
	Backend      api.NodeBackend
	Epoch        string
	Controllable bool
}

type ClaimRequest struct {
	BotID         string
	Target        api.WorkTarget
	ExpectedEpoch string
	Snapshot      SnapshotRef
	Proof         RuntimeProof
}

// CatalogAgent supplies host-discovered metadata and only typed configuration
// mutations. Credentials/native paths stay in its local implementation.
type CatalogAgent interface {
	Catalog(context.Context) (api.NodeCatalog, error)
	Configuration(context.Context, string, api.NodeBackend) (api.NodeRuntimeConfiguration, error)
	Manage(context.Context, ManagementRequest) (api.NodeOperationReceipt, error)
	Reconcile(context.Context, api.NodeOperationRef) (api.NodeOperationReceipt, error)
}

// ManagementRequest is deliberately not a generic RPC. The configuration
// payload uses the existing semantic DTO; Ref identifies the persisted intent.
type ManagementRequest = api.NodeManagementRequest

// Coordinator performs exact-epoch compare-and-swap. Claim requires safe idle
// and the last complete snapshot. Expiry closes admission before any reclaim;
// stable local reclaim is explicit, never triggered by settings selection.
type Coordinator interface {
	Claim(context.Context, ClaimRequest) (Lease, error)
	Heartbeat(context.Context, Lease) (Lease, error)
	Release(context.Context, Lease) error
}

// NotebookPublisher accepts only the active leased publisher. Install stages,
// verifies checksum and atomically replaces the complete Markdown tree,
// including deletions. Authentication, runtime history and machine paths are
// excluded; retain the prior complete cold bundle on failed installation.
type NotebookPublisher interface {
	Publish(context.Context, Lease) (SnapshotRef, error)
	Install(context.Context, Lease, SnapshotRef) error
}

// SnapshotPublisher stores only a verified full bundle from the current
// publisher; a broker verifies both the reference and the lease atomically.
type SnapshotPublisher interface {
	PublishSnapshot(context.Context, Lease, SnapshotRef, []byte) error
}

type SnapshotReader interface {
	LatestSnapshot(context.Context, string) (SnapshotRef, error)
	ReadSnapshot(context.Context, SnapshotRef) ([]byte, error)
}

// AdmissionFence is implemented by the concrete runtime owner. Check happens
// at actual native admission, not when rendering capability or queuing a request.
// Quiesce closes autonomous admission and proves safe idle, or fails closed.
type AdmissionFence interface {
	Proof(context.Context, Lease) (RuntimeProof, error)
	Check(context.Context, Lease) error
	Quiesce(context.Context, Lease) error
}
