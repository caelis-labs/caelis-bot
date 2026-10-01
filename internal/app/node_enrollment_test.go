package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

func enrollmentNativeOwner(t *testing.T, a *Application, options NodeManagementNativeOptions) *nativeNodeManagement {
	t.Helper()
	options.LocalAgent = singleNodeFixture(api.LocalNodeID)
	if err := AttachNodeManagement(a, options); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Backend.CloseNodeManagement(context.Background()) })
	controller, err := backend.NativeNodeManagementController(a.Backend)
	if err != nil {
		t.Fatal(err)
	}
	return controller.(*nodeManagement).agent.(*nativeNodeManagement)
}
func enrollmentRequest(t *testing.T, a *Application, id string) api.NodeAddRequest {
	t.Helper()
	catalog, err := a.Backend.NodeCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return api.NodeAddRequest{OperationID: id, Label: "Fixture machine", Join: api.NodeSSH, SSHDestination: "fixture.invalid", ExpectedRevision: catalog.Revision}
}
func enrollmentRestart(a *Application) *Application {
	return &Application{root: a.root, Backend: backend.NewService(nil, nil, nil, nil, nil)}
}

func TestNativeNodeEnrollmentReadOnlySSHFailuresHaveOriginalFailedReceipts(t *testing.T) {
	for _, tc := range []struct{ name, diagnostic, reason string }{
		{"dns", "ssh: Could not resolve hostname fixture.invalid: nodename nor servname provided, or not known", "dns"},
		{"strict authorization", "Permission denied (publickey).", "authentication"},
		{"strict host key", "Host key verification failed.", "host-key"},
		{"network", "connect to host fixture.invalid port 22: Connection refused", "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			trace := filepath.Join(dir, "ssh-calls")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + nodeShellQuote(trace) + "\nprintf '%s\\n' " + nodeShellQuote(tc.diagnostic+" PRIVATE_NATIVE_DIAGNOSTIC") + " >&2\nexit 255\n"
			if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			var artifacts atomic.Int32
			a := nativeManagementApplication(t)
			native := enrollmentNativeOwner(t, a, NodeManagementNativeOptions{Artifact: func(string) (nodeagent.Artifact, error) {
				artifacts.Add(1)
				return nodeagent.Artifact{}, errors.New("must not prepare artifact")
			}})
			request := enrollmentRequest(t, a, "original-probe")
			first, err := a.Backend.AddNode(t.Context(), request)
			if err != nil || first.OperationID != request.OperationID || first.Outcome != "failed" || first.Reason != tc.reason {
				t.Fatal(first, err)
			}
			again, err := a.Backend.AddNode(t.Context(), request)
			if err != nil || again.Outcome != "failed" || again.Reason != tc.reason {
				t.Fatal("original failure was redispatched", again, err)
			}
			calls, err := os.ReadFile(trace)
			if err != nil || strings.Count(string(calls), "uname -sm") != 1 || strings.Contains(string(calls), "mkdir") || artifacts.Load() != 0 {
				t.Fatal("failed read-only probe reached mutation", string(calls), artifacts.Load(), err)
			}
			if _, err := os.Lstat(filepath.Join(native.directory, "config.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed probe published pairing", err)
			}
			catalog, err := a.Backend.NodeCatalog(t.Context())
			if err != nil || len(catalog.Nodes) != 1 || len(catalog.PendingEnrollments) != 0 {
				t.Fatal(catalog, err)
			}
			query, err := a.Backend.ReconcileNodeEnrollment(t.Context(), request.OperationID)
			if err != nil || query.Outcome != "failed" || query.Reason != tc.reason {
				t.Fatal(query, err)
			}
			for _, value := range []any{first, query} {
				body, _ := json.Marshal(value)
				if strings.Contains(string(body), "PRIVATE_NATIVE") || strings.Contains(string(body), request.SSHDestination) {
					t.Fatal("private probe diagnostic escaped", string(body))
				}
			}
			if err := a.Backend.CloseNodeManagement(t.Context()); err != nil {
				t.Fatal(err)
			}
			reopened := enrollmentRestart(a)
			enrollmentNativeOwner(t, reopened, NodeManagementNativeOptions{})
			query, err = reopened.Backend.ReconcileNodeEnrollment(t.Context(), request.OperationID)
			if err != nil || query.Outcome != "failed" || query.Reason != tc.reason {
				t.Fatal("restart lost original failure", query, err)
			}
			info, err := os.Stat(native.enrollmentPath(request.OperationID))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("receipt not private", err)
			}
			info, err = os.Stat(filepath.Dir(native.enrollmentPath(request.OperationID)))
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatal("receipt directory not private", err)
			}
		})
	}
}

