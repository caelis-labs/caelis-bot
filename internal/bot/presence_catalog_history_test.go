package bot

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPresenceChangeRetainsExactRunningReleaseCatalog(t *testing.T) {
	// Hash independently obtained from Runtime.Definitions at e65385fa12852381a08530541ea6665c1fb08b64.
	const original = "cf4d8b1f45dcaa91a9a4d1ad1a3cd50e710fdb65a8a8cf78cc8b25422aa4bf7b"
	r, _, _ := fixture(t)
	raw, _ := json.Marshal((priorPresenceTools{r}).Definitions())
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != original {
		t.Fatalf("historical catalog changed: %s", got)
	}
	current, _ := json.Marshal(r.Definitions())
	if string(current) == string(raw) || !strings.Contains(string(current), "purpose") {
		t.Fatal("new purpose is not discoverable")
	}
}
