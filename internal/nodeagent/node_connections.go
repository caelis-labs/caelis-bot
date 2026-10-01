package nodeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

// NodeConnectionOwner is a native-only, exact owned Host setup lease. A warm
// managed owner must return a borrowed lease whose Close only detaches setup.
// No renderer request can inject settings, executable, Store or Host authority.
type NodeConnectionOwner interface {
	SetupSettings(context.Context) (api.RuntimeSettings, error)
	Check(context.Context) error
	Close(context.Context) error
	Done() <-chan struct{}
}

type nodeConnectionRecord struct {
	Schema           int                          `json:"schema"`
	Ref              api.NodeRuntimeConnectionRef `json:"ref"`
	Guard            api.NodeEditGuard            `json:"guard"`
	Binding          string                       `json:"binding"`
	Outcome          string                       `json:"outcome"`
	CleanupConfirmed bool                         `json:"cleanupConfirmed"`
	FlowDispatched   bool                         `json:"flowDispatched"`
	Rejected         bool                         `json:"rejected,omitempty"`
}
type nodeConnectionSession struct {
	mu       sync.Mutex
	record   nodeConnectionRecord
	owner    NodeConnectionOwner
	sdk      *caelis.Connections
	metadata OwnedRuntimeSettings
	flows    map[string]bool
	cancel   context.CancelFunc
	done     chan struct{}
	started  bool
	flowID   string
	closing  bool
	closeErr error
}

var _ api.NodeRuntimeConnectionController = (*Service)(nil)
var _ api.NodeRuntimeConnectionController = (*Client)(nil)

func validConnectionRef(ref api.NodeRuntimeConnectionRef) bool {
	return ref.Backend == api.NodeCaelis && identifier.MatchString(ref.NodeID) && identifier.MatchString(ref.OperationID)
}
func connectionBinding(v OwnedRuntimeSettings) string {
	body, _ := json.Marshal(v)
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}
func (s *Service) connectionPath(ref api.NodeRuntimeConnectionRef) string {
	h := sha256.Sum256([]byte(string(ref.Backend) + "\x00" + ref.OperationID))
	return filepath.Join(s.options.Directory, "node-connections", hex.EncodeToString(h[:])+".json")
}
func (s *Service) connectionRecord(ref api.NodeRuntimeConnectionRef) (nodeConnectionRecord, error) {
	var r nodeConnectionRecord
	err := readPrivateJSON(s.connectionPath(ref), &r)
	if err != nil {
		return r, err
	}
	if r.Schema != 1 || r.Ref != ref || !validConnectionRef(r.Ref) || r.Guard.NodeID != ref.NodeID || r.Guard.Backend != ref.Backend || r.Guard.Revision == "" || len(r.Binding) != 64 || (r.Outcome != "intent" && r.Outcome != "active" && r.Outcome != "closed" && r.Outcome != "unknown") || (r.CleanupConfirmed != (r.Outcome == "closed")) || r.Rejected && (r.Outcome != "closed" || r.FlowDispatched) {
		return r, errors.New("original setup record invalid")
	}
	return r, nil
}

// Store initialization is allowed only for an absent designated path. A warm
// native owner may prove a busy marked Store, but never an unmarked directory.
func connectionStoreAvailable(nodeID, store string, warmOwner bool) bool {
	eligible, reason := caelis.ProbeOwnedStore(nodeID, store)
	if eligible {
		return true
	}
	if reason == "owned-store-setup-required" {
		_, err := os.Lstat(store)
		return errors.Is(err, os.ErrNotExist)
	}
	return warmOwner && (reason == "owned-store-controller-busy" || reason == "owned-store-discovery-present")
}

// NodeConnectionError exposes only a fixed nonsecret code and uncertainty.
// Native SDK causes remain local and can be inspected with errors.Is, but are
// never included in Error, JSON, transport responses, journals or receipts.
type NodeConnectionError struct {
	Code    string `json:"code"`
	Unknown bool   `json:"unknown"`
	cause   error
}