func TestNativeNodeEnrollmentUnknownBootstrapNeverRedispatchesAcrossRestart(t *testing.T) {
	var starts atomic.Int32
	options := NodeManagementNativeOptions{Bootstrap: func(context.Context, api.NodeAddRequest) (NodeRegistration, error) {
		starts.Add(1)
		return NodeRegistration{}, errors.New("PRIVATE_POST_BOOTSTRAP_DISCONNECT")
	}}
	a := nativeManagementApplication(t)
	native := enrollmentNativeOwner(t, a, options)
	request := enrollmentRequest(t, a, "original-bootstrap")
	first, err := a.Backend.AddNode(t.Context(), request)
	if err != nil || first.Outcome != "unknown" || first.OperationID != request.OperationID {
		t.Fatal(first, err)
	}
	for i := 0; i < 2; i++ {
		again, err := a.Backend.AddNode(t.Context(), request)
		if err != nil || again.Outcome != "unknown" {
			t.Fatal(again, err)
		}
	}
	changed := request
	changed.Label = "Different machine"
	if got, err := a.Backend.AddNode(t.Context(), changed); err != nil || got.Outcome != "unknown" {
		t.Fatal("original intent changed", got, err)
	}
	fresh := request
	fresh.OperationID = "replacement-forbidden"
	blocked, err := a.Backend.AddNode(t.Context(), fresh)
	if err != nil || blocked.OperationID != request.OperationID || blocked.Outcome != "unknown" || blocked.Reason != "original-pending" || starts.Load() != 1 {
		t.Fatal("unknown operation allowed replacement", blocked, starts.Load(), err)
	}
	if err := a.Backend.CloseNodeManagement(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened := enrollmentRestart(a)
	enrollmentNativeOwner(t, reopened, options)
	catalog, err := reopened.Backend.NodeCatalog(t.Context())
	if err != nil || len(catalog.PendingEnrollments) != 1 || catalog.PendingEnrollments[0] != request.OperationID || len(catalog.Nodes) != 1 {
		t.Fatal("restart lost original pending identity", catalog, err)
	}
	query, err := reopened.Backend.ReconcileNodeEnrollment(t.Context(), request.OperationID)
	if err != nil || query.Outcome != "unknown" || query.OperationID != request.OperationID || starts.Load() != 1 {
		t.Fatal("query dispatched bootstrap", query, starts.Load(), err)
	}
	body, err := os.ReadFile(native.enrollmentPath(request.OperationID))
	if err != nil || strings.Contains(string(body), "PRIVATE_POST") {
		t.Fatal("native diagnostic persisted", err)
	}
}

func TestNativeNodeEnrollmentPublishedPairingProvesOriginalCommitAfterLostReceipt(t *testing.T) {
	var starts atomic.Int32
	options := NodeManagementNativeOptions{Bootstrap: func(_ context.Context, r api.NodeAddRequest) (NodeRegistration, error) {
		starts.Add(1)
		return NodeRegistration{ID: "exact-enrolled-node", Label: r.Label, Join: r.Join, SSHDestination: r.SSHDestination, Directory: "/fixture/private-node", HelperPath: "/fixture/private-node/agent"}, nil
	}, Dial: func(_ context.Context, r NodeRegistration) (nodeplane.CatalogAgent, error) {
		return singleNodeFixture(r.ID), nil
	}}
	a := nativeManagementApplication(t)
	native := enrollmentNativeOwner(t, a, options)
	request := enrollmentRequest(t, a, "original-commit")
	first, err := a.Backend.AddNode(t.Context(), request)
	if err != nil || first.Outcome != "committed" || first.Node.ID != "exact-enrolled-node" {
		t.Fatal(first, err)
	}
	// Model final receipt loss after the original pairing was durably published.
	record, err := readEnrollmentRecord(native.enrollmentPath(request.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	record.Result.Outcome = "unknown"
	record.Result.Reason = "unknown"
	if err = nodeagent.WriteManagedPrivateJSON(native.enrollmentPath(request.OperationID), record); err != nil {
		t.Fatal(err)
	}
	if err = a.Backend.CloseNodeManagement(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened := enrollmentRestart(a)
	enrollmentNativeOwner(t, reopened, options)
	query, err := reopened.Backend.ReconcileNodeEnrollment(t.Context(), request.OperationID)
	if err != nil || query.Outcome != "committed" || query.OperationID != request.OperationID || query.Node.ID != first.Node.ID || starts.Load() != 1 {
		t.Fatal("lost receipt changed original registration", query, starts.Load(), err)
	}
	again, err := reopened.Backend.AddNode(t.Context(), request)
	if err != nil || again.Outcome != "committed" || again.Node.ID != first.Node.ID || starts.Load() != 1 {
		t.Fatal("changed catalog caused original redispatch", again, starts.Load(), err)
	}
}

func TestNativeNodeEnrollmentCapacityIsFailedBeforeBootstrap(t *testing.T) {
	var starts atomic.Int32
	a := nativeManagementApplication(t)
	native := enrollmentNativeOwner(t, a, NodeManagementNativeOptions{Bootstrap: func(context.Context, api.NodeAddRequest) (NodeRegistration, error) {
		starts.Add(1)
		return NodeRegistration{}, errors.New("must not dispatch")
	}})
	request := enrollmentRequest(t, a, "capacity-rejected")
	directory := filepath.Dir(native.enrollmentPath(request.OperationID))
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		r := request
		r.OperationID = fmt.Sprintf("terminal-%d", i)
		record := nodeEnrollmentRecord{Version: 1, Request: r, Digest: enrollmentDigest(r), Phase: "preflight", Result: failedEnrollment(r.OperationID, "preflight")}
		if err := nodeagent.WriteManagedPrivateJSON(native.enrollmentPath(r.OperationID), record); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.Backend.AddNode(t.Context(), request)
	if err != nil || result.Outcome != "failed" || result.Reason != "limit" || starts.Load() != 0 {
		t.Fatal("capacity rejection became uncertain dispatch", result, starts.Load(), err)
	}
	catalog, err := a.Backend.NodeCatalog(t.Context())
	if err != nil || catalog.Revision != request.ExpectedRevision || len(catalog.PendingEnrollments) != 0 || len(catalog.Nodes) != 1 {
		t.Fatal("terminal journal changed catalog revision or pairing", catalog, err)
	}
	if _, err := os.Stat(filepath.Join(native.directory, "config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("capacity rejection changed pairing", err)
	}
}

func TestNativeNodeEnrollmentPreflightJournalDoesNotInvalidateItsOwnCatalogGuard(t *testing.T) {
	a := nativeManagementApplication(t)
	var original api.NodeAddRequest
	options := NodeManagementNativeOptions{Bootstrap: func(ctx context.Context, r api.NodeAddRequest) (NodeRegistration, error) {
		catalog, err := a.Backend.NodeCatalog(ctx)
		if err != nil || catalog.Revision != original.ExpectedRevision || len(catalog.PendingEnrollments) != 1 || catalog.PendingEnrollments[0] != r.OperationID {
			t.Fatal("original journal invalidated its displayed catalog guard", catalog, err)
		}
		return NodeRegistration{}, errors.New("synthetic post-dispatch disconnect")
	}}
	enrollmentNativeOwner(t, a, options)
	original = enrollmentRequest(t, a, "exact-catalog-guard")
	result, err := a.Backend.AddNode(t.Context(), original)
	if err != nil || result.Outcome != "unknown" {
		t.Fatal(result, err)
	}
}

func TestNativeNodeEnrollmentUnavailablePrivateJournalRejectsBeforeBootstrap(t *testing.T) {
	var starts atomic.Int32
	a := nativeManagementApplication(t)
	native := enrollmentNativeOwner(t, a, NodeManagementNativeOptions{Bootstrap: func(context.Context, api.NodeAddRequest) (NodeRegistration, error) {
		starts.Add(1)
		return NodeRegistration{}, errors.New("must not dispatch")
	}})
	request := enrollmentRequest(t, a, "private-storage-unavailable")
	if err := os.Mkdir(filepath.Dir(native.enrollmentPath(request.OperationID)), 0755); err != nil {
		t.Fatal(err)
	}
	result, err := a.Backend.AddNode(t.Context(), request)
	if err != nil || result.Outcome != "failed" || result.Reason != "receipt-unavailable" || starts.Load() != 0 {
		t.Fatal("unavailable journal admitted bootstrap", result, starts.Load(), err)
	}
	if _, err := os.Lstat(filepath.Join(native.directory, "config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unavailable journal published pairing", err)
	}
}
