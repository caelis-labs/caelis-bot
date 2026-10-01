package roaming

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type ownerFixture struct {
	withdrawn, stopped atomic.Int32
	stopErr, idleErr   error
}

type deadlineOwner struct {
	*ownerFixture
	entered, proceed chan struct{}
	err              error
	deadline         time.Time
}

func (o *deadlineOwner) ConfigureLeaseDeadline(ctx context.Context, l nodeplane.Lease, deadline time.Time) error {
	o.deadline = deadline
	close(o.entered)
	select {
	case <-o.proceed:
		return o.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestLeaseAdmissionWaitsForIndependentNativeDeadline(t *testing.T) {
	for _, failing := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "watchdog-failed"}[failing], func(t *testing.T) {
			o := &deadlineOwner{ownerFixture: &ownerFixture{}, entered: make(chan struct{}), proceed: make(chan struct{})}
			if failing {
				o.err = errors.New("independent watcher unavailable")
			}
			g := NewGuard("owned-node", api.NodeCodex, o, true)
			defer g.Revoke()
			started := time.Now()
			done := make(chan error, 1)
			go func() { done <- g.Install(grant(), started) }()
			<-o.entered
			if _, _, err := g.Begin(t.Context()); !errors.Is(err, ErrFenced) {
				t.Fatal("effects admitted before independent deadline confirmation", err)
			}
			close(o.proceed)
			err := <-done
			if failing != (err != nil) {
				t.Fatal("wrong installation outcome", err)
			}
			if o.deadline != started.Add(45*time.Second) {
				t.Fatal("watcher did not receive conservative request-start deadline")
			}
			_, active := g.Lease()
			if active == failing {
				t.Fatal("watcher failure retained grant")
			}
		})
	}
}

func (o *ownerFixture) WithdrawWorkerGrants(context.Context) error { o.withdrawn.Add(1); return nil }
func (o *ownerFixture) Stop(context.Context) error                 { o.stopped.Add(1); return o.stopErr }
func (o *ownerFixture) SafeIdle(context.Context) error             { return o.idleErr }
func grant() nodeplane.Lease {
	return nodeplane.Lease{BotID: "stable-bot", NodeID: "owned-node", Backend: api.NodeCodex, Epoch: "broker-epoch-1", TTLMs: 60000}
}
func installed(t *testing.T) (*Guard, *ownerFixture, time.Time) {
	t.Helper()
	o := &ownerFixture{}
	g := NewGuard("owned-node", api.NodeCodex, o, true)
	start := time.Now()
	if err := g.Install(grant(), start); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Revoke)
	return g, o, start
}

type leaseWorkerFixture struct {
	api.WorkRuntime
	aware bool
}

