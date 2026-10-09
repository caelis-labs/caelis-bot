package desktop

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/app"
)

func TestNativePluginActionInstallsWithoutConnectingRuntime(t *testing.T) {
	assembly, err := newProductAssembly(t.TempDir(), []string{"en"}, func(*Service) app.Host { return app.Host{} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = assembly.Core.Close() })
	result, err := assembly.Service.PluginAction("github", "install")
	if err != nil {
		t.Fatal("native plugin action required a connected Runtime", err)
	}
	readback, err := assembly.Service.Plugins()
	if err != nil || readback.Revision != result.Revision {
		t.Fatal("native readback did not confirm the package", err)
	}
	for _, item := range readback.Items {
		if item.ID != "github" {
			continue
		}
		if !item.Installed || !item.Enabled || item.Connection == nil || item.Connection.Stored {
			t.Fatal("install and activation were conflated", item)
		}
		return
	}
	t.Fatal("reviewed package missing from native snapshot")
}
