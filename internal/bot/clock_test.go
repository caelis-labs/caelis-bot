package bot

import (
	"testing"
	"time"
)

func TestClockDoesNotAdvertiseLocalOrAmbiguousZone(t *testing.T) {
	if got := clockZone(time.UTC); got != "UTC" {
		t.Fatal(got)
	}
	if got := clockZone(time.FixedZone("Synthetic/Unknown", 3600)); got != "" {
		t.Fatal("invented a named zone", got)
	}
	r, _, _ := fixture(t)
	clock := r.Clock()
	if clock["timeZone"] != "UTC" || clock["timeZoneStatus"] != "known" || clock["utcOffset"] != "+00:00" {
		t.Fatal(clock)
	}
}

func TestClockExplicitUnknownOverrideDoesNotGuessSystemZone(t *testing.T) {
	t.Setenv("TZ", "Synthetic/Unknown")
	if got := clockZone(time.FixedZone("Local", 0)); got != "" {
		t.Fatal("unknown process timezone replaced by system zone", got)
	}
	t.Setenv("TZ", "Asia/Shanghai")
	if got := clockZone(time.FixedZone("Local", 8*3600)); got != "Asia/Shanghai" {
		t.Fatal(got)
	}
}
