//go:build darwin && cgo && keychainfixture

package machines

import (
	"crypto/rand"
	"os"
	"os/exec"
	"testing"
)

func TestNativeLegacyCredentialCleanupIsAccountScoped(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_KEYCHAIN") != "1" {
		t.Skip("opt-in isolated Keychain accounts")
	}
	id := "delete-fixture-" + rand.Text()
	other := id + "-other"
	for _, account := range []string{id, other} {
		if err := saveLegacyCredentialFixture(account); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			DeleteLegacyCredential(account)
		})
	}
	if err := DeleteLegacyCredential(id); err != nil {
		t.Fatal(err)
	}
	if err := DeleteLegacyCredential(id); err != nil {
		t.Fatal("not idempotent", err)
	}
	if exec.Command("security", "find-generic-password", "-s", "dev.caelis.bot.ssh", "-a", id).Run() == nil {
		t.Fatal("owned credential retained")
	}
	if err := exec.Command("security", "find-generic-password", "-s", "dev.caelis.bot.ssh", "-a", other).Run(); err != nil {
		t.Fatal("unrelated account removed", err)
	}
}
