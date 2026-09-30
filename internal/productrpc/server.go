package productrpc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

// Options is native-only configuration. Token must come from private OS storage,
// never command-line arguments, renderer configuration, model tools, or logs.
type Options struct {
	Context                    context.Context
	NodeID, BotID, JournalFile string
	Token                      string `json:"-"`
	Capabilities               Capabilities
	Resources                  Resources
	// OnStopped belongs to the native process owner. It runs after the stop
	// receipt publication and response observation boundary, never on detach.
	OnStopped func(Result)
	// Management is constructed once from the inspected native service scope.
	// It receives no wire-supplied directory, credential or connection options.
	Management func(productmanagement.Scope) (productmanagement.Port, error)
}

type Server struct {
	port       Port
	management productmanagement.Port
	opts       Options
	identity   Identity
	projection projection
	journal    *journal
	commands   sync.Mutex
	stateMu    sync.Mutex
	revision   uint64
	changed    chan struct{}
	stopping   bool
	uploadMu   sync.Mutex
	uploads    map[string]string
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func NewServer(port Port, opts Options) (*Server, error) {
	if port == nil || !identifier.MatchString(opts.NodeID) || !identifier.MatchString(opts.BotID) || len(opts.Token) < 32 || len(opts.Token) > 256 || opts.JournalFile == "" {
		return nil, errors.New("invalid native product service configuration")
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	j, err := openJournal(opts.JournalFile, opts.BotID)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	s := &Server{port: port, opts: opts, journal: j, projection: projection{key: key}, changed: make(chan struct{}), revision: 1, uploads: make(map[string]string)}
	s.identity = Identity{Version: ProtocolVersion, NodeID: opts.NodeID, Scope: Scope{BotID: opts.BotID, Generation: rand.Text()}, Capabilities: opts.Capabilities}
	if opts.Resources == nil {
		s.identity.Capabilities.Files = false
	}
	s.identity.Capabilities.RuntimeManagement = false
	if opts.Management != nil {
		s.management, err = opts.Management(managementScope(s.identity.Scope))
		if err != nil {
			return nil, err
		}
		if s.management != nil {
			caps := s.management.Capabilities()
			s.identity.Capabilities.RuntimeManagement = caps.Installation || caps.Configuration
		}
	}
	return s, nil
}

// NotifySnapshot wakes observers to re-read the composed product Port snapshot.
// A raw engine callback is only a signal, never the authoritative wire snapshot.
func (s *Server) NotifySnapshot() {
	s.stateMu.Lock()
	s.revision++
	close(s.changed)
	s.changed = make(chan struct{})
	s.stateMu.Unlock()
}

func (s *Server) Identity() Identity { return s.identity }

// Serve only accepts a node-local listener. SSH forwarding is owned elsewhere.
// Closing a connection/listener detaches observers and never calls StopBot.
func (s *Server) Serve(listener net.Listener) error {
	switch addr := listener.Addr().(type) {
	case *net.TCPAddr:
		if !addr.IP.IsLoopback() {
			return errors.New("product listener must be loopback")
		}
	case *net.UnixAddr:
		// Unix listener creation and private parent permissions belong to the host.
	default:
		return errors.New("unsupported product listener")
	}
	h := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	return h.Serve(listener)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Browser origins cannot use this native-only bearer channel.
	if r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.opts.Token)) != 1 {
		problem(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/identity":
		s.write(w, s.identity)
	case r.Method == "POST" && r.URL.Path == "/v1/state":
		var scope Scope
		if !decode(w, r, &scope) || !s.scope(w, scope) {
			return
		}
		s.state(w, false)
	case r.Method == "POST" && r.URL.Path == "/v1/watch":
		var watch struct {
			Scope
			Cursor Cursor `json:"cursor"`
		}
		if !decode(w, r, &watch) || !s.scope(w, watch.Scope) {
			return
		}
		s.watch(w, r, watch.Cursor)
	case r.Method == "POST" && r.URL.Path == "/v1/commands":
		var command Command
		if !decode(w, r, &command) || !s.scope(w, command.Scope) {
			return
		}
		s.command(w, r, command)
	case r.Method == "POST" && r.URL.Path == "/v1/receipt":
		var query struct {
			Scope
			ID string `json:"id"`
		}
		if !decode(w, r, &query) || !s.scope(w, query.Scope) {
			return
		}
		if !identifier.MatchString(query.ID) {
			problem(w, 400, "invalid-command-id")
			return
		}
		result, found := s.journal.lookup(query.ID)
		if !found {
			result = Result{ID: query.ID, Outcome: "unknown", Code: "receipt-unavailable"}
		}
		s.write(w, result)
	case r.Method == "POST" && managementPath(r.URL.Path):
		s.manageHTTP(w, r)
	case r.Method == "PUT" && r.URL.Path == "/v1/resources":
		s.upload(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/resources":
		s.download(w, r)
	default:
		problem(w, http.StatusNotFound, "unknown-endpoint")
	}
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		problem(w, 415, "json-required")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxCommandBytes))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		problem(w, 400, "invalid-request")
		return false
	}
	return true
}

