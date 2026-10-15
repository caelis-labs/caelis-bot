package caelisruntime

import (
	"context"
	"strings"
	"testing"
)

func TestReleaseChecksumRequiresExactArchiveAndValidSHA256(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, body := range []string{hash + "  wrong.tar.gz", "bad  expected.tar.gz", strings.Repeat("z", 64) + "  expected.tar.gz"} {
		if _, err := checksumFor([]byte(body), "expected.tar.gz"); err == nil {
			t.Fatal("invalid release accepted")
		}
	}
	if got, err := checksumFor([]byte(hash+"  expected.tar.gz"), "expected.tar.gz"); err != nil || got != hash {
		t.Fatal(got, err)
	}
}
func TestInstallProgressPreservesMeasuredFacts(t *testing.T) {
	want := InstallProgress{Phase: "downloading", Downloaded: 512, Total: 1024, BytesPerSecond: 128}
	var got InstallProgress
	ctx := WithInstallProgress(context.Background(), func(p InstallProgress) { got = p })
	ReportInstallProgress(ctx, want)
	if got != want {
		t.Fatal(got)
	}
	ReportInstallProgress(context.Background(), want)
}
