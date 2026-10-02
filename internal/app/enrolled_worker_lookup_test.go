package app

import "testing"

// Composition coverage remains available on every supported compile target.
// The opt-in native shell/owner fixture has its own Unix platform boundary.
func TestDefaultConstructorsAssembleEnrolledWorkerLookup(t *testing.T) {
	for _, owned := range []bool{false, true} {
		root := t.TempDir()
		var a *Application
		var err error
		if owned {
			a, err = NewOwnedResident(t.Context(), root, Host{}, "fixture-owner", "/fixture/caelis-node")
		} else {
			a, err = New(root, Host{})
		}
		if err != nil {
			t.Fatal(err)
		}
		if a.registeredWorkers == nil || a.started {
			t.Fatal("ordinary composition lacks dormant native lookup")
		}
		if err = a.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
