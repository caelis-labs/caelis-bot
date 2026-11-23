package api

import (
	"encoding/json"
	"slices"
)

// ForService selects the catalog, bearer and direct approvals for one private
// stdio child. Older resident bindings can still use the aggregate connection.
func (c *ToolConnection) ForService(service ToolService) *ToolConnection {
	var catalog []ToolDefinition
	if json.Unmarshal([]byte(c.Env["CAELIS_BOT_TOOL_CATALOG"]), &catalog) != nil {
		return nil
	}
	catalog = slices.DeleteFunc(catalog, func(tool ToolDefinition) bool {
		return !slices.Contains(service.Tools, tool.Name)
	})
	if len(catalog) == 0 {
		return nil
	}
	config := c.Clone()
	config.Services = nil
	raw, _ := json.Marshal(catalog)
	config.Env["CAELIS_BOT_TOOL_CATALOG"] = string(raw)
	config.Env["CAELIS_BOT_SERVICE_NAME"] = service.Name
	config.Env["CAELIS_BOT_TOKEN"] = service.Token
	config.ApprovedTools = slices.DeleteFunc(config.ApprovedTools, func(name string) bool {
		return !slices.ContainsFunc(catalog, func(tool ToolDefinition) bool { return tool.Name == name })
	})
	return config
}
