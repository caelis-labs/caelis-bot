package app

import (
	"github.com/caelis-labs/caelis-bot/internal/machines"
	"path/filepath"
	"testing"
)

// Replacing a test adapter must rewire the same machine router used by product
// observation and interactions, not only Application's local owner pointer.
func wireTestWorkers(t *testing.T, a *Application, pool *localWorkers) {
	t.Helper()
	a.localWork = pool
	router, err := machines.Open(filepath.Join(a.root, "Machines"), pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.machines = router
	a.Backend.ConfigureMachines(router)
	a.Backend.ConfigureWorkerInteractions(router)
}
