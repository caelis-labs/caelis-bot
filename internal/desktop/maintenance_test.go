package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticExportWritesPrivateChosenFileAndCancelWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	s := &Service{diagnosticReport: func() ([]byte, error) { return []byte(`{"connection":"ready"}`), nil }}
	s.saveDiagnosticPath = func() (string, error) { return "", nil }
	if message, err := s.ExportDiagnostics(); err != nil || message != "" {
		t.Fatal("cancel was reported as export")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancel wrote a file")
	}
	s.saveDiagnosticPath = func() (string, error) { return path, nil }
	if message, err := s.ExportDiagnostics(); err != nil || message == "" {
		t.Fatal("export failed", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("report not private", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "{\"connection\":\"ready\"}\n" {
		t.Fatal("changed report bytes")
	}
}
