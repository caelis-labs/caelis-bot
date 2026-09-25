package taskterminal

import (
	"reflect"
	"strings"
	"testing"
)

func TestTerminalArgumentsKeepThePrivateScriptAsOneArgument(t *testing.T) {
	path := "/tmp/folder with ' quotes/$(unexpected).command"
	for _, pref := range []string{"system", "terminal", "iterm2", "ghostty"} {
		args, e := OpenArgs(pref, path)
		if e != nil || args[len(args)-1] != path {
			t.Fatal(pref, args, e)
		}
		for _, a := range args {
			if strings.Contains(a, " -c ") {
				t.Fatal("shell command interpolation")
			}
		}
	}
	args, _ := OpenArgs("iterm2", path)
	if !reflect.DeepEqual(args, []string{"-b", "com.googlecode.iterm2", path}) {
		t.Fatal(args)
	}
	for _, pref := range []string{"bad", "Terminal; touch bad"} {
		if _, e := OpenArgs(pref, path); e == nil {
			t.Fatal("unsupported adapter")
		}
	}
	if _, e := OpenArgs("system", "relative"); e == nil {
		t.Fatal("relative path accepted")
	}
	choices := Choices(func(id string) bool { return id == BundleID("terminal") })
	if !choices[0].Available || !choices[1].Available || choices[2].Available || choices[3].Available {
		t.Fatal(choices)
	}
}
