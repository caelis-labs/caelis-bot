package desktop

// FeatureCapability separates host implementation, current readiness, user
// choice and OS authorization. A denied permission never rewrites a saved
// product choice, and a product choice never claims a system grant.
type FeatureCapability struct {
	Supported  bool   `json:"supported"`
	Available  bool   `json:"available"`
	Enabled    bool   `json:"enabled"`
	Permission string `json:"permission,omitempty"`
}

type FeatureCapabilities struct {
	Capture FeatureCapability `json:"capture"`
	Paste   FeatureCapability `json:"paste"`
}

func (s *Service) FeatureCapabilities() FeatureCapabilities {
	s.mu.Lock()
	_, capture := s.native.(captureDriver)
	ready := s.started && !s.stopped
	enabled := s.capture.preferences.Enabled
	paste := s.readClipboard != nil
	s.mu.Unlock()
	result := FeatureCapabilities{
		Capture: FeatureCapability{Supported: capture, Available: capture && ready, Enabled: enabled},
		Paste:   FeatureCapability{Supported: paste, Available: paste && capture && ready, Enabled: enabled},
	}
	if !capture {
		return result
	}
	for _, permission := range systemPermissionState(s.permissionTerminal()).Permissions {
		if permission.ID == "screenCapture" {
			result.Capture.Permission = permission.Status
			break
		}
	}
	return result
}
