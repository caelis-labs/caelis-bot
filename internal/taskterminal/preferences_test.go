package taskterminal

import "testing"

func TestTerminalChoicesOnlyEnableInstalledApplications(t *testing.T) {
	choices := Choices(func(id string) bool { return id == BundleID("terminal") })
	if !choices[0].Available || !choices[1].Available || choices[2].Available || choices[3].Available {
		t.Fatal(choices)
	}
	for _, pref := range []string{"terminal", "iterm2", "ghostty"} {
		if PreferenceForBundle(BundleID(pref)) != pref {
			t.Fatal("terminal preference did not round trip", pref)
		}
	}
	if BundleID("custom") != "" || PreferenceForBundle("example.unknown") != "" {
		t.Fatal("unknown application was assigned a built-in preference")
	}
}
