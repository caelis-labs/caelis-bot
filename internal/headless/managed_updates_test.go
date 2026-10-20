package headless

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagedAdmissionAllowsOpenSSEButDrainsWrites(t *testing.T) {
	h := &managedHost{ready: true}
	for _, method := range []string{"GET", "POST"} {
		entered, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		handler := h.handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-finish }))
		go func() {
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "http://localhost/events", nil))
			close(done)
		}()
		<-entered
		locked := h.gate.TryLock()
		if locked {
			h.gate.Unlock()
		}
		close(finish)
		<-done
		if locked != (method == "GET") {
			t.Fatal("wrong update admission", method)
		}
	}
	h.frozen = true
	w := httptest.NewRecorder()
	h.handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("frozen write reached service") })).ServeHTTP(w, httptest.NewRequest("POST", "http://localhost/command", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
