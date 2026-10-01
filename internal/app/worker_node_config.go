package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func loadWorkerNodeDocument(filename string) (workerNodeDocument, error) {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return workerNodeDocument{Version: 1, Nodes: []backend.WorkerNodeConfig{}}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 128*1024 {
		return workerNodeDocument{}, errors.New("worker node configuration is unavailable")
	}
	b, err := os.ReadFile(filename)
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	var document workerNodeDocument
	if err != nil || decoder.Decode(&document) != nil || document.Version != 1 || len(document.Nodes) > 16 {
		return workerNodeDocument{}, errors.New("worker node configuration is unavailable")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return workerNodeDocument{}, errors.New("worker node configuration is unavailable")
	}
	seen := map[api.WorkTarget]bool{}
	machines := map[string]backend.WorkerNodeConfig{}
	for _, config := range document.Nodes {
		target := configuredWorkerTarget(config)
		previous, exists := machines[config.ID]
		if validateWorkerNode(config) != nil || seen[target] || exists && (previous.SSH != config.SSH || previous.Label != config.Label) {
			return workerNodeDocument{}, errors.New("worker node configuration is unavailable")
		}
		seen[target] = true
		machines[config.ID] = config
	}
	return document, nil
}

// ValidateConfiguredWorkerTargets is an offline preflight for explicit native
// startup. It uses the same private document validation as ordinary APP setup.
// No selection retains the default optional-config tolerance and performs no IO.
func ValidateConfiguredWorkerTargets(root string, targets []api.WorkTarget) error {
	if len(targets) == 0 {
		return nil
	}
	document, err := loadWorkerNodeDocument(filepath.Join(root, "worker-nodes.json"))
	if err != nil {
		return err
	}
	seen := map[api.WorkTarget]bool{}
	for _, target := range targets {
		if target.Role != api.RoleWorker || (target.Backend != "caelis" && target.Backend != "codex") || !workerNodeID.MatchString(target.NodeID) || seen[target] {
			return errors.New("invalid or repeated exact Worker target")
		}
		seen[target] = true
		matches := 0
		for _, config := range document.Nodes {
			if configuredWorkerTarget(config) == target {
				matches++
			}
		}
		if matches != 1 {
			return errors.New("exact Worker target is not configured")
		}
	}
	return nil
}
