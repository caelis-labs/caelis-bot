package plugins

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

func secretKey(root, id string, revision uint64) string {
	sum := sha256.Sum256([]byte(root))
	return "plugin-" + hex.EncodeToString(sum[:8]) + "-" + id + "-" + fmt.Sprint(revision)
}

func validCredential(value string) bool {
	if value == "" || len(value) > 8192 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// ConfigureConnection saves a write-only credential in Bot's native secret
// store and publishes a new activation revision. The caller holds Runtime
// admission through apply and the confirmed state write, as for package actions.
func (m *Manager) ConfigureConnection(ctx context.Context, id, secret, caPEM string, clear bool, apply func(context.Context, Selection) error) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entry(id)
	if !ok || e.Connection == nil {
		return m.snapshotLocked(), errors.New("plugin has no credential connection")
	}
	if e.Connection.Kind == "oauth" {
		return m.snapshotLocked(), errors.New("OAuth connection is not available")
	}
	if _, installed := m.state.Installed[id]; !installed {
		return m.snapshotLocked(), errors.New("plugin is not installed")
	}
	secret = strings.TrimSpace(secret)
	if !clear && !validCredential(secret) {
		return m.snapshotLocked(), errors.New("invalid credential")
	}
	if caPEM != "" {
		if !e.Connection.TrustCA || len(caPEM) > 64<<10 {
			return m.snapshotLocked(), errors.New("invalid CA certificate")
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return m.snapshotLocked(), errors.New("invalid CA certificate")
		}
	}
	current := m.state.Connections[id]
	if clear && !current.Configured {
		return m.snapshotLocked(), nil
	}
	if !clear && current.Configured {
		old, loadErr := m.secrets.Load(secretKey(m.root, id, current.Revision))
		if loadErr == nil && old == secret {
			if caPEM == "" {
				return m.snapshotLocked(), nil
			}
			previousCA, caErr := m.secrets.Load(secretKey(m.root, id, current.Revision) + "-ca")
			if caErr == nil && previousCA == caPEM {
				return m.snapshotLocked(), nil
			}
		}
	}
	next := cloneState(m.state)
	next.Revision++
	record := connectionRecord{Revision: next.Revision, Configured: !clear, HasCA: !clear && (caPEM != "" || current.HasCA)}
	next.Connections[id] = record
	if !clear {
		if caPEM == "" && current.HasCA {
			var loadErr error
			caPEM, loadErr = m.secrets.Load(secretKey(m.root, id, current.Revision) + "-ca")
			if loadErr != nil {
				return m.snapshotLocked(), errors.New("saved CA certificate unavailable")
			}
		}
		if err := m.secrets.Save(secretKey(m.root, id, record.Revision), secret); err != nil {
			return m.snapshotLocked(), errors.New("credential store unavailable")
		}
		if record.HasCA {
			if err := m.secrets.Save(secretKey(m.root, id, record.Revision)+"-ca", caPEM); err != nil {
				return m.snapshotLocked(), errors.New("credential store unavailable")
			}
		}
	}
	if apply != nil {
		if err := apply(ctx, m.selectionLocked(next)); err != nil {
			return m.snapshotLocked(), err
		}
	}
	if err := m.save(next); err != nil {
		failures := []error{err}
		if e := m.save(m.state); e != nil {
			failures = append(failures, fmt.Errorf("connection state restoration failed: %w", e))
		}
		if apply != nil {
			recovery, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			e := apply(recovery, m.selectionLocked(m.state))
			cancel()
			if e != nil {
				failures = append(failures, fmt.Errorf("connection rollback failed: %w", e))
			}
		}
		return m.snapshotLocked(), errors.Join(failures...)
	}
	m.state = next
	// Old workers keep the credential they loaded at process start. Revoking the
	// old Keychain item prevents a stale profile from starting a new process.
	if current.Configured {
		_ = m.secrets.Delete(secretKey(m.root, id, current.Revision))
		if current.HasCA {
			_ = m.secrets.Delete(secretKey(m.root, id, current.Revision) + "-ca")
		}
	}
	return m.snapshotLocked(), nil
}
