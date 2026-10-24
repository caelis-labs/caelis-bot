package websurface

import (
	"net/http"

	"github.com/caelis-labs/caelis-bot/internal/managedupdate"
)

// RegisterManagedUpdates adds a fixed, typed surface. ServeHTTP still owns
// cookie/Origin/CSRF checks. The launcher owns durable update receipts, allowing
// a read after the Bot process that accepted the HTTP request has exited.
func (s *Server) RegisterManagedUpdates(owner managedupdate.Owner) {
	s.mux.HandleFunc("GET /api/v1/updates/status", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, owner.Status())
	})
	s.mux.HandleFunc("GET /api/v1/updates/receipts/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := owner.Receipt(r.PathValue("id"))
		if e != nil {
			fail(w, 409, managedupdate.Code(e))
			return
		}
		jsonOut(w, 200, v)
	})
	s.mux.HandleFunc("POST /api/v1/updates/command", func(w http.ResponseWriter, r *http.Request) {
		var c managedupdate.Command
		if decode(w, r, &c) != nil {
			fail(w, 400, "invalid_request")
			return
		}
		v, e := owner.Command(c)
		if e != nil {
			fail(w, 409, managedupdate.Code(e))
			return
		}
		jsonOut(w, 200, v)
	})
}

// UpdateQuiescent is checked while the headless HTTP admission gate excludes all
// incoming requests. Unknown accepted writes are not cleared to permit restart.
func (s *Server) UpdateQuiescent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, op := range s.operations {
		op, err := s.reconcileOperation(id, op)
		if err != nil || !operationTerminal(op) && !s.botAuthorizationAllowsRestartLocked(id, op) {
			return false
		}
	}
	return true
}