func (e *NodeConnectionError) Error() string { return "node connection " + e.Code }
func (e *NodeConnectionError) Unwrap() error { return e.cause }
func connectionError(reason string) error {
	code := strings.ReplaceAll(reason, " ", "-")
	unknown := strings.Contains(reason, "outcome") || strings.Contains(reason, "unconfirmed") || strings.Contains(reason, "original")
	return &NodeConnectionError{Code: code, Unknown: unknown}
}
func connectionSDKError(reason string, cause error) error {
	e := connectionError(reason).(*NodeConnectionError)
	e.cause = cause
	return e
}

// Begin is the sole explicit user action allowed to prepare an absent private
// Store and start a bounded setup Host. Replays consult the original record
// before guard checks, and can never redispatch an unresolved original intent.
func (s *Service) BeginNodeRuntimeConnection(ctx context.Context, guard api.NodeEditGuard, operationID string) (api.NodeRuntimeConnectionRef, error) {
	ref := api.NodeRuntimeConnectionRef{NodeID: guard.NodeID, Backend: guard.Backend, OperationID: operationID}
	if !validConnectionRef(ref) || ref.NodeID != s.options.NodeID || guard.Revision == "" {
		return ref, connectionError("scope changed")
	}
	if err := ctx.Err(); err != nil {
		return ref, err
	}
	if port, delegated, err := s.managedNodeConnections(ctx, ref.NodeID, ref.Backend); delegated {
		if err != nil {
			return ref, err
		}
		defer closeManagedNodeConnectionController(port)
		return port.BeginNodeRuntimeConnection(ctx, guard, operationID)
	}
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()
	if prior, err := s.connectionRecord(ref); err == nil {
		if prior.Guard != guard {
			return ref, connectionError("original intent changed")
		}
		if prior.Rejected {
			return ref, connectionError("begin rejected")
		}
		if prior.Outcome == "active" && s.connections[ref.OperationID] != nil {
			return ref, nil
		}
		return ref, connectionError("original outcome unavailable")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ref, connectionError("original outcome unavailable")
	}
	if !codex.OwnedRuntimeSupported() {
		return ref, connectionError("unsupported platform")
	}
	view, err := s.Configuration(ctx, ref.NodeID, ref.Backend)
	if err != nil || view.Guard != guard {
		return ref, connectionError("settings changed before begin")
	}
	if view.Executable == nil || !view.Executable.Installed {
		return ref, connectionError("runtime unavailable")
	}
	metadata, err := s.ReadOwnedRuntimeSettings(ctx, ref.NodeID, ref.Backend)
	if err != nil {
		return ref, connectionError("runtime unavailable")
	}
	companion, err := s.readinessCompanion(ctx)
	if err != nil || verifyReadinessCompanion(companion) != nil {
		return ref, connectionError("verified companion unavailable")
	}
	storeAvailable := connectionStoreAvailable(ref.NodeID, metadata.Store, s.options.NodeConnectionOwner != nil)
	binding := connectionBinding(metadata)
	parent := filepath.Dir(s.connectionPath(ref))
	if err := os.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return ref, connectionError("journal unavailable")
	}
	if CheckPrivateDirectory(parent) != nil {
		return ref, connectionError("journal unavailable")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) >= MaxOperations {
		return ref, connectionError("journal unavailable")
	}
	for _, entry := range entries {
		var prior nodeConnectionRecord
		if !entry.Type().IsRegular() || readPrivateJSON(filepath.Join(parent, entry.Name()), &prior) != nil || prior.Schema != 1 || !validConnectionRef(prior.Ref) || prior.Outcome != "closed" {
			return ref, connectionError("original cleanup unconfirmed")
		}
	}
	record := nodeConnectionRecord{Schema: 1, Ref: ref, Guard: guard, Binding: binding, Outcome: "intent"}
	if !storeAvailable {
		// This terminal receipt proves nothing was dispatched. A lost rejection
		// response can be recovered through Close with the original reference.
		record.Outcome, record.CleanupConfirmed, record.Rejected = "closed", true, true
	}
	if writeState(s.connectionPath(ref), record) != nil {
		return ref, connectionError("journal unavailable")
	}
	directory, e := os.Open(s.options.Directory)
	if e != nil {
		return ref, connectionError("journal unavailable")
	}
	e = errors.Join(directory.Sync(), directory.Close())
	if e != nil {
		return ref, connectionError("journal unavailable")
	}
	if record.Rejected {
		return ref, connectionError("begin rejected")
	}
	// Admission now belongs to the native owner, independently of the RPC observer.
	life, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	session := &nodeConnectionSession{record: record, metadata: metadata, flows: map[string]bool{}, cancel: cancel, done: make(chan struct{}), sdk: &caelis.Connections{}}
	if s.connections == nil {
		s.connections = map[string]*nodeConnectionSession{}
	}
	s.connections[ref.OperationID] = session
	// Recheck the exact frozen native binding after the durable admission.
	latest, e := s.ReadOwnedRuntimeSettings(life, ref.NodeID, ref.Backend)
	effectsPossible := false
	if e != nil || latest != metadata || verifyReadinessCompanion(companion) != nil {
		e = connectionError("native binding changed")
	}
	if e == nil && s.options.NodeConnectionOwner != nil {
		effectsPossible = true
		session.owner, e = s.options.NodeConnectionOwner(life, metadata)
	}
	// A trusted hook may prove there is no active Caelis owner by returning
	// nil,nil. Cold setup still uses the same private Store rules and helper.
	if e == nil && session.owner == nil {
		eligible, reason := caelis.ProbeOwnedStore(ref.NodeID, metadata.Store)
		if !eligible && reason == "owned-store-setup-required" {
			// Only an absent designated Store may be initialized. PrepareOwnedStore
			// rejects races, symlinks, shared defaults and every existing Store.
			if _, statErr := os.Lstat(metadata.Store); errors.Is(statErr, os.ErrNotExist) {
				effectsPossible = true
				e = caelis.PrepareOwnedStore(ref.NodeID, metadata.Store)
			} else {
				e = connectionError("private store unavailable")
			}
		} else if !eligible {
			e = connectionError("private store unavailable")
		}
		if e == nil {
			effectsPossible = true
			if s.beginSetup != nil {
				session.owner, e = s.beginSetup(life, caelis.OwnedHostOptions{NodeID: ref.NodeID, Binary: metadata.Binary, Store: metadata.Store, WatchdogHelper: companion.Path})
			} else {
				session.owner, e = caelis.BeginOwnedSetup(life, caelis.OwnedHostOptions{NodeID: ref.NodeID, Binary: metadata.Binary, Store: metadata.Store, WatchdogHelper: companion.Path})
			}
		}
	}
	if e != nil || session.owner == nil {
		cancel()
		session.record.Outcome = "unknown"
		if !effectsPossible {
			session.record.Outcome, session.record.CleanupConfirmed, session.record.Rejected = "closed", true, true
		}
		persistErr := writeState(s.connectionPath(ref), session.record)
		if session.owner != nil {
			go s.closeConnectionSession(session)
		} else {
			close(session.done)
			if session.record.Rejected {
				delete(s.connections, ref.OperationID)
			}
		}
		if session.record.Rejected && persistErr == nil {
			return ref, connectionError("begin rejected")
		}
		return ref, connectionError("begin outcome unconfirmed")
	}
	e = session.owner.Check(life)
	var settings api.RuntimeSettings
	if e == nil {
		settings, e = session.owner.SetupSettings(life)
	}
	if e != nil || settings.Runtime != "caelis" || settings.CLIPath != metadata.Binary || settings.CaelisStore != metadata.Store {
		go s.closeConnectionSession(session)
		return ref, connectionError("native binding changed")
	}
	session.record.Outcome = "active"
	if writeState(s.connectionPath(ref), session.record) != nil {
		go s.closeConnectionSession(session)
		return ref, connectionError("original outcome unavailable")
	}
	go func() {
		select {
		case <-life.Done():
		case <-session.owner.Done():
		}
		s.closeConnectionSession(session)
	}()
	return ref, nil
}

