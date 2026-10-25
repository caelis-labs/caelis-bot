package workerpeer

import (
	"regexp"
	"testing"
)

func TestAddMachineCodeExchangeBudgetAndSuspendedGrant(t *testing.T) {
	s, h, _ := peerFixture(t)
	original := pairFixture(t, s, h, "original-source")
	if err := s.Enable(false); err != nil {
		t.Fatal(err)
	}
	code, err := s.BeginPairing(t.Context())
	if err != nil || !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code.Code) {
		t.Fatal(code, err)
	}
	if _, err := Call(t.Context(), original, Request{Action: "info"}); err == nil || err.Error() != "peer_authorization_revoked" {
		t.Fatal("suspended grant revived", err)
	}
	wrong := "000000"
	if code.Code == wrong {
		wrong = "000001"
	}
	for range 5 {
		if _, err := Pair(t.Context(), h.URL, wrong, "new-source"); err == nil {
			t.Fatal("wrong code accepted")
		}
	}
	if _, err := Pair(t.Context(), h.URL, code.Code, "new-source"); err == nil {
		t.Fatal("exhausted code accepted")
	}
	if len(s.Status().Sources) != 0 {
		t.Fatal("guessing created authorization")
	}
	code, err = s.BeginPairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Original proof, source and task binding remain the recovery authority.
	restored, err := Pair(t.Context(), h.URL, code.Code, "original-source", original.Token)
	if err != nil || restored.Token != original.Token {
		t.Fatal("original proof lost", err)
	}
	if _, err = Pair(t.Context(), h.URL, code.Code, "new-source"); err == nil {
		t.Fatal("single-use code reused")
	}
	if _, err = Call(t.Context(), restored.Connection, Request{Action: "info"}); err != nil {
		t.Fatal(err)
	}
}
