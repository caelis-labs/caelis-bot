package nodeagent

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// NativeEnrollmentIdentity is native pairing metadata, never a renderer DTO.
// Its directory is the existing enrollment, not a roaming or reverse-join slot.
type NativeEnrollmentIdentity struct {
	NodeID, Directory string
}

func ReadNativeEnrollmentIdentity(directory, nodeID string) (NativeEnrollmentIdentity, error) {
	value := NativeEnrollmentIdentity{NodeID: nodeID, Directory: directory}
	if !identifier.MatchString(nodeID) || CheckPrivateDirectory(directory) != nil {
		return value, errors.New("existing enrolled coordinator required")
	}
	path := filepath.Join(directory, "node.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 || !privateFileOwnedByCurrentUser(info) {
		return value, errors.New("private enrolled coordinator identity unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return value, errors.New("private enrolled coordinator identity unavailable")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0600 || opened.Size() > 4096 || !privateFileOwnedByCurrentUser(opened) {
		return value, errors.New("private enrolled coordinator identity changed")
	}
	var identity struct {
		ID string `json:"id"`
	}
	d := json.NewDecoder(io.LimitReader(f, 4097))
	d.DisallowUnknownFields()
	if d.Decode(&identity) != nil || d.Decode(new(any)) != io.EOF || identity.ID != nodeID {
		return value, errors.New("enrolled coordinator identity changed")
	}
	return value, nil
}

// This closed local port uses the Service's actual enrolled identity. A public
// presentation alias must not be substituted for a machine's stored node ID.
func (s *Service) NativeEnrollmentIdentity() (NativeEnrollmentIdentity, error) {
	return ReadNativeEnrollmentIdentity(s.options.Directory, s.options.NodeID)
}

func ValidateRoamingCoordinatorIdentity(m RoamingManagedDeployment) error {
	identity := m.CoordinatorIdentity
	if identity == nil || !identifier.MatchString(identity.NodeID) || !filepath.IsAbs(identity.Directory) || filepath.Clean(identity.Directory) != identity.Directory || m.BrokerNodeID == "" || m.BrokerNodeID != api.LocalNodeID && identity.NodeID != m.BrokerNodeID {
		return errors.New("exact enrolled coordinator identity required")
	}
	return nil
}

// The helper and directory come only from frozen native enrollment. This is
// one closed read-only command, not a command-bearing RPC or renderer input.
func RoamingCoordinatorVerificationCommand(m RoamingManagedDeployment) (string, error) {
	if err := ValidateRoamingCoordinatorIdentity(m); err != nil || !filepath.IsAbs(m.JoinHelper) || filepath.Clean(m.JoinHelper) != m.JoinHelper {
		return "", errors.New("fixed enrolled coordinator helper required")
	}
	return shellQuote(m.JoinHelper) + " verify-join-directory --directory " + shellQuote(m.CoordinatorIdentity.Directory) + " --node-id " + shellQuote(m.CoordinatorIdentity.NodeID), nil
}
