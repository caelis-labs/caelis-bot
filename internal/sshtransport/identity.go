package sshtransport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Only a native file picker may register a key. Store a protected path reference,
// never key bytes, passwords or passphrases, and expose only an opaque handle.
func (t *Transport) RegisterIdentity(path string) (api.SSHIdentity, error) {
	if !filepath.IsAbs(path) {
		return api.SSHIdentity{}, errors.New("ssh_key_unavailable")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return api.SSHIdentity{}, errors.New("ssh_key_unavailable")
	}
	t.configMu.Lock()
	defer t.configMu.Unlock()
	entries, err := t.identities()
	if err != nil {
		return api.SSHIdentity{}, err
	}
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	id := hex.EncodeToString(sum[:])
	entries[id] = path
	if err = localstate.WriteConfirmed(filepath.Join(t.root, "identities.json"), entries); err != nil {
		return api.SSHIdentity{}, errors.New("ssh_key_unavailable")
	}
	return api.SSHIdentity{ID: id, Name: filepath.Base(path)}, nil
}
func (t *Transport) identities() (map[string]string, error) {
	entries := map[string]string{}
	b, err := os.ReadFile(filepath.Join(t.root, "identities.json"))
	if os.IsNotExist(err) {
		return entries, nil
	}
	if err != nil || json.Unmarshal(b, &entries) != nil || entries == nil {
		return nil, errors.New("ssh_key_unavailable")
	}
	return entries, nil
}
func (t *Transport) identityPath(id string) (string, error) {
	t.configMu.Lock()
	defer t.configMu.Unlock()
	entries, err := t.identities()
	if err != nil {
		return "", err
	}
	path := entries[id]
	if !filepath.IsAbs(path) {
		return "", errors.New("ssh_key_unavailable")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("ssh_key_unavailable")
	}
	return path, nil
}