func (s *Service) closeConnectionSession(session *nodeConnectionSession) {
	session.mu.Lock()
	if session.closing {
		session.mu.Unlock()
		return
	}
	session.closing = true
	session.mu.Unlock()
	session.sdk.Close()
	stop, finish := context.WithTimeout(context.Background(), 8*time.Second)
	err := session.owner.Close(stop)
	finish()
	session.cancel()
	session.mu.Lock()
	session.record.Outcome = "unknown"
	if err == nil {
		session.record.Outcome = "closed"
		session.record.CleanupConfirmed = true
	}
	session.mu.Unlock()
	s.connectionsMu.Lock()
	if writeState(s.connectionPath(session.record.Ref), session.record) != nil {
		err = connectionError("cleanup receipt unconfirmed")
	}
	// Retain only the sanitized durable outcome after cleanup. Dropping the
	// session releases transient SDK authorization challenges and flow data.
	delete(s.connections, session.record.Ref.OperationID)
	s.connectionsMu.Unlock()
	session.mu.Lock()
	if err != nil {
		session.closeErr = connectionError("cleanup unconfirmed")
	}
	close(session.done)
	session.mu.Unlock()
}
func (s *Service) connectionSession(ctx context.Context, ref api.NodeRuntimeConnectionRef) (*nodeConnectionSession, api.RuntimeSettings, error) {
	if !validConnectionRef(ref) || ref.NodeID != s.options.NodeID {
		return nil, api.RuntimeSettings{}, connectionError("scope changed")
	}
	s.connectionsMu.Lock()
	session := s.connections[ref.OperationID]
	s.connectionsMu.Unlock()
	if session == nil || session.record.Ref != ref {
		return nil, api.RuntimeSettings{}, connectionError("original interaction unavailable")
	}
	session.mu.Lock()
	active := !session.closing && session.record.Outcome == "active"
	session.mu.Unlock()
	if !active {
		return nil, api.RuntimeSettings{}, connectionError("original interaction unavailable")
	}
	current, err := s.ReadOwnedRuntimeSettings(ctx, ref.NodeID, ref.Backend)
	if err != nil || current != session.metadata {
		return nil, api.RuntimeSettings{}, connectionError("native binding changed")
	}
	if session.owner.Check(ctx) != nil {
		return nil, api.RuntimeSettings{}, connectionError("native owner unavailable")
	}
	settings, err := session.owner.SetupSettings(ctx)
	if err != nil || settings.Runtime != "caelis" || settings.CLIPath != current.Binary || settings.CaelisStore != current.Store {
		return nil, api.RuntimeSettings{}, connectionError("native owner unavailable")
	}
	return session, settings, nil
}
func (s *Service) NodeRuntimeConnectionCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, kind string) (api.RuntimeConnectionCatalog, error) {
	if !validConnectionRef(ref) || ref.NodeID != s.options.NodeID {
		return api.RuntimeConnectionCatalog{}, connectionError("scope changed")
	}
	if port, delegated, err := s.managedNodeConnections(ctx, ref.NodeID, ref.Backend); delegated {
		if err != nil {
			return api.RuntimeConnectionCatalog{}, err
		}
		defer closeManagedNodeConnectionController(port)
		return port.NodeRuntimeConnectionCatalog(ctx, ref, kind)
	}
	if kind != "account" && kind != "api-key" && kind != "agent" {
		return api.RuntimeConnectionCatalog{}, connectionError("catalog unavailable")
	}
	_, settings, err := s.connectionSession(ctx, ref)
	if err != nil {
		return api.RuntimeConnectionCatalog{}, err
	}
	result, err := caelis.ConnectionCatalog(ctx, settings, kind)
	if err != nil {
		return api.RuntimeConnectionCatalog{}, connectionError("catalog unavailable")
	}
	// Native SDK failure strings may contain private response data.
	if result.Unavailable != "" {
		result.Unavailable = "node-connection-unavailable"
	}
	return result, nil
}
func (s *Service) NodeRuntimeSetupCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, action, provider, baseURL string) ([]api.SetupChoice, error) {
	if !validConnectionRef(ref) || ref.NodeID != s.options.NodeID {
		return nil, connectionError("scope changed")
	}
	if port, delegated, err := s.managedNodeConnections(ctx, ref.NodeID, ref.Backend); delegated {
		if err != nil {
			return nil, err
		}
		defer closeManagedNodeConnectionController(port)
		return port.NodeRuntimeSetupCatalog(ctx, ref, action, provider, baseURL)
	}
	if action != "providers" && action != "endpoints" && action != "models" || len(provider) > 512 || len(baseURL) > 4096 {
		return nil, connectionError("catalog unavailable")
	}
	_, settings, err := s.connectionSession(ctx, ref)
	if err != nil {
		return nil, err
	}
	out, err := caelis.SetupCatalog(ctx, api.SetupRequest{Settings: settings, Action: action, Provider: provider, BaseURL: baseURL})
	if err != nil {
		return nil, connectionError("catalog unavailable")
	}
	return out, nil
}
func safeConnectionFlow(flow api.RuntimeFlow) api.RuntimeFlow {
	// Public candidates and transient authorization UI are intentionally returned;
	// SDK error text is never returned or persisted through the node facade.
	// Runtime installation metadata is native SDK presentation only; the fixed
	// target Runtime settings never come from this user input or SDK projection.
	if flow.Stage == "failed" {
		flow.Message = "node-connection-failed"
	}
	if flow.Stage == "unknown" {
		flow.Message = "node-connection-outcome-unknown"
	}
	return flow
}
func (s *Service) StartNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, input api.RuntimeConnectionInput) (api.RuntimeFlow, error) {
	if !validConnectionRef(ref) || ref.NodeID != s.options.NodeID {
		return api.RuntimeFlow{}, connectionError("scope changed")
	}
	if port, delegated, err := s.managedNodeConnections(ctx, ref.NodeID, ref.Backend); delegated {
		if err != nil {
			return api.RuntimeFlow{}, err
		}
		defer closeManagedNodeConnectionController(port)
		return port.StartNodeRuntimeConnection(ctx, ref, input)
	}
	if input.Settings != nil || input.Kind != "account" && input.Kind != "api-key" && input.Kind != "agent" || len(input.APIKey) > 16384 || len(input.BaseURL) > 4096 || len(input.Choice) > 512 || len(input.Model) > 512 || len(input.Command) > 4096 {
		return api.RuntimeFlow{}, connectionError("input unavailable")
	}
	session, settings, err := s.connectionSession(ctx, ref)
	if err != nil {
		return api.RuntimeFlow{}, err
	}
	s.connectionsMu.Lock()
	session.mu.Lock()
	if session.started {
		id := session.flowID
		session.mu.Unlock()
		s.connectionsMu.Unlock()
		if id == "" {
			return api.RuntimeFlow{}, connectionError("original start outcome unconfirmed")
		}
		flow, err := session.sdk.Advance(ctx, api.RuntimeFlowAction{ID: id, Action: "refresh"})
		if err != nil {
			return api.RuntimeFlow{}, connectionError("original flow unavailable")
		}
		return safeConnectionFlow(flow), nil
	}
	if session.closing {
		session.mu.Unlock()
		s.connectionsMu.Unlock()
		return api.RuntimeFlow{}, connectionError("original interaction unavailable")
	}
	session.started = true
	session.record.FlowDispatched = true
	record := session.record
	session.mu.Unlock()
	// Only a public dispatch fact is durable. Secret inputs and raw SDK flows
	// never enter this journal, including on response loss or agent restart.
	journalErr := writeState(s.connectionPath(ref), record)
	s.connectionsMu.Unlock()
	if journalErr != nil {
		return api.RuntimeFlow{}, connectionError("original start intent unavailable")
	}
	flow, err := session.sdk.Start(ctx, settings, input)
	if err != nil {
		return api.RuntimeFlow{}, connectionSDKError("start outcome unavailable", err)
	}
	session.mu.Lock()
	session.flows[flow.ID] = true
	session.flowID = flow.ID
	session.mu.Unlock()
	return safeConnectionFlow(flow), nil
}
func (s *Service) connectionFlow(ctx context.Context, ref api.NodeRuntimeConnectionRef, id string) (*nodeConnectionSession, error) {
	if len(id) > 256 || strings.ContainsAny(id, "\x00\r\n") {
		return nil, connectionError("flow scope changed")
	}
	session, _, err := s.connectionSession(ctx, ref)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	exists := session.flows[id]
	session.mu.Unlock()
	if !exists {
		return nil, connectionError("flow scope changed")
	}
	return session, nil
}
func (s *Service) AdvanceNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, action api.RuntimeFlowAction) (api.RuntimeFlow, error) {
	if !validConnectionRef(ref) || ref.NodeID != s.options.NodeID {
		return api.RuntimeFlow{}, connectionError("scope changed")
	}
	if port, delegated, err := s.managedNodeConnections(ctx, ref.NodeID, ref.Backend); delegated {
		if err != nil {
			return api.RuntimeFlow{}, err
		}
		defer closeManagedNodeConnectionController(port)
		return port.AdvanceNodeRuntimeConnection(ctx, ref, action)
	}
	session, err := s.connectionFlow(ctx, ref, action.ID)
	if err != nil {
		return api.RuntimeFlow{}, err
	}
	flow, err := session.sdk.Advance(ctx, action)
	if err != nil {
		return api.RuntimeFlow{}, connectionSDKError("advance outcome unavailable", err)
	}
	return safeConnectionFlow(flow), nil
}
func (s *Service) WaitNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, id string, after int) (api.RuntimeFlow, error) {
	if !validConnectionRef(ref) || ref.NodeID != s.options.NodeID {
		return api.RuntimeFlow{}, connectionError("scope changed")
	}
	if port, delegated, err := s.managedNodeConnections(ctx, ref.NodeID, ref.Backend); delegated {
		if err != nil {
			return api.RuntimeFlow{}, err
		}
		defer closeManagedNodeConnectionController(port)
		return port.WaitNodeRuntimeConnection(ctx, ref, id, after)
	}
	if after < 0 {
		return api.RuntimeFlow{}, connectionError("flow scope changed")
	}
	session, err := s.connectionFlow(ctx, ref, id)
	if err != nil {
		return api.RuntimeFlow{}, err
	}
	flow, err := session.sdk.Wait(ctx, id, after)
	if err != nil {
		return api.RuntimeFlow{}, connectionSDKError("wait unavailable", err)
	}
	return safeConnectionFlow(flow), nil
}
func (s *Service) CancelNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, id string) error {
	if !validConnectionRef(ref) || ref.NodeID != s.options.NodeID {
		return connectionError("scope changed")
	}
	if port, delegated, err := s.managedNodeConnections(ctx, ref.NodeID, ref.Backend); delegated {
		if err != nil {
			return err
		}
		defer closeManagedNodeConnectionController(port)
		return port.CancelNodeRuntimeConnection(ctx, ref, id)
	}
	session, err := s.connectionFlow(ctx, ref, id)
	if err != nil {
		return err
	}
	if session.sdk.Cancel(ctx, id) != nil {
		return connectionError("cancel outcome unavailable")
	}
	return nil
}
func (s *Service) CloseNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef) error {
	if !validConnectionRef(ref) || ref.NodeID != s.options.NodeID {
		return connectionError("scope changed")
	}
	if port, delegated, err := s.managedNodeConnections(ctx, ref.NodeID, ref.Backend); delegated {
		if err != nil {
			return err
		}
		defer closeManagedNodeConnectionController(port)
		return port.CloseNodeRuntimeConnection(ctx, ref)
	}
	s.connectionsMu.Lock()
	record, err := s.connectionRecord(ref)
	session := s.connections[ref.OperationID]
	s.connectionsMu.Unlock()
	if err != nil {
		return connectionError("original cleanup unavailable")
	}
	if record.CleanupConfirmed && record.Outcome == "closed" {
		return nil
	}
	if session == nil || session.owner == nil || session.record.Ref != ref {
		return connectionError("original cleanup unconfirmed")
	}
	go s.closeConnectionSession(session)
	select {
	case <-ctx.Done():
		return connectionError("cleanup unconfirmed")
	case <-session.done:
		session.mu.Lock()
		defer session.mu.Unlock()
		return session.closeErr
	}
}
