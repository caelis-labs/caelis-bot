package nodeagent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type RoamingWorkerRuntime struct {
	Backend   string                    `json:"backend"`
	Binary    string                    `json:"binary"`
	Store     string                    `json:"store,omitempty"`
	Model     string                    `json:"model,omitempty"`
	Execution api.WorkExecutionSettings `json:"execution"`
}

func ValidateRoamingWorkers(payload []byte) error {
	var document struct {
		Version int                                              `json:"version"`
		Nodes   []struct{ ID, Label, Backend, Transport string } `json:"nodes"`
		Agents  []struct{ NodeID, Backend, Socket string }       `json:"agents"`
		Sources []struct {
			NodeID   string
			Backends []string
		} `json:"sources"`
		Runtimes []RoamingWorkerRuntime `json:"runtimes"`
	}
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if d.Decode(&document) != nil || d.Decode(new(any)) != io.EOF || document.Version != 1 || len(document.Nodes) > 32 || len(document.Nodes) != len(document.Agents) || len(document.Runtimes) > 2 {
		return errors.New("invalid closed target worker roster")
	}
	for _, v := range document.Nodes {
		if !identifier.MatchString(v.ID) || (v.Backend != "codex" && v.Backend != "caelis") || v.Transport != "registered-agent" {
			return errors.New("invalid worker route")
		}
	}
	seen := map[api.WorkTarget]bool{}
	for _, v := range document.Nodes {
		target := api.WorkTarget{NodeID: v.ID, Backend: v.Backend, Role: api.RoleWorker}
		if seen[target] {
			return errors.New("duplicate worker route")
		}
		seen[target] = true
	}
	for _, v := range document.Agents {
		if !identifier.MatchString(v.NodeID) || !filepath.IsAbs(v.Socket) || filepath.Clean(v.Socket) != v.Socket || len(v.Socket) >= 100 || strings.ContainsAny(v.Socket, ":\x00\r\n") || !seen[api.WorkTarget{NodeID: v.NodeID, Backend: v.Backend, Role: api.RoleWorker}] {
			return errors.New("invalid private worker route")
		}
		delete(seen, api.WorkTarget{NodeID: v.NodeID, Backend: v.Backend, Role: api.RoleWorker})
	}
	if len(document.Sources) > 32 {
		return errors.New("bounded worker source roster required")
	}
	for _, v := range document.Sources {
		if !identifier.MatchString(v.NodeID) || len(v.Backends) > 2 {
			return errors.New("invalid worker source pairing")
		}
		for _, b := range v.Backends {
			if b != "codex" && b != "caelis" {
				return errors.New("invalid worker source backend")
			}
		}
	}
	for _, v := range document.Runtimes {
		if (v.Backend != "codex" && v.Backend != "caelis") || !filepath.IsAbs(v.Binary) || filepath.Clean(v.Binary) != v.Binary || v.Backend == "caelis" && !filepath.IsAbs(v.Store) {
			return errors.New("invalid target-local runtime")
		}
	}
	return nil
}
