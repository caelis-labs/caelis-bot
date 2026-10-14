package caelis

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

type grantFixture struct {
	mu              sync.Mutex
	life            wire.ApplicationConnection
	receipts        map[string]connectionGrantReceipt
	posts           []string
	dropNext        bool
	badAppRead      bool
	badHostRead     bool
	badReceiptID    bool
	revokeAfterPost bool
	caps            []string
}

func newGrantFixture(t *testing.T) (*Session, *grantFixture) {
	t.Helper()
	f := &grantFixture{life: wire.ApplicationConnection{ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", ExpiresAt: time.Now().Add(time.Hour), Name: "Caelis Bot"}, receipts: map[string]connectionGrantReceipt{}}
	f.caps = append(append([]string{}, required...), "display-replacements-v1", connectionGrantCapability, "application-execution-approval-never-v1", "worker-creation-permissions-v1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/schema/") {
			w.Header().Set("X-Caelis-Control-Schema", "1.0")
		}
		path := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/control/v1/schema"), "/api/control/v1")
		if path == "/initialize" {
			writeFixture(w, wire.ServerInfo{ApiVersion: "v1", ProtocolVersion: 1, EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: f.caps})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(path, "/applications/connections/") && auth != "Bearer host-token" || path == "/application/connection" && auth != "Bearer app-token" {
			w.WriteHeader(http.StatusForbidden)
			writeFixture(w, map[string]string{"code": "unauthorized"})
			return
		}
		switch {
		case r.Method == "GET" && path == "/applications/connections/client":
			life := f.life
			if f.badHostRead {
				life.ConnectionId = "other"
			}
			writeFixture(w, life)
		case r.Method == "GET" && path == "/application/connection":
			life := f.life
			if f.badAppRead {
				life.AllowUnreviewedApplicationExecution = pointer(false)
			}
			writeFixture(w, life)
		case r.Method == "GET" && strings.HasPrefix(path, "/applications/connections/client/grant-updates/"):
			op := strings.TrimPrefix(path, "/applications/connections/client/grant-updates/")
			if receipt, ok := f.receipts[op]; ok {
				writeFixture(w, receipt)
			} else {
				w.WriteHeader(http.StatusNotFound)
				writeFixture(w, map[string]string{"code": "not_found"})
			}
		case r.Method == "POST" && path == "/applications/connections/client/grants":
			var req connectionGrantRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil || req.OperationID == "" || r.Header.Get("Idempotency-Key") != req.OperationID {
				t.Error("grant request or idempotency header invalid")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.posts = append(f.posts, req.OperationID)
			if f.dropNext {
				f.dropNext = false
				drop(w)
				return
			}
			if req.ExpectedAllowWorkerFullAccess == nil || req.ExpectedAllowUnreviewedApplicationExecution == nil || *req.ExpectedAllowWorkerFullAccess != value(f.life.AllowWorkerFullAccess) || *req.ExpectedAllowUnreviewedApplicationExecution != value(f.life.AllowUnreviewedApplicationExecution) {
				w.WriteHeader(http.StatusConflict)
				writeFixture(w, map[string]string{"code": "conflict"})
				return
			}
			if req.AllowWorkerFullAccess != nil {
				f.life.AllowWorkerFullAccess = req.AllowWorkerFullAccess
			}
			if req.AllowUnreviewedApplicationExecution != nil {
				f.life.AllowUnreviewedApplicationExecution = req.AllowUnreviewedApplicationExecution
			}
			receipt := connectionGrantReceipt{OperationID: req.OperationID, Connection: f.life}
			if f.badReceiptID {
				receipt.OperationID = "other-operation"
			}
			f.receipts[req.OperationID] = receipt
			if f.revokeAfterPost {
				if req.AllowWorkerFullAccess != nil {
					f.life.AllowWorkerFullAccess = pointer(false)
				} else {
					f.life.AllowUnreviewedApplicationExecution = pointer(false)
				}
			}
			writeFixture(w, receipt)
		default:
			t.Errorf("unexpected grant fixture request: %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	store := t.TempDir()
	serviceDir := filepath.Join(store, "runtime", "service")
	if err := os.MkdirAll(serviceDir, 0700); err != nil {
		t.Fatal(err)
	}
	discovery, _ := json.Marshal(discovery{Schema: "caelis.control.service-discovery/v1", Endpoint: server.URL, InstanceID: "instance", PrincipalID: "owner"})
	if err := os.WriteFile(filepath.Join(serviceDir, "discovery.json"), discovery, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serviceDir, "auth.token"), []byte("host-token"), 0600); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(t.TempDir(), "private")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Directory: private, Settings: api.RuntimeSettings{Runtime: "caelis", CaelisStore: store}})
	app, err := newClient(server.URL, "app-token")
	if err != nil {
		t.Fatal(err)
	}
	app.schema = true
	s.client, s.info, s.connected = app, wire.ServerInfo{Capabilities: f.caps}, true
	s.state.StoreID, s.state.InstanceID, s.state.PrincipalID, s.state.Endpoint, s.state.Connection = "store", "instance", "owner", server.URL, f.life
	s.state.Session = wire.ApplicationBinding{SessionId: "original", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: wire.ApplicationProfile{Permissions: &wire.ApplicationPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("manual")}}}
	s.state.Workers["old-task"] = worker{Task: api.Task{ID: "old-task", Status: "working"}, Binding: wire.ApplicationBinding{SessionId: "old-worker", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner"}}
	return s, f
}

func TestFullAccessGrantScopesKeepConnectionAndExistingBinding(t *testing.T) {
	s, f := newGrantFixture(t)
	old := clone(s.state.Session)
	if err := s.AuthorizeFullAccess(t.Context(), "bot"); err != nil {
		t.Fatal(err)
	}
	if !value(f.life.AllowUnreviewedApplicationExecution) || value(f.life.AllowWorkerFullAccess) || s.checkFutureBotMode("full-access") != nil || s.CheckFutureWorkExecutionMode("full-access") == nil {
		t.Fatal("Bot grant affected Worker ceiling or failed native readback", f.life)
	}
	if err := s.AuthorizeFullAccess(t.Context(), "worker"); err != nil {
		t.Fatal(err)
	}
	if !value(f.life.AllowWorkerFullAccess) || s.CheckFutureWorkExecutionMode("full-access") != nil || s.state.Connection.ConnectionId != "client" || len(f.posts) != 2 || s.state.Session.SessionId != old.SessionId || value(s.state.Session.Profile.Permissions.Mode) != value(old.Profile.Permissions.Mode) || s.state.Workers["old-task"].Binding.SessionId != "old-worker" || s.state.Workers["old-task"].Task.Status != "working" {
		t.Fatal("grant changed original binding or connection", f.life, s.state.Session, f.posts)
	}
	if err := s.AuthorizeFullAccess(t.Context(), "bot"); err != nil || len(f.posts) != 2 {
		t.Fatal("already granted Bot scope dispatched another mutation", err, f.posts)
	}
}

func TestFullAccessUnknownWriteRetriesOriginalOperation(t *testing.T) {
	s, f := newGrantFixture(t)
	f.dropNext = true
	if err := s.AuthorizeFullAccess(t.Context(), "worker"); err == nil || len(f.posts) != 1 {
		t.Fatal("lost write was treated as a confirmed grant", err, f.posts)
	}
	if _, err := privateRead(grantIntentPath(s.path, "worker"), 65536); err != nil {
		t.Fatal("unknown original operation was not retained", err)
	}
	if err := s.AuthorizeFullAccess(t.Context(), "worker"); err != nil || len(f.posts) != 2 || f.posts[0] != f.posts[1] || !value(f.life.AllowWorkerFullAccess) {
		t.Fatal("unknown operation was replaced instead of retried", err, f.posts)
	}
	if _, err := os.Stat(grantIntentPath(s.path, "worker")); !os.IsNotExist(err) {
		t.Fatal("confirmed intent was not cleared", err)
	}
}

func TestFullAccessGrantRequiresCurrentHostContractAndReadback(t *testing.T) {
	for _, problem := range []string{"old-host", "host-identity", "app-readback", "receipt-id", "receipt-not-current", "invalid-scope"} {
		t.Run(problem, func(t *testing.T) {
			s, f := newGrantFixture(t)
			switch problem {
			case "old-host":
				s.info.Capabilities = slices.DeleteFunc(slices.Clone(f.caps), func(cap string) bool { return cap == connectionGrantCapability })
			case "host-identity":
				f.badHostRead = true
			case "app-readback":
				f.badAppRead = true
			case "receipt-id":
				f.badReceiptID = true
			case "receipt-not-current":
				f.revokeAfterPost = true
			}
			scope := "bot"
			if problem == "invalid-scope" {
				scope = "both"
			}
			err := s.AuthorizeFullAccess(t.Context(), scope)
			if err == nil {
				t.Fatal("unverified full access was accepted")
			}
			if problem != "app-readback" && problem != "receipt-id" && problem != "receipt-not-current" && len(f.posts) != 0 {
				t.Fatal("rejected preflight changed Host grant", f.posts)
			}
			if problem == "app-readback" || problem == "receipt-id" || problem == "receipt-not-current" {
				if len(f.posts) != 1 || s.checkFutureBotMode("full-access") == nil {
					t.Fatal("unverified application readback enabled future full access", f.posts)
				}
				if _, e := privateRead(grantIntentPath(s.path, "bot"), 65536); e != nil {
					t.Fatal("uncertain readback lost original grant receipt", e)
				}
			}
		})
	}
}

func TestPreviouslyGrantedConnectionDoesNotRequireGrantUpdateCapability(t *testing.T) {
	s, f := newGrantFixture(t)
	f.caps = slices.DeleteFunc(f.caps, func(cap string) bool { return cap == connectionGrantCapability })
	s.info.Capabilities = slices.Clone(f.caps)
	f.life.AllowUnreviewedApplicationExecution = pointer(true)
	if err := s.AuthorizeFullAccess(t.Context(), "bot"); err != nil || len(f.posts) != 0 || s.checkFutureBotMode("full-access") != nil {
		t.Fatal("an existing Host grant was treated as a request for new authority", err, f.posts)
	}
}
