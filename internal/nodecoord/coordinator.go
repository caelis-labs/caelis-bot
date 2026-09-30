// Package nodecoord implements a single, explicitly configured broker. It owns
// lease CAS and a complete cold Notebook cache, never native execution.
package nodecoord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

var (
	ErrUnavailable = errors.New("broker unavailable or clock uncertain")
	ErrConflict    = errors.New("lease epoch or owner conflict")
	ErrIneligible  = errors.New("runtime ownership proof unavailable")
	ErrSnapshot    = errors.New("latest complete Notebook snapshot required")
)

const MaxSnapshotBytes = 16 << 20

// Verify must consult a trusted lifecycle owner, including safe idle, pending
// and unknown work, autonomous admission and deadline fencing. Wire booleans
// alone never constitute proof. It is rechecked on every grant and renewal.
type Verify func(context.Context, nodeplane.ClaimRequest) error

type Options struct {
	Directory string
	BotID     string
	Now       func() time.Time
	Verify    Verify
	// VerifyRenew proves the running owner still controls deadline fencing.
	// If absent, conservative claim eligibility (safe idle) is rechecked.
	VerifyRenew      func(context.Context, nodeplane.Lease, nodeplane.ClaimRequest) error
	ValidateSnapshot func(context.Context, []byte) (nodeplane.SnapshotRef, error)
	// PreferredNodeID is trusted native configuration, independent of view
	// selection. An empty value preserves ordinary first-eligible lease CAS.
	PreferredNodeID string
	// ReadOwnerEligibility reads the exact paired native owner. It is used only
	// to reclaim at safe idle; renewal verification remains separate so busy,
	// pending and unknown work can retain its existing authority.
	ReadOwnerEligibility func(context.Context, api.WorkTarget) (nodeplane.RuntimeEligibility, error)
}

type diskState struct {
	Format  int                    `json:"format"`
	BotID   string                 `json:"botId"`
	Counter string                 `json:"counter"`
	Lease   nodeplane.Lease        `json:"lease"`
	Claim   nodeplane.ClaimRequest `json:"claim"`
	Latest  nodeplane.SnapshotRef  `json:"latest"`
}

type Coordinator struct {
	mu         sync.Mutex
	opts       Options
	state      diskState
	deadline   time.Time
	quarantine time.Time
	last       time.Time
	poisoned   bool
	unlock     func()
	preference preferredIntent
}

func Open(o Options) (*Coordinator, error) {
	if !filepath.IsAbs(o.Directory) || filepath.Clean(o.Directory) != o.Directory || o.BotID == "" {
		return nil, errors.New("explicit private broker directory and Bot identity required")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if err := os.MkdirAll(o.Directory, 0700); err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(o.Directory)
	if err != nil || canonical != o.Directory {
		return nil, errors.New("broker directory redirected")
	}
	info, err := os.Lstat(o.Directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, errors.New("broker directory must be private")
	}
	_, lockStatErr := os.Lstat(filepath.Join(o.Directory, ".owner.lock"))
	previousOwner := lockStatErr == nil
	unlock, err := lockDirectory(filepath.Join(o.Directory, ".owner.lock"))
	if err != nil {
		return nil, err
	}
	c := &Coordinator{opts: o, unlock: unlock, state: diskState{Format: 1, BotID: o.BotID, Counter: "0"}}
	c.last = o.Now()
	path := filepath.Join(o.Directory, "state.json")
	info, err = os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 64<<10 {
			c.Close()
			return nil, errors.New("broker state must be private regular file")
		}
		b, e := os.ReadFile(path)
		if e != nil || len(b) > 64<<10 || json.Unmarshal(b, &c.state) != nil || c.state.Format != 1 || c.state.BotID != o.BotID || !validState(c.state, o.BotID) {
			c.Close()
			return nil, errors.New("invalid broker state")
		}
		// Durable UTC is diagnostic only. Restart has no surviving monotonic clock;
		// wait a complete TTL before any new side effect, even for an expired record.
		c.quarantine = c.last.Add(nodeplane.DefaultLeaseExpiry)
	} else if !errors.Is(err, os.ErrNotExist) {
		c.Close()
		return nil, err
	} else if previousOwner {
		c.Close()
		return nil, errors.New("broker durable state missing; epoch cannot be reset")
	} else if err = c.persist(c.state); err != nil {
		c.Close()
		return nil, err
	}
	if c.state.Latest.BotID != "" {
		if _, e := c.readSnapshot(context.Background(), c.state.Latest); e != nil {
			c.Close()
			return nil, e
		}
	}
	return c, nil
}

