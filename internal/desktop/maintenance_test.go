package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("report not private", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "{\"connection\":\"ready\"}\n" {
		t.Fatal("changed report bytes")
	}
}

func TestPresentationWarningsAreClassifiedOnceAndFeedbackIsExplicit(t *testing.T) {
	var codes []string
	var clipboard string
	s := &Service{presentationWarning: func(code string) { codes = append(codes, code) }, diagnosticReport: func() ([]byte, error) { return []byte(`{"connection":"ready"}`), nil }, copyText: func(value string) bool { clipboard = value; return true }}
	s.ReportPresentationWarning("surface_render_failed")
	s.ReportPresentationWarning("surface_render_failed")
	s.ReportPresentationWarning("PRIVATE_SENTINEL raw exception")
	if len(codes) != 1 || clipboard != "" {
		t.Fatal("warning duplicated or copied without user action", codes)
	}
	if err := s.CopyDiagnostics(); err != nil || !strings.Contains(clipboard, "surface_render_failed") || strings.Contains(clipboard, "PRIVATE_SENTINEL") {
		t.Fatal("feedback omitted classification or exposed raw input", err)
	}
	clipboard = "retained clipboard"
	s.diagnosticReport = func() ([]byte, error) { return nil, errors.New("PRIVATE_SENTINEL") }
	if err := s.CopyDiagnostics(); err == nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") || clipboard != "retained clipboard" {
		t.Fatal("failed feedback exposed diagnostics or replaced clipboard", err)
	}
}
