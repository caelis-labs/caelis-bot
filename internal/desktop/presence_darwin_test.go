//go:build darwin && cgo

package desktop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Compile the production Objective-C serializer, rather than a Go JSON fixture.
// Synthetic OS observations keep this independent of the user's lock state and
// avoid querying the active desktop or touching the running product.
func TestNativePresenceJSONBoundary(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.m")
	if err := os.WriteFile(probe, []byte(`#include "presence_darwin.h"
#include <stdio.h>
#include <stdlib.h>
int main(void) {
 int cases[][5]={{1,1,0,1,1},{1,1,1,1,1},{1,0,0,1,1},{1,1,0,0,1},{0,1,0,1,1}};
 for(int i=0;i<5;i++) { int *v=cases[i]; char *p=bot_presence_encode(v[0],v[1],v[2],v[3],v[4],12.5,"fixture.application"); if(!p)return 1; puts(p);free(p); }
}`), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "probe")
	cmd := exec.Command("clang", "-fobjc-arc", "-I.", "-framework", "Cocoa", "-framework", "CoreGraphics", "-framework", "IOKit", "presence_darwin.m", probe, "-o", binary)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native compile: %v\n%s", err, out)
	}
	raw, err := exec.Command(binary).Output()
	if err != nil {
		t.Fatal(err)
	}
	var values []json.RawMessage
	// The line stream contains the actual NSJSONSerialization bytes.
	for _, line := range splitJSONLines(raw) {
		values = append(values, line)
	}
	if len(values) != 5 {
		t.Fatalf("native cases: %d", len(values))
	}
	d := presenceSampleDecoder{}
	want := []string{"true", "false", "null", "false", "true"}
	for i, raw := range values {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["Unlocked"]) != want[i] {
			t.Fatalf("case %d JSON type: %s", i, fields["Unlocked"])
		}
		s := d.decode(raw)
		if s.IdleSeconds != 12.5 || s.Application != "fixture.application" {
			t.Fatalf("case %d lost sample", i)
		}
		if (s.Unlocked == nil) != (i == 2) || s.Available() != (i == 0) {
			t.Fatalf("case %d wrong presence: %+v", i, s.Presence)
		}
	}
}