func validState(s diskState, bot string) bool {
	if !decimal(s.Counter, true) {
		return false
	}
	if s.Counter == "0" {
		if s.Lease.Epoch != "" || s.Claim.BotID != "" {
			return false
		}
	} else {
		if s.Lease.Epoch != s.Counter || s.Lease.BotID != bot || s.Claim.BotID != bot || s.Lease.NodeID != s.Claim.Target.NodeID || string(s.Lease.Backend) != s.Claim.Target.Backend {
			return false
		}
	}
	if s.Latest.BotID != "" && (s.Latest.BotID != bot || !decimal(s.Latest.Version, false) || !decimal(s.Latest.Epoch, true)) {
		return false
	}
	return true
}

func (c *Coordinator) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unlock != nil {
		c.unlock()
		c.unlock = nil
	}
	c.poisoned = true
}
func (c *Coordinator) tick(ctx context.Context) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	now := c.opts.Now()
	if now.Before(c.last) {
		c.poisoned = true
	}
	c.last = now
	if c.poisoned || c.unlock == nil || now.Before(c.quarantine) {
		return now, ErrUnavailable
	}
	return now, nil
}
func decimal(s string, zero bool) bool {
	if s == "0" {
		return zero
	}
	if s == "" || s[0] == '0' {
		return false
	}
	for _, v := range s {
		if v < '0' || v > '9' {
			return false
		}
	}
	return len(s) <= 128
}
func next(s string) string {
	n, _ := new(big.Int).SetString(s, 10)
	return n.Add(n, big.NewInt(1)).String()
}
func equalLease(a, b nodeplane.Lease) bool {
	return a.BotID == b.BotID && a.NodeID == b.NodeID && a.Backend == b.Backend && a.Epoch == b.Epoch && a.Epoch != ""
}
func (c *Coordinator) active(now time.Time) bool {
	return c.state.Lease.Epoch != "" && !c.deadline.IsZero() && now.Before(c.deadline)
}
func (c *Coordinator) verify(ctx context.Context, r nodeplane.ClaimRequest) error {
	if r.BotID != c.opts.BotID || r.Target.Role != api.RoleBot || r.Target.NodeID == "" || (r.Target.Backend != string(api.NodeCodex) && r.Target.Backend != string(api.NodeCaelis)) || !r.Proof.Controllable || r.Proof.NodeID != r.Target.NodeID || string(r.Proof.Backend) != r.Target.Backend || r.Proof.Epoch == "" || c.opts.Verify == nil {
		return ErrIneligible
	}
	if c.opts.Verify(ctx, r) != nil {
		return ErrIneligible
	}
	return ctx.Err()
}
func (c *Coordinator) Claim(ctx context.Context, r nodeplane.ClaimRequest) (nodeplane.Lease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now, err := c.tick(ctx)
	if err != nil {
		return nodeplane.Lease{}, err
	}
	if r.Snapshot != c.state.Latest || r.Snapshot.BotID != c.opts.BotID || r.Snapshot.Digest == "" {
		c.invalidatePreferredClaim(r)
		return nodeplane.Lease{}, ErrSnapshot
	}
	if _, err = c.readSnapshot(ctx, r.Snapshot); err != nil {
		return nodeplane.Lease{}, ErrSnapshot
	}
	if err = c.verify(ctx, r); err != nil {
		c.invalidatePreferredClaim(r)
		return nodeplane.Lease{}, err
	}
	now, err = c.tick(ctx)
	if err != nil {
		return nodeplane.Lease{}, err
	}
	if c.active(now) {
		// A repeated original CAS is a receipt lookup, not a renewal or a new epoch.
		if r == c.state.Claim {
			return c.grant(now), nil
		}
		c.observePreferredClaim(now, r)
		return nodeplane.Lease{}, ErrConflict
	}
	if r.ExpectedEpoch != c.state.Lease.Epoch {
		return nodeplane.Lease{}, ErrConflict
	}
	s := c.state
	s.Counter = next(s.Counter)
	s.Lease = nodeplane.Lease{BotID: r.BotID, NodeID: r.Target.NodeID, Backend: api.NodeBackend(r.Target.Backend), Epoch: s.Counter, ExpiresAt: now.Add(nodeplane.DefaultLeaseExpiry)}
	s.Claim = r
	if err = c.persist(s); err != nil {
		return nodeplane.Lease{}, err
	}
	c.state = s
	c.deadline = now.Add(nodeplane.DefaultLeaseExpiry)
	c.preference = preferredIntent{}
	return c.grant(now), nil
}
func (c *Coordinator) grant(now time.Time) nodeplane.Lease {
	l := c.state.Lease
	l.ExpiresAt = c.deadline
	l.TTLMs = min(nodeplane.DefaultLeaseExpiry.Milliseconds(), max(int64(0), c.deadline.Sub(c.opts.Now()).Milliseconds()))
	return l
}
func (c *Coordinator) Heartbeat(ctx context.Context, l nodeplane.Lease) (nodeplane.Lease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now, err := c.tick(ctx)
	if err != nil {
		return nodeplane.Lease{}, err
	}
	if !c.active(now) || !equalLease(l, c.state.Lease) {
		return nodeplane.Lease{}, ErrConflict
	}
	if !c.state.Claim.Proof.Controllable {
		return nodeplane.Lease{}, ErrIneligible
	}
	if c.state.Claim.Snapshot != c.state.Latest {
		return nodeplane.Lease{}, ErrSnapshot
	}
	if _, err = c.readSnapshot(ctx, c.state.Latest); err != nil {
		return nodeplane.Lease{}, ErrSnapshot
	}
	if c.opts.VerifyRenew != nil {
		err = c.opts.VerifyRenew(ctx, c.state.Lease, c.state.Claim)
	} else {
		err = c.verify(ctx, c.state.Claim)
	}
	if err != nil {
		return nodeplane.Lease{}, err
	}
	now, err = c.tick(ctx)
	if err != nil {
		return nodeplane.Lease{}, err
	}
	if !c.active(now) {
		return nodeplane.Lease{}, ErrConflict
	}
	if c.opts.PreferredNodeID != "" && c.preference.claim.BotID != "" {
		if err = c.reclaimPreferred(ctx); err != nil {
			return nodeplane.Lease{}, err
		}
		// Paired reads can take time; never renew across the old deadline or
		// after cancellation while observing a preferred candidate.
		now, err = c.tick(ctx)
		if err != nil {
			return nodeplane.Lease{}, err
		}
		if !c.active(now) {
			return nodeplane.Lease{}, ErrConflict
		}
	}
	s := c.state
	s.Lease.ExpiresAt = now.Add(nodeplane.DefaultLeaseExpiry)
	if err = c.persist(s); err != nil {
		return nodeplane.Lease{}, err
	}
	c.state = s
	c.deadline = s.Lease.ExpiresAt
	return c.grant(now), nil
}
func (c *Coordinator) Release(ctx context.Context, l nodeplane.Lease) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now, err := c.tick(ctx)
	if err != nil {
		return err
	}
	if !c.active(now) || !equalLease(l, c.state.Lease) {
		return ErrConflict
	}
	// No early regrant: local native quiescence cannot be inferred from a wire
	// release. Keep its epoch and conservative deadline, but stop renewal.
	s := c.state
	s.Claim.Proof.Controllable = false
	if err = c.persist(s); err != nil {
		return err
	}
	c.state = s
	return nil
}

