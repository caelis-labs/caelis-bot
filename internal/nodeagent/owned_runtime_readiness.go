package nodeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
)

// OwnedRuntimeCompanion is a trusted native packaged helper binding. No wire
// request can select this helper or supply its expected release digest.
type OwnedRuntimeCompanion struct{ Path, SHA256 string }

// OwnedRuntimeReadinessRequest is emitted only after explicit persistent-plan
// approval. Binary/Store are frozen native metadata, never renderer input.
type OwnedRuntimeReadinessRequest struct {
	NodeID         string          `json:"nodeId"`
	Backend        api.NodeBackend `json:"backend"`
	OperationID    string          `json:"operationId"`
	ExpectedBinary string          `json:"expectedBinary"`
	ExpectedStore  string          `json:"expectedStore"`
	Model          string          `json:"model,omitempty"`
}

type OwnedRuntimeReadinessReceipt struct {
	NodeID               string          `json:"nodeId"`
	Backend              api.NodeBackend `json:"backend"`
	OperationID          string          `json:"operationId"`
	Outcome              string          `json:"outcome"`
	Ready                bool            `json:"ready"`
	Model                string          `json:"model,omitempty"`
	NativeConfigRevision string          `json:"nativeConfigRevision,omitempty"`
	AuthenticatedModels  []string        `json:"authenticatedModels,omitempty"`
	StopConfirmed        bool            `json:"stopConfirmed"`
	Reason               string          `json:"reason,omitempty"`
}

type ownedRuntimeReadinessRef struct {
	NodeID      string          `json:"nodeId"`
	Backend     api.NodeBackend `json:"backend"`
	OperationID string          `json:"operationId"`
}
type ownedReadinessCheck func(context.Context, caelis.OwnedHostOptions, string) (caelis.OwnedReadiness, error)
type ownedRuntimeReadinessPort interface {
	CheckOwnedRuntimeReadiness(context.Context, OwnedRuntimeReadinessRequest) (OwnedRuntimeReadinessReceipt, error)
	ReadOwnedRuntimeReadiness(context.Context, string, api.NodeBackend, string) (OwnedRuntimeReadinessReceipt, error)
}
type ownedRuntimeReadinessRecord struct {
	Schema  int                          `json:"schema"`
	Request OwnedRuntimeReadinessRequest `json:"request"`
	Receipt OwnedRuntimeReadinessReceipt `json:"receipt"`
}

func publicReadinessValue(value string) bool {
	return len(value) <= 512 && !strings.ContainsAny(value, "\x00\r\n")
}
func validReadinessRef(r ownedRuntimeReadinessRef) bool {
	return identifier.MatchString(r.NodeID) && r.Backend == api.NodeCaelis && identifier.MatchString(r.OperationID)
}
func validReadinessRequest(r OwnedRuntimeReadinessRequest) bool {
	return validReadinessRef(ownedRuntimeReadinessRef{r.NodeID, r.Backend, r.OperationID}) && cleanOwnedRuntimePath(r.ExpectedBinary) && cleanOwnedRuntimePath(r.ExpectedStore) && publicReadinessValue(r.Model)
}
func readinessIdentity(r OwnedRuntimeReadinessRequest) OwnedRuntimeReadinessReceipt {
	return OwnedRuntimeReadinessReceipt{NodeID: r.NodeID, Backend: r.Backend, OperationID: r.OperationID, Outcome: "unknown", Reason: "readiness-in-progress"}
}

func (s *Service) readinessDirectory(b api.NodeBackend, operationID string) string {
	h := sha256.Sum256([]byte(string(b) + "\x00" + operationID))
	return filepath.Join(s.options.Directory, "owned-readiness", hex.EncodeToString(h[:]))
}
func (s *Service) readReadinessRecord(r ownedRuntimeReadinessRef) (ownedRuntimeReadinessRecord, error) {
	var record ownedRuntimeReadinessRecord
	if err := readPrivateJSON(filepath.Join(s.readinessDirectory(r.Backend, r.OperationID), "intent.json"), &record); err != nil {
		return record, err
	}
	if record.Schema != 1 || !validReadinessRequest(record.Request) || record.Request.NodeID != r.NodeID || record.Request.Backend != r.Backend || record.Request.OperationID != r.OperationID || !validReadinessReceipt(record.Receipt, r) {
		return record, errors.New("original readiness receipt invalid")
	}
	return record, nil
}

