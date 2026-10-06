//go:build darwin && cgo

package desktop

import "testing"

func TestCaptureSmokeReadsProjectedFacts(t *testing.T) {
	var observed captureSmokeObservation
	err := decodeCaptureSmokeResult(map[string]any{"result": map[string]any{
		"objects":  []any{map[string]any{"ref": "window-1", "kind": "window", "app": "app-1", "name": map[string]any{"known": "Disposable Window"}}},
		"coverage": map[string]any{"complete": true},
	}}, &observed)
	if err != nil || !observed.Coverage.Complete || len(observed.Objects) != 1 || observed.Objects[0].Name.Known != "Disposable Window" {
		t.Fatalf("projected observation: %+v, %v", observed, err)
	}
}