func problem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{code})
}

func (s *Server) write(w http.ResponseWriter, value any) {
	b, err := json.Marshal(value)
	if err != nil || len(b) > MaxSnapshotBytes {
		problem(w, 500, "response-limit")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	_, _ = w.Write(b)
}

func (s *Server) scope(w http.ResponseWriter, scope Scope) bool {
	if scope != s.identity.Scope {
		problem(w, http.StatusConflict, "service-scope-changed")
		return false
	}
	return true
}

func (s *Server) state(w http.ResponseWriter, reset bool) {
	s.stateMu.Lock()
	cursor := Cursor{Generation: s.identity.Generation, Revision: strconv.FormatUint(s.revision, 10)}
	s.stateMu.Unlock()
	state, err := s.projection.state(s.identity.Scope, cursor, s.port)
	if err != nil {
		problem(w, 500, "snapshot-limit")
		return
	}
	state.Reset = reset
	s.write(w, state)
}

func (s *Server) watch(w http.ResponseWriter, r *http.Request, cursor Cursor) {
	after, err := strconv.ParseUint(cursor.Revision, 10, 64)
	s.stateMu.Lock()
	revision, changed := s.revision, s.changed
	s.stateMu.Unlock()
	if cursor.Generation != s.identity.Generation || err != nil || after > revision {
		s.state(w, true)
		return
	}
	if after == revision {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		if r.Context().Err() != nil {
			return
		}
	}
	s.state(w, false)
}

func validCommand(c Command) bool {
	if !identifier.MatchString(c.ID) {
		return false
	}
	n := 0
	for _, set := range []bool{c.Submission != nil, c.Decision != nil, c.Introduction != nil, c.Draft != nil, c.Turn != "", c.RuntimeManagement != nil, c.Configuration != nil} {
		if set {
			n++
		}
	}
	switch c.Kind {
	case "manage-runtime":
		return n == 1 && c.RuntimeManagement != nil && c.RuntimeManagement.ID == c.ID && c.RuntimeManagement.Scope == managementScope(c.Scope) && validRuntimeManagement(*c.RuntimeManagement, false)
	case "configure-runtime":
		return n == 1 && c.Configuration != nil && c.Configuration.ID == c.ID && c.Configuration.Scope == managementScope(c.Scope) && validConfiguration(c.Configuration.Change)
	case "submit":
		return n == 1 && c.Submission != nil && c.Submission.ID == c.ID && len(c.Submission.Text) <= 256<<10 && validIDs(c.Submission.FileIDs, 8) && validIDs(c.Submission.ReferenceIDs, 64)
	case "decide":
		return n == 1 && c.Decision != nil && validDecision(*c.Decision)
	case "interrupt":
		return n == 1 && identifier.MatchString(c.Turn)
	case "initialize":
		return n == 1 && c.Introduction != nil && len(c.Introduction.Name) <= 256 && len(c.Introduction.Description) <= 16<<10
	case "save-draft":
		return n == 1 && c.Draft != nil && len(c.Draft.Text) <= 256<<10 && validIDs(c.Draft.ReferenceIDs, 64)
	case "retry-introduction", "load-earlier", "stop-bot":
		return n == 0
	}
	return false
}

func validIDs(ids []string, limit int) bool {
	if len(ids) > limit {
		return false
	}
	for _, id := range ids {
		if !identifier.MatchString(id) {
			return false
		}
	}
	return true
}

func validDecision(d ApprovalDecision) bool {
	if !identifier.MatchString(d.ID) || !identifier.MatchString(d.Target) || len(d.Choice) > 256 || len(d.Answers) > 64 {
		return false
	}
	for key, answers := range d.Answers {
		if len(key) == 0 || len(key) > 256 || len(answers) > 128 {
			return false
		}
		for _, answer := range answers {
			if len(answer) > 16<<10 {
				return false
			}
		}
	}
	return true
}

func (s *Server) command(w http.ResponseWriter, r *http.Request, command Command) {
	if !validCommand(command) {
		problem(w, 400, "invalid-command")
		return
	}
	if command.Kind == "save-draft" {
		// Draft is a native revision-CAS replacement, not a business intent.
		// Observation loss is reconciled by reading State; replay with its old
		// revision cannot replace a newer draft, including after server restart.
		s.commands.Lock()
		defer s.commands.Unlock()
		result := s.execute(r.Context(), command)
		s.NotifySnapshot()
		s.write(w, result)
		return
	}
	canonical := command
	canonical.Generation = "" // Restart cannot grant a duplicate dispatch.
	if canonical.RuntimeManagement != nil {
		v := *canonical.RuntimeManagement
		v.Generation = ""
		canonical.RuntimeManagement = &v
	}
	if canonical.Configuration != nil {
		v := *canonical.Configuration
		v.Generation = ""
		canonical.Configuration = &v
	}
	b, _ := json.Marshal(canonical)
	digest := sha256.Sum256(b)
	result, fresh, err := s.journal.reserve(command.ID, hex.EncodeToString(digest[:]), command.RuntimeManagement)
	if err != nil {
		problem(w, 409, "command-conflict-or-journal-unavailable")
		return
	}
	if !fresh {
		s.write(w, result)
		return
	}
	done := make(chan Result, 1)
	observed := make(chan struct{})
	defer close(observed)
	go func() {
		s.commands.Lock()
		defer s.commands.Unlock()
		ctx, cancel := context.WithTimeout(s.opts.Context, 2*time.Minute)
		defer cancel()
		result := s.execute(ctx, command)
		stopped := command.Kind == "stop-bot" && s.stopping
		if s.journal.finish(command.ID, result) != nil {
			result = Result{ID: command.ID, Outcome: "unknown", Code: "receipt-save-failed"}
		}
		s.NotifySnapshot()
		done <- result
		if stopped && s.opts.OnStopped != nil {
			// A lost response already ends observation. A live response gets a
			// bounded chance to flush before its owner releases the listener.
			wait, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			select {
			case <-observed:
			case <-wait.Done():
			}
			s.opts.OnStopped(result)
		}
	}()
	select {
	case result := <-done:
		s.write(w, result)
		if command.Kind == "stop-bot" {
			_ = http.NewResponseController(w).Flush()
		}
	case <-r.Context().Done():
	}
}

func (s *Server) execute(ctx context.Context, c Command) Result {
	r := Result{ID: c.ID, Outcome: "accepted"}
	if s.stopping {
		r.Outcome = "rejected"
		r.Code = "bot-stopping"
		return r
	}
	var err error
	snapshot := s.port.Snapshot()
	switch c.Kind {
	case "manage-runtime", "configure-runtime":
		return s.executeManagement(ctx, c)
	case "submit":
		in := *c.Submission
		in.FileIDs = append([]string(nil), in.FileIDs...)
		s.uploadMu.Lock()
		for i, id := range in.FileIDs {
			native, ok := s.uploads[id]
			if !ok {
				s.uploadMu.Unlock()
				r.Outcome, r.Code = "rejected", "stale-input-file"
				return r
			}
			in.FileIDs[i] = native
		}
		s.uploadMu.Unlock()
		var ok bool
		in.ReferenceIDs, ok = s.projection.references(snapshot, in.ReferenceIDs)
		if !ok {
			r.Outcome = "rejected"
			r.Code = "stale-reference"
			return r
		}
		var receipt api.Receipt
		receipt, err = s.port.Submit(ctx, in)
		r.Submission = &receipt
		if receipt.ID != c.ID || (receipt.Outcome != "accepted" && receipt.Outcome != "rejected" && receipt.Outcome != "unknown") {
			r.Outcome = "unknown"
			r.Code = "invalid-native-receipt"
			return r
		}
		r.Outcome = receipt.Outcome
	case "decide":
		decision, ok := s.projection.decision(snapshot, *c.Decision)
		if !ok {
			r.Outcome = "rejected"
			r.Code = "stale-approval"
			return r
		}
		err = s.port.Decide(ctx, decision)
	case "interrupt":
		if !s.identity.Capabilities.Interrupt {
			r.Outcome = "rejected"
			r.Code = "unavailable"
			return r
		}
		if snapshot.CurrentTurn == "" || s.projection.handle("turn", snapshot.CurrentTurn) != c.Turn {
			r.Outcome = "rejected"
			r.Code = "stale-turn"
			return r
		}
		err = s.port.InterruptTurn(ctx, snapshot.CurrentTurn)
	case "initialize":
		value, e := s.port.InitializeBot(ctx, *c.Introduction)
		r.Initialization = &value
		err = e
	case "retry-introduction":
		value, e := s.port.RetryBotIntroduction(ctx)
		r.Initialization = &value
		err = e
	case "save-draft":
		draft := *c.Draft
		if s.port.Draft().Revision != draft.Revision {
			r.Outcome, r.Code = "rejected", "stale-draft"
			return r
		}
		var ok bool
		draft.ReferenceIDs, ok = s.projection.references(snapshot, draft.ReferenceIDs)
		if !ok {
			r.Outcome = "rejected"
			r.Code = "stale-reference"
			return r
		}
		value, e := s.port.SaveDraft(draft)
		err = e
		for i := range value.ReferenceIDs {
			value.ReferenceIDs[i] = s.projection.handle("reference", value.ReferenceIDs[i])
		}
		r.Draft = &value
	case "load-earlier":
		err = s.port.LoadEarlier(ctx)
	case "stop-bot":
		s.stopping = true
		err = s.port.StopBot(ctx)
	}
	if err != nil {
		if errors.Is(err, ErrUnsupported) {
			r.Outcome = "rejected"
			r.Code = "unavailable"
		} else if c.Kind != "submit" || r.Outcome == "unknown" {
			r.Outcome = "unknown"
			r.Code = "native-outcome-unconfirmed"
		}
	}
	return r
}
