//go:build darwin && cgo

package machines

import (
	"crypto/rand"
	"os"
	"testing"
)

func TestNativeKeychainSecretLifecycle(t *testing.T) {
	if os.Getenv("CAELIS_BOT_KEYCHAIN_TEST") != "1" {
		t.Skip("explicit disposable Keychain acceptance only")
	}
	id := "acceptance-" + rand.Text()
	t.Cleanup(func() { _ = deleteSecret(id) })
	for _, secret := range []string{"disposable-canary-one", "disposable-canary-two"} {
		if err := saveSecret(id, secret); err != nil {
			t.Fatal(err)
		}
		if got, err := loadSecret(id); err != nil || got != secret {
			t.Fatal("Keychain did not preserve the write-only secret")
		}
	}
	if err := deleteSecret(id); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSecret(id); err == nil {
		t.Fatal("removed credential remained readable")
	}
}