func (s *Service) ReadOwnedRuntimeReadiness(ctx context.Context, nodeID string, b api.NodeBackend, operationID string) (OwnedRuntimeReadinessReceipt, error) {
	r := ownedRuntimeReadinessRef{nodeID, b, operationID}
	if err := ctx.Err(); err != nil {
		return OwnedRuntimeReadinessReceipt{}, err
	}
	if !validReadinessRef(r) || nodeID != s.options.NodeID {
		return OwnedRuntimeReadinessReceipt{}, errors.New("original readiness scope changed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.readReadinessRecord(r)
	if err != nil {
		return OwnedRuntimeReadinessReceipt{NodeID: nodeID, Backend: b, OperationID: operationID, Outcome: "unknown", Reason: "original-readiness-unavailable"}, nil
	}
	return record.Receipt, nil
}

func (s *Service) readinessCompanion(ctx context.Context) (OwnedRuntimeCompanion, error) {
	if s.options.OwnedRuntimeCompanion != nil {
		return s.options.OwnedRuntimeCompanion(ctx)
	}
	metadata, err := ReadNativeCompanionMetadata(s.options.Directory, s.options.NodeID)
	if err != nil || metadata.HelperSHA256 == "" {
		return OwnedRuntimeCompanion{}, errors.New("reviewed readiness companion unavailable")
	}
	return OwnedRuntimeCompanion{Path: metadata.Helper, SHA256: metadata.HelperSHA256}, nil
}
func verifyReadinessCompanion(c OwnedRuntimeCompanion) error {
	digest, err := hex.DecodeString(c.SHA256)
	if err != nil || len(digest) != sha256.Size || !cleanOwnedRuntimePath(c.Path) {
		return errors.New("reviewed readiness companion invalid")
	}
	info, err := os.Lstat(c.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 || info.Size() <= 0 || info.Size() > 256<<20 {
		return errors.New("reviewed readiness companion unavailable")
	}
	f, err := os.Open(c.Path)
	if err != nil {
		return errors.New("reviewed readiness companion unavailable")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("reviewed readiness companion changed")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil || hex.EncodeToString(h.Sum(nil)) != strings.ToLower(c.SHA256) {
		return errors.New("reviewed readiness companion changed")
	}
	return nil
}

// CheckOwnedRuntimeReadiness journals the original approved intent before a
// bounded owned-Host check. Repeating any existing operation only reconciles;
// an interrupted or unknown check can never launch a replacement Host.
func (s *Service) CheckOwnedRuntimeReadiness(ctx context.Context, r OwnedRuntimeReadinessRequest) (OwnedRuntimeReadinessReceipt, error) {
	result := readinessIdentity(r)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !validReadinessRequest(r) || r.NodeID != s.options.NodeID {
		return result, errors.New("exact native readiness request required")
	}
	ref := ownedRuntimeReadinessRef{r.NodeID, r.Backend, r.OperationID}
	s.mu.Lock()
	record, existingErr := s.readReadinessRecord(ref)
	if existingErr == nil {
		s.mu.Unlock()
		if record.Request != r {
			return result, errors.New("original readiness intent changed")
		}
		return record.Receipt, nil
	}
	if _, err := os.Lstat(s.readinessDirectory(r.Backend, r.OperationID)); !errors.Is(err, os.ErrNotExist) {
		s.mu.Unlock()
		result.Reason = "original-readiness-unavailable"
		return result, nil
	}
	s.mu.Unlock()
	metadata, err := s.ReadOwnedRuntimeSettings(ctx, r.NodeID, r.Backend)
	if err != nil || metadata.Binary != r.ExpectedBinary || metadata.Store != r.ExpectedStore {
		return result, errors.New("reviewed readiness native binding changed")
	}
	if eligible, _ := caelis.ProbeOwnedStore(r.NodeID, metadata.Store); !eligible {
		return result, errors.New("designated owned Store unavailable")
	}
	companion, err := s.readinessCompanion(ctx)
	if err != nil || verifyReadinessCompanion(companion) != nil {
		return result, errors.New("reviewed readiness companion unavailable")
	}
	s.mu.Lock()
	parent := filepath.Join(s.options.Directory, "owned-readiness")
	if err := os.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		s.mu.Unlock()
		return result, err
	}
	if err := CheckPrivateDirectory(parent); err != nil {
		s.mu.Unlock()
		return result, err
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) >= MaxOperations {
		s.mu.Unlock()
		return result, errors.New("native readiness receipt limit")
	}
	for _, entry := range entries {
		var prior ownedRuntimeReadinessRecord
		if !entry.IsDir() || readPrivateJSON(filepath.Join(parent, entry.Name(), "intent.json"), &prior) != nil || prior.Schema != 1 || !validReadinessRequest(prior.Request) || !validReadinessReceipt(prior.Receipt, ownedRuntimeReadinessRef{prior.Request.NodeID, prior.Request.Backend, prior.Request.OperationID}) || prior.Receipt.Outcome == "unknown" && prior.Request.ExpectedStore == r.ExpectedStore {
			s.mu.Unlock()
			result.Reason = "prior-readiness-unconfirmed"
			return result, nil
		}
	}
	directory := s.readinessDirectory(r.Backend, r.OperationID)
	if err := os.Mkdir(directory, 0700); err != nil {
		s.mu.Unlock()
		return s.ReadOwnedRuntimeReadiness(ctx, r.NodeID, r.Backend, r.OperationID)
	}
	record = ownedRuntimeReadinessRecord{Schema: 1, Request: r, Receipt: result}
	if err := writeState(filepath.Join(directory, "intent.json"), record); err != nil {
		s.mu.Unlock()
		return result, err
	}
	for _, path := range []string{parent, s.options.Directory} {
		file, err := os.Open(path)
		if err != nil {
			s.mu.Unlock()
			return result, err
		}
		err = errors.Join(file.Sync(), file.Close())
		if err != nil {
			s.mu.Unlock()
			return result, err
		}
	}
	check := s.readinessCheck
	if check == nil {
		check = caelis.ProbeOwnedReadiness
	}
	s.mu.Unlock()
	life, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	readiness, checkErr := check(life, caelis.OwnedHostOptions{NodeID: r.NodeID, Binary: metadata.Binary, Store: metadata.Store, WatchdogHelper: companion.Path}, r.Model)
	if checkErr != nil {
		result.Reason = "native-readiness-outcome-unconfirmed"
	} else {
		result.StopConfirmed = true
		result.Outcome = "unavailable"
		result.Reason = readiness.Reason
		result.Model = readiness.CurrentModel
		result.NativeConfigRevision = readiness.NativeConfigRevision
		result.AuthenticatedModels = append([]string(nil), readiness.AuthenticatedModels...)
		if readiness.Ready {
			result.Outcome = "ready"
			result.Ready = true
			result.Reason = ""
			if r.Model != "" {
				result.Model = r.Model
			}
		}
		if !validReadinessReceipt(result, ref) {
			result = readinessIdentity(r)
			result.Reason = "native-readiness-outcome-unconfirmed"
		}
	}
	record.Receipt = result
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeState(filepath.Join(directory, "intent.json"), record); err != nil {
		result = readinessIdentity(r)
		result.Reason = "readiness-receipt-unconfirmed"
		return result, nil
	}
	return result, nil
}

func validReadinessReceipt(r OwnedRuntimeReadinessReceipt, expected ownedRuntimeReadinessRef) bool {
	if r.NodeID != expected.NodeID || r.Backend != expected.Backend || r.OperationID != expected.OperationID || !publicReadinessValue(r.Model) || !publicReadinessValue(r.NativeConfigRevision) || len(r.Reason) > 128 || strings.ContainsAny(r.Reason, "\x00\r\n/\\ ") {
		return false
	}
	if len(r.AuthenticatedModels) > 4096 {
		return false
	}
	for _, model := range r.AuthenticatedModels {
		if model == "" || !publicReadinessValue(model) {
			return false
		}
	}
	switch r.Outcome {
	case "ready":
		return r.Ready && r.StopConfirmed && r.Model != "" && r.Reason == ""
	case "unavailable":
		return !r.Ready && r.StopConfirmed && r.Reason != ""
	case "unknown":
		return !r.Ready && !r.StopConfirmed && r.Reason != ""
	}
	return false
}

func (c *Client) CheckOwnedRuntimeReadiness(ctx context.Context, r OwnedRuntimeReadinessRequest) (OwnedRuntimeReadinessReceipt, error) {
	if err := ctx.Err(); err != nil {
		return readinessIdentity(r), err
	}
	if !validReadinessRequest(r) || r.NodeID != c.expected {
		return OwnedRuntimeReadinessReceipt{}, errors.New("exact native readiness request required")
	}
	var out OwnedRuntimeReadinessReceipt
	if err := c.request(context.WithoutCancel(ctx), "POST", "/v1/node/check-owned-readiness", r, &out); err != nil {
		return out, err
	}
	if !validReadinessReceipt(out, ownedRuntimeReadinessRef{r.NodeID, r.Backend, r.OperationID}) {
		return OwnedRuntimeReadinessReceipt{}, errors.New("invalid native readiness receipt")
	}
	return out, nil
}
func (c *Client) ReadOwnedRuntimeReadiness(ctx context.Context, nodeID string, b api.NodeBackend, operationID string) (OwnedRuntimeReadinessReceipt, error) {
	r := ownedRuntimeReadinessRef{nodeID, b, operationID}
	if !validReadinessRef(r) || nodeID != c.expected {
		return OwnedRuntimeReadinessReceipt{}, errors.New("original readiness scope changed")
	}
	var out OwnedRuntimeReadinessReceipt
	if err := c.request(ctx, "POST", "/v1/node/owned-readiness-receipt", r, &out); err != nil {
		return out, err
	}
	if !validReadinessReceipt(out, r) {
		return OwnedRuntimeReadinessReceipt{}, errors.New("invalid original readiness receipt")
	}
	return out, nil
}
