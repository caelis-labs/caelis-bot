package desktop

import (
	"path/filepath"
	"testing"
)

func TestFeatureCapabilitiesKeepChoiceSeparateFromHostSupport(t *testing.T) {
	s := newService(fileStore{filepath.Join(t.TempDir(), "placement.json")})
	s.capture.preferences.Enabled = true
	capabilities := s.FeatureCapabilities()
	if !capabilities.Capture.Enabled || capabilities.Capture.Supported || capabilities.Capture.Available ||
		capabilities.Capture.Permission != "" || capabilities.Paste.Supported || capabilities.Paste.Available {
		t.Fatalf("feature choice must not imply host support or a permission grant: %+v", capabilities)
	}
}
