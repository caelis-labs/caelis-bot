package runtimeinstall

import (
	"context"
	"crypto/x509"
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"
)

func TestInstallFailureEvidenceIsClassifiedAndPrivate(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{context.DeadlineExceeded, "download_timeout"}, {context.Canceled, "cancelled"},
		{fs.ErrPermission, "installation_permission"}, {syscall.ENOSPC, "storage_unavailable"},
		{x509.UnknownAuthorityError{}, "download_tls"}, {errors.New("https://private/?token=SECRET"), "unclassified"},
		{&Failure{Code: "download_http", HTTPStatus: 503}, "download_http"},
	} {
		f := Classify(tc.err)
		if f.Code != tc.code || strings.Contains(f.Error(), "SECRET") {
			t.Fatal(f)
		}
	}
	for _, tc := range []struct{ stderr, code string }{
		{"curl: failed to connect https://PRIVATE?token=SECRET", "download_network"},
		{"No space left on device PRIVATE_PATH", "storage_unavailable"},
		{"checksum mismatch SECRET", "archive_invalid"},
		{"sha256sum: command not found", "missing_dependency"},
		{"unknown private text SECRET", "installer_exit"},
	} {
		f := classifyInstallerExit(tc.stderr, 7)
		if f.Code != tc.code || f.ExitCode != 7 || strings.Contains(f.Error(), "SECRET") {
			t.Fatal(f)
		}
	}
	output := &boundedOutput{}
	n, e := output.Write([]byte(strings.Repeat("x", 20000)))
	if n != 20000 || e != nil || output.Len() != 16384 {
		t.Fatal("stderr is not bounded")
	}
}
