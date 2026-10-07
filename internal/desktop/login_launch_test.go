package desktop

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/app"
)

func TestLoginLaunchKeepsFirstRunSettingsClosed(t *testing.T) {
	for _, atLogin := range []bool{false, true} {
		root := t.TempDir()
		assembly, err := newProductAssembly(root, []string{"en"}, func(*Service) app.Host { return app.Host{} })
		if err != nil {
			t.Fatal(err)
		}
		if !assembly.Core.NeedsSetup() {
			t.Fatal("fresh profile did not require setup")
		}
		if err := assembly.StartForLaunch(atLogin); err != nil {
			t.Fatal(err)
		}
		if got := assembly.Service.settingsSection; atLogin && got != "" || !atLogin && got != "setup" {
			t.Fatalf("atLogin=%t opened %q", atLogin, got)
		}
		if err := assembly.Core.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
