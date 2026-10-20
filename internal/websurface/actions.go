package websurface

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type operation struct {
	Digest string          `json:"digest"`
	Owner  string          `json:"owner"`
	State  string          `json:"state"`
	Value  json.RawMessage `json:"value,omitempty"`
	Error  string          `json:"error,omitempty"`
	At     int64           `json:"at"`
}
type actionRequest struct {
	ID   string            `json:"id"`
	Args []json.RawMessage `json:"args"`
}
type action struct {
	Path                  string
	Function              any
	Read, Composer, Local bool
}

// bind only decodes the signature of a function explicitly listed in routes.go.
// There is no service name, reflection method lookup, arbitrary Go call, Core
// route or shell command supplied by an HTTP client.
func bind(function any) (func(context.Context, []json.RawMessage) (any, error), error) {
	f := reflect.ValueOf(function)
	t := f.Type()
	if t.Kind() != reflect.Func {
		return nil, errors.New("invalid action")
	}
	ctxType := reflect.TypeFor[context.Context]()
	errType := reflect.TypeFor[error]()
	return func(ctx context.Context, raw []json.RawMessage) (any, error) {
		args := make([]reflect.Value, t.NumIn())
		offset := 0
		if t.NumIn() > 0 && t.In(0) == ctxType {
			args[0] = reflect.ValueOf(ctx)
			offset = 1
		}
		if len(raw) != t.NumIn()-offset {
			return nil, errors.New("invalid argument count")
		}
		for i, b := range raw {
			v := reflect.New(t.In(i + offset))
			d := json.NewDecoder(bytes.NewReader(b))
			d.DisallowUnknownFields()
			if e := d.Decode(v.Interface()); e != nil {
				return nil, errors.New("invalid argument")
			}
			args[i+offset] = v.Elem()
		}
		out := f.Call(args)
		if len(out) > 0 && out[len(out)-1].Type() == errType {
			e := out[len(out)-1]
			out = out[:len(out)-1]
			if !e.IsNil() {
				return nil, e.Interface().(error)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out[0].Interface(), nil
	}, nil
}

func (s *Server) register(a action) {
	invoke, err := bind(a.Function)
	if err != nil {
		panic(err)
	}
	s.mux.HandleFunc("POST "+a.Path, func(w http.ResponseWriter, r *http.Request) {
		var in actionRequest
		if decode(w, r, &in) != nil {
			fail(w, 400, "invalid_request")
			return
		}
		if a.Composer {
			s.composerMu.Lock()
			defer s.composerMu.Unlock()
			if !s.ownsComposer(r) {
				fail(w, 409, "composer_owned_elsewhere")
				return
			}
		}
		if a.Read || a.Local {
			value, e := invoke(api.WithSetupAccess(r.Context(), "web:"+sessionID(r)), in.Args)
			if e != nil {
				fail(w, 503, "read_unavailable")
				return
			}
			jsonOut(w, 200, map[string]any{"value": value})
			return
		}
		if len(in.ID) < 16 || len(in.ID) > 128 {
			fail(w, 400, "request_id_required")
			return
		}
		body, _ := json.Marshal(in.Args)
		fingerprint := digest(a.Path + string(body))
		owner := sessionID(r)
		s.mu.Lock()
		if old, exists := s.operations[in.ID]; exists {
			s.mu.Unlock()
			if old.Owner != owner || old.Digest != fingerprint {
				fail(w, 409, "request_conflict")
				return
			}
			if old.State != "complete" {
				fail(w, 409, "operation_unconfirmed")
				return
			}
			if old.Error != "" {
				fail(w, 409, old.Error)
				return
			}
			jsonOut(w, 200, map[string]any{"value": old.Value})
			return
		}
		// Retain identity tombstones. Expiry must never authorize replay. Local
		// draft CAS is handled by its existing owner, not this effect journal.
		if len(s.operations) > 4096 {
			s.mu.Unlock()
			fail(w, 503, "operation_capacity")
			return
		}
		s.operations[in.ID] = operation{Digest: fingerprint, Owner: owner, State: "pending", At: time.Now().Unix()}
		if e := s.saveOperations(); e != nil {
			delete(s.operations, in.ID)
			s.mu.Unlock()
			fail(w, 503, "not_dispatched")
			return
		}
		s.mu.Unlock()
		// A browser disconnect does not cancel admitted Bot work. Shutdown and
		// the original owner still govern lifetime; no automatic effect retry.
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
		defer cancel()
		value, e := invoke(api.WithSetupAccess(ctx, "web:"+owner), in.Args)
		result, _ := json.Marshal(receiptValue(value))
		s.mu.Lock()
		pending := s.operations[in.ID]
		record := pending
		record.State = "complete"
		record.Value = result
		if e != nil {
			record.Error = "operation_unconfirmed"
			var blocked *api.SetupBlockedError
			if errors.As(e, &blocked) && (blocked.Code == "runtime_recovery_required" || blocked.Code == "runtime_not_ready") {
				record.Error = "setup_blocked_" + blocked.Code
			}
			record.Value = nil
		}
		s.operations[in.ID] = record
		persistErr := s.saveOperations()
		if persistErr != nil {
			s.operations[in.ID] = pending
		}
		s.mu.Unlock()
		if e != nil && persistErr == nil {
			fail(w, 409, record.Error)
			return
		}
		if persistErr != nil {
			fail(w, 409, "operation_unconfirmed")
			return
		}
		jsonOut(w, 200, map[string]any{"value": value})
	})
}
func (s *Server) saveOperations() error {
	return localstate.Write(filepath.Join(s.config.Directory, "operations.json"), s.operations)
}

func (s *Server) ownsComposer(r *http.Request) bool {
	return s.writer == sessionID(r)+":"+r.Header.Get("X-Caelis-Tab") && r.Header.Get("X-Caelis-Edit-Epoch") == s.writerEpoch && time.Now().Before(s.writerUntil)
}
func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TakeOver bool `json:"takeOver"`
	}
	if decode(w, r, &in) != nil {
		fail(w, 400, "invalid_request")
		return
	}
	tab := r.Header.Get("X-Caelis-Tab")
	if len(tab) < 16 || len(tab) > 128 {
		fail(w, 400, "invalid_tab")
		return
	}
	s.composerMu.Lock()
	defer s.composerMu.Unlock()
	key := sessionID(r) + ":" + tab
	if s.writer == key || s.writer == "" || time.Now().After(s.writerUntil) || in.TakeOver {
		if s.writer != key || time.Now().After(s.writerUntil) {
			s.writerEpoch = token()
		}
		s.writer = key
		s.writerUntil = time.Now().Add(30 * time.Second)
	}
	epoch := ""
	if s.writer == key {
		epoch = s.writerEpoch
	}
	jsonOut(w, 200, map[string]any{"editing": s.writer == key, "epoch": epoch})
}

// A completed receipt proves dispatch; challenge bytes are read from the live
// owner with InspectSetup. Do not persist codes or replay expired challenges.
func receiptValue(value any) any {
	if state, ok := value.(api.SetupState); ok {
		state.Login = nil
		return state
	}
	return value
}
