package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Deployment Worker bindings are nonsecret native configuration, separate from
// Notebook snapshots. They must be regenerated from approved exact node pairs.
func loadRoamingWorkers(path string) ([]backend.WorkerNodeConfig, error) {
	if path == "" {
		return nil, nil
	}
	b, err := readBrokerFile(path, 64<<10)
	if err != nil {
		return nil, err
	}
	var document struct {
		Version int                        `json:"version"`
		Nodes   []backend.WorkerNodeConfig `json:"nodes"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&document) != nil || d.Decode(&struct{}{}) != io.EOF || document.Version != 1 || len(document.Nodes) > 16 {
		return nil, errors.New("invalid private exact Worker deployment plan")
	}
	seen := map[api.WorkTarget]bool{}
	for _, config := range document.Nodes {
		target := startupTarget(config)
		if target.Validate() != nil || target.NodeID == api.LocalNodeID || seen[target] || target.Backend != "codex" {
			return nil, errors.New("managed remote Worker requires exact configured lease-aware Codex target")
		}
		seen[target] = true
	}
	return document.Nodes, nil
}
func connectRoamingWorkers(ctx context.Context, service *backend.Service, configs []backend.WorkerNodeConfig) error {
	selected := make([]api.WorkTarget, 0, len(configs))
	for _, config := range configs {
		if _, err := service.SaveWorkerNode(config, service.WorkerNodes().Revision); err != nil {
			return err
		}
		selected = append(selected, startupTarget(config))
	}
	return connectStartupWorkers(ctx, service, selected)
}