func (w leaseWorkerFixture) LeaseAwareAdmission() bool { return w.aware }
func TestManagedWorkerGrantPreservesNativeSourceAndRequiresTargetCapability(t *testing.T) {
	g := NewGuard("owned-node", api.NodeCodex, &ownerFixture{}, true)
	if err := g.ConfigureBroker("paired-broker"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := g.Install(grant(), started); err != nil {
		t.Fatal(err)
	}
	defer g.Revoke()
	ctx, release, err := g.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	source := api.WorkDispatchSource{NodeID: api.LocalNodeID, Backend: "codex", BindingID: "opaque-binding", OperationID: "opaque-native-turn", Kind: "native_activation"}
	verified, err := g.AnnotateWorkSource(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if verified.NodeID != "owned-node" || verified.BindingID != source.BindingID || verified.OperationID != source.OperationID || verified.Lease != (api.WorkerLeaseGrant{BotID: "stable-bot", BrokerNodeID: "paired-broker", SourceNodeID: "owned-node", Backend: "codex", Epoch: grant().Epoch}) {
		t.Fatal("native source or broker identity changed", verified)
	}
	if _, err = g.AnnotateWorkSource(t.Context(), source); err == nil {
		t.Fatal("source callback admitted without operation ticket")
	}
	remote := api.WorkTarget{NodeID: "worker-node", Backend: "codex", Role: api.RoleWorker}
	if err = g.CheckWorkRuntime(ctx, remote, leaseWorkerFixture{aware: false}); err == nil {
		t.Fatal("unverified target admitted")
	}
	if err = g.CheckWorkRuntime(ctx, remote, leaseWorkerFixture{aware: true}); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	g.now = func() time.Time { return started.Add(46 * time.Second) }
	g.mu.Unlock()
	if _, err = g.AnnotateWorkSource(ctx, source); !errors.Is(err, ErrFenced) {
		t.Fatal("expired source retained grant", err)
	}
	if err = g.CheckWorkRuntime(ctx, remote, leaseWorkerFixture{aware: true}); err == nil {
		t.Fatal("target capability extended expired source authority")
	}
}

func TestRequestStartDeadlineRejectsDelayedResponseAndWallTimestamp(t *testing.T) {
	o := &ownerFixture{}
	g := NewGuard("owned-node", api.NodeCodex, o, true)
	start := time.Now().Add(-50 * time.Second)
	if err := g.Install(grant(), start); !errors.Is(err, ErrFenced) {
		t.Fatalf("late grant accepted: %v", err)
	}
	if err := g.Install(grant(), time.Now().Round(0)); err == nil {
		t.Fatal("serialized wall clock granted authority")
	}
	if _, _, err := g.Begin(t.Context()); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
	if o.stopped.Load() != 0 {
		t.Fatal("ungranted admission stopped prepared owner")
	}
}

func TestExpiryDuringNativeWriteBarrierPreventsEffectAndCancelsTurn(t *testing.T) {
	g, o, start := installed(t)
	ctx, release, err := g.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ready, resume := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	var writes atomic.Int32
	go func() {
		close(ready)
		<-resume
		if err := g.CheckContext(ctx); err != nil {
			result <- err
			return
		}
		writes.Add(1)
		result <- nil
	}()
	<-ready
	g.mu.Lock()
	g.now = func() time.Time { return start.Add(46 * time.Second) }
	g.mu.Unlock()
	if _, _, err = g.Begin(t.Context()); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
	close(resume)
	if err = <-result; err == nil {
		t.Fatal("expired native write passed barrier")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("turn context not canceled")
	}
	release()
	if err = g.WaitStopped(t.Context()); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 0 || o.withdrawn.Load() != 1 || o.stopped.Load() != 1 {
		t.Fatal("write or duplicate shutdown", writes.Load(), o.withdrawn.Load(), o.stopped.Load())
	}
}

func TestSuspendAndWakeRequireFreshOwnerAndNeverNewEpochOnOldRuntime(t *testing.T) {
	g, _, _ := installed(t)
	ctx, release, err := g.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	g.Suspend()
	release()
	if err = g.WaitStopped(t.Context()); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("suspend retained operation authority")
	}
	if err = g.Wake(); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
	lease := grant()
	lease.Epoch = "broker-epoch-2"
	if err = g.Install(lease, time.Now()); !errors.Is(err, ErrFenced) {
		t.Fatal("old runtime accepted new epoch")
	}
	fresh := NewGuard("owned-node", api.NodeCodex, &ownerFixture{}, true)
	if err = fresh.Install(lease, time.Now()); err != nil {
		t.Fatal(err)
	}
	fresh.Revoke()
}

func TestStaleTimerCannotRevokeRenewalAndWrongContextCannotDispatch(t *testing.T) {
	g, _, start := installed(t)
	old := g.deadline
	if err := g.Install(grant(), start.Add(time.Millisecond)); !errors.Is(err, ErrFenced) {
		t.Fatal("future monotonic start should fail", err)
	}
	newer := time.Now()
	if !newer.After(start) {
		t.Fatal("clock did not advance")
	}
	if err := g.Install(grant(), newer); err != nil {
		t.Fatal(err)
	}
	g.revokeIf(grant().Epoch, old)
	if err := g.Check(t.Context(), grant()); err != nil {
		t.Fatal("stale timer revoked renewed lease", err)
	}
	if err := g.CheckContext(t.Context()); !errors.Is(err, ErrFenced) {
		t.Fatal("context without exact guard ticket admitted")
	}
	lease := grant()
	lease.Epoch = "other"
	if err := g.Install(lease, time.Now()); !errors.Is(err, ErrFenced) {
		t.Fatal("different epoch renewed old runtime")
	}
}

func TestUnknownExternalWorkAndShutdownFailureNeverProduceQuiescenceProof(t *testing.T) {
	for _, failure := range []string{"stop", "unknown"} {
		t.Run(failure, func(t *testing.T) {
			g, o, _ := installed(t)
			if failure == "stop" {
				o.stopErr = errors.New("unconfirmed descendant")
			} else {
				o.idleErr = errors.New("external operation unknown")
			}
			g.Revoke()
			if err := g.WaitStopped(t.Context()); err == nil {
				t.Fatal("uncertain work claimed safe")
			}
			if _, err := g.Proof(t.Context(), grant()); err == nil {
				t.Fatal("stopped runtime claimed eligibility")
			}
		})
	}
}

func TestPreparedProofHasClosedAdmissionAndRejectsUnmanagedHost(t *testing.T) {
	for _, eligible := range []bool{false, true} {
		g := NewGuard("owned-node", api.NodeCodex, &ownerFixture{}, eligible)
		proof, err := g.Proof(t.Context(), grant())
		if eligible && (err != nil || !proof.Controllable) {
			t.Fatal(err)
		}
		if !eligible && err == nil {
			t.Fatal("shared owner admitted")
		}
		if _, _, err := g.Begin(t.Context()); err == nil {
			t.Fatal("prepared proof allowed work before grant")
		}
	}
}
