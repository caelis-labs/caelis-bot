//go:build !windows

package caelisruntime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoveryUsesPublicStatusAndStartOnlyForMissingOriginalStore(t *testing.T) {
	for _, tc := range []struct {
		name, status    string
		fail, wantStart bool
		want            error
	}{
		{name: "running", status: `{"state":"running"}`},
		{name: "starting", status: `{"state":"starting"}`, want: ErrServiceStateUnknown},
		{name: "unknown", status: `{}`, want: ErrServiceStateUnknown},
		{name: "malformed", status: `PRIVATE_OUTPUT`, want: ErrServiceStateUnknown},
		{name: "stopped", status: `{"state":"stopped"}`, wantStart: true},
		{name: "failed start", status: `{"state":"stopped"}`, fail: true, wantStart: true, want: ErrServiceStartFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin, log := filepath.Join(dir, "caelis"), filepath.Join(dir, "calls")
			t.Setenv("RECOVERY_LOG", log)
			t.Setenv("RECOVERY_STATUS", tc.status)
			t.Setenv("CAELIS_CONTROL_TOKEN", "PRIVATE_TOKEN")
			code := "#!/bin/sh\n[ -z \"$CAELIS_CONTROL_TOKEN\" ] || exit 92\nprintf '%s\\n' \"$*\" >> \"$RECOVERY_LOG\"\ncase \"$1 $2\" in\n'service status') printf '%s\\n' \"$RECOVERY_STATUS\";;\n'service start') "
			if tc.fail {
				code += "echo PRIVATE_START_ERROR >&2; exit 1"
			} else {
				code += "printf '{\"state\":\"running\",\"distribution_version\":\"new-selected-version\"}\\n'"
			}
			code += ";;\n*) exit 93;;\nesac\n"
			if err := os.WriteFile(bin, []byte(code), 0700); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if err := Recover(t.Context(), bin, dir); !errors.Is(err, tc.want) || err != nil && strings.Contains(err.Error(), "PRIVATE") {
					t.Fatal(err)
				}
			}
			b, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			want := "service status --store-dir " + dir + " --format json\n"
			if tc.wantStart {
				want += "service start --store-dir " + dir + " --format json\n"
			}
			if string(b) != want {
				t.Fatalf("wrong store, command, or repeated start: %q", b)
			}
		})
	}
}
