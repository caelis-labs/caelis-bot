package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPermissionGuideOnlyRecordsReviewNotGrants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permission-guide.json")
	s := &Service{}
	s.configurePermissionGuide(path)
	if !s.PermissionGuidePending() {
		t.Fatal("new installation should see the guide")
	}
	if err := s.FinishPermissionGuide(); err != nil {
		t.Fatal(err)
	}
	next := &Service{}
	next.configurePermissionGuide(path)
	if next.PermissionGuidePending() {
		t.Fatal("guide completion was not persisted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{\"seen\":true}\n" {
		t.Fatal("must never persist cached grants", string(data), err)
	}
}
func TestPermissionGuideFailedSaveDoesNotDismiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-directory")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	s.configurePermissionGuide(filepath.Join(path, "guide.json"))
	if err := s.FinishPermissionGuide(); err == nil {
		t.Fatal("expected save failure")
	}
	if !s.PermissionGuidePending() {
		t.Fatal("failed save dismissed guide")
	}
}
func TestPermissionResetNeverAllowsBroadOrInjectedService(t *testing.T) {
	for _, id := range []string{"", "All", "all", "notifications", "Accessibility", "automation; All", "../ScreenCapture"} {
		if permissionResetService(id) != "" {
			t.Fatal("unsafe permission", id)
		}
		if err := (&Service{}).ResetSystemPermission(t.Context(), id); err == nil {
			t.Fatal("invalid reset accepted", id)
		}
	}
	for id, want := range map[string]string{"accessibility": "Accessibility", "automation": "AppleEvents", "screenCapture": "ScreenCapture"} {
		if permissionResetService(id) != want {
			t.Fatal(id)
		}
	}
}
