package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sort"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// IndexServer is a Runtime-confirmed plugin service. Callers must omit failed,
// disabled, and unknown services; this file never grants invocation authority.
type IndexServer struct {
	PackageID, Name, RuntimeName string
	Tools                        []Tool
}

type indexTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type indexService struct {
	Package string      `json:"package"`
	Service string      `json:"service"`
	Runtime string      `json:"runtime"`
	Tools   []indexTool `json:"tools"`
}
type toolIndex struct {
	Services []indexService `json:"services"`
}

// WriteIndex writes ordinary, non-secret tool clues only when the sanitized
// content changes. Empty services means no connected directory was confirmed.
func WriteIndex(path string, connected []IndexServer) error {
	index := toolIndex{Services: []indexService{}}
	for _, raw := range connected {
		pkg, service, runtime := SafeDisplayText(raw.PackageID), SafeDisplayText(raw.Name), SafeDisplayText(raw.RuntimeName)
		if pkg == "" || service == "" || runtime == "" {
			continue
		}
		entry := indexService{Package: pkg, Service: service, Runtime: runtime, Tools: []indexTool{}}
		seen := map[string]bool{}
		for _, tool := range raw.Tools {
			name := SafeDisplayToolName(tool.Name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			entry.Tools = append(entry.Tools, indexTool{Name: name, Description: SafeDisplayDescription(tool.Description)})
		}
		sort.Slice(entry.Tools, func(i, j int) bool { return entry.Tools[i].Name < entry.Tools[j].Name })
		index.Services = append(index.Services, entry)
	}
	sort.Slice(index.Services, func(i, j int) bool {
		if index.Services[i].Package != index.Services[j].Package {
			return index.Services[i].Package < index.Services[j].Package
		}
		return index.Services[i].Service < index.Services[j].Service
	})
	want, err := json.Marshal(index)
	if err != nil {
		return err
	}
	want = append(want, '\n')
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("plugin index is not a regular file")
		}
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Equal(current, want) {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return localstate.WriteConfirmed(path, index)
}
