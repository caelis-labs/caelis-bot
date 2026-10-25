package machines

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPendingPeerIsNonblockingExcludedAndOnlyCheckedExplicitly(t *testing.T) {
	target := newDualPeerTarget(t)
	s, err := Open(t.TempDir(), inert{})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	v, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: "pending", Address: target.http.URL})
	if err != nil || v.PairingStatus != "pending" || time.Since(before) > time.Second {
		t.Fatal("request waited for human", v, err)
	}
	if len(s.TaskMachines()) != 0 || len(s.state.Routes) != 0 || len(target.owners) != 0 {
		t.Fatal("pending created delegation/native authority")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.observeMachineProfiles(ctx); close(done) }()
	p := s.state.Profiles[v.ID]
	if err = target.peer.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: p.PairingEpoch, Allow: true}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if s.Machines()[0].PairingStatus != "pending" || len(target.owners) != 0 {
		t.Fatal("background health polled pending authorization")
	}
	reopened, err := Open(s.root, inert{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.InspectMachine(t.Context(), v.ID, "")
	if err != nil || got.PairingStatus != "approved" || got.State != "ready" || len(reopened.TaskMachines()) != 1 {
		t.Fatal("explicit check did not recover original approval", got, err)
	}
	if reopened.state.Profiles[v.ID].Peer.Token != p.Peer.Token || reopened.state.Profiles[v.ID].PairingEpoch != p.PairingEpoch {
		t.Fatal("check replaced identity")
	}
}
func TestUnconfirmedTargetCannotBeReplacedOrCertifiedByOtherPeer(t *testing.T) {
	first, second := newDualPeerTarget(t), newDualPeerTarget(t)
	drop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != workerpeer.Prefix+"pair" {
			first.peer.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		first.peer.ServeHTTP(rec, r)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer drop.Close()
	s, err := Open(t.TempDir(), inert{})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.PrepareBotConnection(api.BotConnectionInput{ID: "original", Address: drop.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ConnectPreparedBot(t.Context(), ref, api.BotConnectionInput{ID: "original", Address: drop.URL}); err == nil {
		t.Fatal("lost response became success")
	}
	changed, prepErr := s.PrepareBotConnection(api.BotConnectionInput{ID: "original", Address: second.http.URL, Reconnect: true}, nil)
	if prepErr == nil {
		if _, _, err = s.ConnectPreparedBot(t.Context(), changed, api.BotConnectionInput{ID: "original", Address: second.http.URL, Reconnect: true}); err == nil {
			t.Fatal("unknown target switched peer")
		}
	}
	if _, err = s.ReadBotConnectionReceipt(t.Context(), ref); err == nil {
		t.Fatal("unknown original receipt certified")
	}
	if len(second.peer.Status().Requests) != 0 {
		t.Fatal("changed address sent a replacement request")
	}
}
func TestLaterIntentCannotCertifyOriginalRequestOrCrossSourceProof(t *testing.T) {
	target := newDualPeerTarget(t)
	s, err := Open(t.TempDir(), inert{})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.PrepareBotConnection(api.BotConnectionInput{ID: "target", Address: target.http.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ConnectPreparedBot(t.Context(), ref, api.BotConnectionInput{ID: "target", Address: target.http.URL}); err != nil {
		t.Fatal(err)
	}
	if err = target.peer.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: ref.Pairing, Allow: false}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.InspectMachine(t.Context(), "target", ""); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.PrepareBotConnection(api.BotConnectionInput{ID: "target", Address: target.http.URL, Reconnect: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Pairing == ref.Pairing {
		t.Fatal("explicit new intent reused old ID")
	}
	if _, _, err = s.ConnectPreparedBot(t.Context(), fresh, api.BotConnectionInput{ID: "target", Address: target.http.URL}); err != nil {
		t.Fatal(err)
	}
	if err = target.peer.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: fresh.Pairing, Allow: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadBotConnectionReceipt(t.Context(), ref); err == nil {
		t.Fatal("later approval certified old receipt")
	}
	if _, err = workerpeer.RequestPairing(t.Context(), target.http.URL, rand.Text(), s.state.ProfileID, "Other", rand.Text()+rand.Text(), target.peer.Status().Peer); err == nil {
		t.Fatal("another proof took source")
	}
}
func TestRejectedFirstRequestCanBeRemovedAndCorrected(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "pairing_requests_full", 429) }))
	defer target.Close()
	s, err := Open(t.TempDir(), inert{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ConnectBot(t.Context(), api.BotConnectionInput{ID: "first", Address: target.URL})
	var rejected *api.ActionRejectedError
	if !errors.As(err, &rejected) || len(s.Machines()) != 0 {
		t.Fatal("known no-write refusal retained an unusable profile", err, s.Machines())
	}
}

func TestUndispatchedFirstIntentCanBeCertifiedWithoutNetworkReplay(t *testing.T) {
	s, err := Open(t.TempDir(), inert{})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.PrepareBotConnection(api.BotConnectionInput{ID: "first", Address: "http://127.0.0.1:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ReadBotConnectionReceipt(t.Context(), ref)
	var rejected *api.ActionRejectedError
	if !errors.As(err, &rejected) || rejected.Code != "pairing_request_not_dispatched" {
		t.Fatal("unsent intent trapped unknown", err)
	}
}

func TestArchivedPendingReAddReadsOriginalWithoutResubmitting(t *testing.T) {
	target := newDualPeerTarget(t)
	s, err := Open(t.TempDir(), inert{})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: "original", Address: target.http.URL})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.machineProfile(v.ID)
	if err = s.RemoveMachine(t.Context(), v.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := s.ConnectBot(t.Context(), api.BotConnectionInput{ID: v.ID, Address: target.http.URL, Reconnect: true})
	after, _ := s.machineProfile(v.ID)
	if err != nil || restored.Archived || after.PairingEpoch != before.PairingEpoch || after.Peer != before.Peer || restored.PairingStatus != "pending" {
		t.Fatal("archived original request could not be restored", restored, err)
	}
	if len(target.peer.Status().Requests) != 1 {
		t.Fatal("restore created another request")
	}
	approveMachine(t, target, s, v.ID)
	in := api.TaskStart{Machine: v.ID, RequestID: "start-original", Prompt: "fixture"}
	workspace, prepErr := s.PrepareRemoteWork(t.Context(), in, "retained-task")
	if prepErr != nil {
		t.Fatal(prepErr)
	}
	if _, err = s.StartWork(t.Context(), api.WorkStart{ID: "retained-task", Workspace: workspace, TaskStart: in}); err != nil {
		t.Fatal(err)
	}
	before, _ = s.machineProfile(v.ID)
	if err = s.RemoveMachine(t.Context(), v.ID); err != nil {
		t.Fatal(err)
	}
	restored, err = s.ConnectBot(t.Context(), api.BotConnectionInput{ID: v.ID, Address: target.http.URL, Reconnect: true})
	after, _ = s.machineProfile(v.ID)
	if err != nil || restored.State != "ready" || restored.Archived || after.PairingEpoch != before.PairingEpoch || len(target.peer.Status().Requests) != 0 {
		t.Fatal("approved restore demanded another authorization", restored, err)
	}
}