// SeedSnapshot is explicit first-profile initialization. It is unavailable once
// any lease or snapshot has been committed; normal publishers cannot reset it.
func (c *Coordinator) SeedSnapshot(ctx context.Context, ref nodeplane.SnapshotRef, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.tick(ctx); err != nil {
		return err
	}
	if c.state.Counter != "0" || c.state.Latest.BotID != "" || ref.Epoch != "0" {
		return ErrConflict
	}
	if err := c.storeSnapshot(ctx, ref, payload); err != nil {
		return err
	}
	s := c.state
	s.Latest = ref
	if err := c.persist(s); err != nil {
		return err
	}
	c.state = s
	return nil
}
func (c *Coordinator) PublishSnapshot(ctx context.Context, l nodeplane.Lease, ref nodeplane.SnapshotRef, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now, err := c.tick(ctx)
	if err != nil {
		return err
	}
	if !c.active(now) || !equalLease(l, c.state.Lease) {
		return ErrConflict
	}
	if ref == c.state.Latest {
		_, err = c.readSnapshot(ctx, ref)
		return err
	}
	if ref.Epoch != l.Epoch || !decimal(ref.Version, false) {
		return ErrSnapshot
	}
	v, _ := new(big.Int).SetString(ref.Version, 10)
	prev, _ := new(big.Int).SetString(c.state.Latest.Version, 10)
	if prev == nil || v.Cmp(prev) <= 0 {
		return ErrSnapshot
	}
	if err = c.storeSnapshot(ctx, ref, payload); err != nil {
		return err
	}
	// Codec validation may take time. Recheck lease and cancellation immediately
	// before changing the authoritative pointer, after immutable staging.
	now, err = c.tick(ctx)
	if err != nil {
		return err
	}
	if !c.active(now) {
		return ErrConflict
	}
	previous := c.state.Latest
	s := c.state
	s.Latest = ref
	s.Claim.Snapshot = ref
	if err = c.persist(s); err != nil {
		return err
	}
	c.state = s
	c.preference = preferredIntent{}
	// The cache retains one complete bundle; remove the previous immutable
	// blob only after the new authoritative pointer is durably committed.
	if previous.Digest != ref.Digest {
		_ = os.Remove(c.snapshotPath(previous))
	}
	return nil
}
func (c *Coordinator) LatestSnapshot(ctx context.Context, bot string) (nodeplane.SnapshotRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nodeplane.SnapshotRef{}, err
	}
	if bot != c.opts.BotID || c.state.Latest.BotID == "" {
		return nodeplane.SnapshotRef{}, ErrSnapshot
	}
	if _, err := c.readSnapshot(ctx, c.state.Latest); err != nil {
		return nodeplane.SnapshotRef{}, err
	}
	return c.state.Latest, nil
}
func (c *Coordinator) ReadSnapshot(ctx context.Context, ref nodeplane.SnapshotRef) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ref != c.state.Latest {
		return nil, ErrSnapshot
	}
	return c.readSnapshot(ctx, ref)
}

// CommitInstall serializes the latest check and final offline-generation rename.
func (c *Coordinator) CommitInstall(ctx context.Context, ref nodeplane.SnapshotRef, install func() error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if ref != c.state.Latest || install == nil {
		return ErrSnapshot
	}
	if _, err := c.readSnapshot(ctx, ref); err != nil {
		return err
	}
	return install()
}
func (c *Coordinator) SnapshotState(ctx context.Context) (nodeplane.Lease, nodeplane.SnapshotRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nodeplane.Lease{}, nodeplane.SnapshotRef{}, err
	}
	return c.state.Lease, c.state.Latest, nil
}
func (c *Coordinator) snapshotPath(ref nodeplane.SnapshotRef) string {
	return filepath.Join(c.opts.Directory, "snapshot-"+ref.Digest+".json")
}
func (c *Coordinator) storeSnapshot(ctx context.Context, ref nodeplane.SnapshotRef, payload []byte) error {
	if ref.BotID != c.opts.BotID || !decimal(ref.Version, false) || !decimal(ref.Epoch, true) || len(payload) == 0 || len(payload) > MaxSnapshotBytes || len(ref.Digest) != 64 || c.opts.ValidateSnapshot == nil {
		return ErrSnapshot
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != ref.Digest {
		return ErrSnapshot
	}
	got, err := c.opts.ValidateSnapshot(ctx, payload)
	if err != nil || got != ref {
		return ErrSnapshot
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return atomicWrite(c.snapshotPath(ref), payload)
}
func (c *Coordinator) readSnapshot(ctx context.Context, ref nodeplane.SnapshotRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ref.BotID != c.opts.BotID || len(ref.Digest) != 64 {
		return nil, ErrSnapshot
	}
	for _, ch := range ref.Digest {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return nil, ErrSnapshot
		}
	}
	path := c.snapshotPath(ref)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > MaxSnapshotBytes {
		return nil, ErrSnapshot
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrSnapshot
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != ref.Digest {
		return nil, ErrSnapshot
	}
	return b, nil
}
func (c *Coordinator) persist(s diskState) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(c.opts.Directory, "state.json"), b); err != nil {
		c.poisoned = true
		return fmt.Errorf("broker durable commit uncertain: %w", err)
	}
	return nil
}
func atomicWrite(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".stage-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
