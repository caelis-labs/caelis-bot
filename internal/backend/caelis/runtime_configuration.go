package caelis

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func rawCatalog(ctx context.Context, c *client, command string) ([]wire.SlashArgCandidate, error) {
	var out []wire.SlashArgCandidate
	err := c.json(ctx, "POST", "/completion/slash-arguments", wire.CompletionRequest{Command: &command, Limit: pointer(1000)}, &out, "", "")
	return out, err
}
func hostRevision(ctx context.Context, c *client) (wire.StatusSnapshot, error) {
	var status wire.StatusSnapshot
	err := c.json(ctx, "GET", "/status", nil, &status, "", "")
	return status, err
}
func modelOptions(candidates []wire.SlashArgCandidate) []api.ModelOption {
	out := []api.ModelOption{}
	for _, m := range candidates {
		if value(m.NoAuth) {
			continue
		}
		v := api.ModelOption{Model: m.Value, Name: value(m.Display), Description: value(m.Detail), Efforts: []string{}, ServiceTiers: []api.ServiceTier{}}
		if v.Name == "" {
			v.Name = m.Value
		}
		if m.ModelSelection != nil {
			v.Default = value(m.ModelSelection.Current)
			v.DefaultEffort = m.ModelSelection.Effort
			v.Efforts = append(v.Efforts, m.ModelSelection.Efforts...)
			if value(m.ModelSelection.FastSupported) {
				v.ServiceTiers = append(v.ServiceTiers, api.ServiceTier{ID: "priority", Name: "Fast"})
			}
		}
		out = append(out, v)
	}
	return out
}

// ReadRuntimeConfiguration reads a coherent revision of the public Host
// projection. It never resolves settings through a Bot application credential.
func ReadRuntimeConfiguration(ctx context.Context, settings api.RuntimeSettings) (api.RuntimeConfiguration, error) {
	c, err := setupClient(ctx, settings)
	if err != nil {
		return api.RuntimeConfiguration{}, err
	}
	defer c.http.CloseIdleConnections()
	for range 3 {
		before, err := hostRevision(ctx, c)
		if err != nil {
			return api.RuntimeConfiguration{}, err
		}
		var status wire.AgentBindingStatus
		if err = c.json(ctx, "POST", "/agents/binding-status", wire.AgentRequest{}, &status, "", ""); err != nil {
			return api.RuntimeConfiguration{}, err
		}
		candidates, err := rawCatalog(ctx, c, "model")
		if err != nil {
			return api.RuntimeConfiguration{}, err
		}
		var disconnected wire.DisconnectCandidatesSnapshot
		if err = c.json(ctx, "POST", "/agents/disconnect-candidates", wire.AgentRequest{}, &disconnected, "", ""); err != nil {
			return api.RuntimeConfiguration{}, err
		}
		after, err := hostRevision(ctx, c)
		if err != nil {
			return api.RuntimeConfiguration{}, err
		}
		if before.Configuration.Revision != after.Configuration.Revision || disconnected.Revision != after.Configuration.Revision {
			continue
		}
		info, err := initialize(ctx, c)
		if err != nil {
			return api.RuntimeConfiguration{}, err
		}
		out := projectRuntimeConfiguration(status, candidates, disconnected, after)
		out.OAuthAvailable = slices.Contains(info.Capabilities, "model-auth-stream-v1")
		return out, nil
	}
	return api.RuntimeConfiguration{}, errors.New("Caelis 配置正在变化，请刷新后重试")
}
func projectRuntimeConfiguration(status wire.AgentBindingStatus, candidates []wire.SlashArgCandidate, disconnected wire.DisconnectCandidatesSnapshot, host wire.StatusSnapshot) api.RuntimeConfiguration {
	rev := string(host.Configuration.Revision)
	out := api.RuntimeConfiguration{Revision: rev, Models: modelOptions(candidates), Team: api.RuntimeTeam{Available: true, Revision: rev, Roles: []api.RuntimeRole{}, Sets: []api.RuntimeTeamSet{}, Models: []api.ModelOption{}}, Connections: []api.RuntimeConnectionGroup{}}
	type profileSource struct {
		Provider *struct {
			ModelConfigID string `json:"model_config_id"`
		} `json:"provider"`
		ACP *struct {
			AgentID string `json:"agent_id"`
		} `json:"acp"`
	}
	sources := map[string]profileSource{}
	for _, p := range status.Targets {
		id := value(p.Id)
		v := api.ModelOption{Model: id, Name: value(p.DisplayName), DefaultEffort: value(p.Effort.DefaultEffort), Efforts: []string{}, ServiceTiers: []api.ServiceTier{}}
		if v.Name == "" {
			v.Name = id
		}
		for _, e := range p.Effort.Choices {
			v.Efforts = append(v.Efforts, value(e.Canonical))
		}
		if p.Speed != nil {
			for _, speed := range p.Speed.Choices {
				v.ServiceTiers = append(v.ServiceTiers, api.ServiceTier{ID: speed.Canonical, Name: map[string]string{"fast": "Fast", "standard": "标准"}[speed.Canonical]})
			}
		}
		out.Team.Models = append(out.Team.Models, v)
		b, _ := json.Marshal(p.Backend)
		var source profileSource
		_ = json.Unmarshal(b, &source)
		sources[id] = source
	}
	for _, r := range status.Handles {
		b, _ := json.Marshal(r.Definition)
		var d struct {
			Handle, Description, Class string
			Custom, Configurable       bool
		}
		_ = json.Unmarshal(b, &d)
		if d.Handle == "self" || !d.Configurable {
			continue
		}
		out.Team.Roles = append(out.Team.Roles, api.RuntimeRole{ID: d.Handle, ModelIDs: r.EligibleProfileIds, Description: d.Description, System: d.Class == "system", Custom: d.Custom, Selection: api.WorkExecutionSettings{Model: value(r.Binding.ProfileId), Effort: value(r.Binding.Effort), ServiceTier: value(r.Binding.Speed)}, Inherited: value(r.Binding.ProfileId) == ""})
	}
	for _, set := range status.Sets {
		out.Team.Sets = append(out.Team.Sets, api.RuntimeTeamSet{Name: set.Name, Available: set.Available, Problem: set.Problem})
		if set.Active {
			out.Team.ActiveSet = set.Name
		}
	}
	groups := map[string]int{}
	agents := map[string]string{}
	for _, a := range disconnected.Candidates {
		agents[value(a.AgentId)] = value(a.Name)
	}
	for _, m := range candidates {
		groupID, kind, display := "models", "provider", "模型服务"
		profileID := ""
		for id, source := range sources {
			if source.Provider != nil && source.Provider.ModelConfigID == value(m.ModelConfigId) && value(m.ModelConfigId) != "" {
				profileID = id
				break
			}
			if source.ACP != nil && id == m.Value {
				profileID = id
				break
			}
		}
		source := sources[profileID]
		if source.ACP != nil {
			groupID = source.ACP.AgentID
			kind = "agent"
			display = agents[groupID]
			if display == "" {
				display = groupID
			}
		} else if prefix, _, ok := strings.Cut(m.Value, "/"); ok {
			groupID = prefix
			display = prefix
		}
		groupKey := kind + ":" + groupID
		index, ok := groups[groupKey]
		if !ok {
			index = len(out.Connections)
			groups[groupKey] = index
			out.Connections = append(out.Connections, api.RuntimeConnectionGroup{ID: groupID, Name: display, Kind: kind, Detail: "保存在本机 Caelis", Models: []api.RuntimeConnectionModel{}})
		}
		v := api.RuntimeConnectionModel{ID: m.Value, Name: value(m.Display), Uses: []string{}, Unavailable: value(m.NoAuth)}
		if v.Name == "" {
			v.Name = m.Value
		}
		if m.ModelSelection != nil && value(m.ModelSelection.Current) {
			out.Main = api.WorkExecutionSettings{Model: m.Value, Effort: m.ModelSelection.Effort}
			if value(m.ModelSelection.Fast) {
				out.Main.ServiceTier = "priority"
			}
			v.Uses = append(v.Uses, "Caelis 主模型")
		}
		for _, r := range out.Team.Roles {
			if profileID != "" && r.Selection.Model == profileID {
				v.Uses = append(v.Uses, r.ID)
			}
		}
		out.Connections[index].Models = append(out.Connections[index].Models, v)
	}
	return out
}
func mutationResult(op string, result wire.CommandResult, err error) api.RuntimeMutationResult {
	out := api.RuntimeMutationResult{OperationID: op, Outcome: string(result.Outcome)}
	if err != nil || out.Outcome == "" || result.OperationId != op {
		out.Outcome = "unknown"
	}
	switch out.Outcome {
	case "committed":
		out.Message = "配置已保存"
	case "conflicted":
		out.Message = "配置已被其他客户端修改，请刷新后核对"
	case "rejected":
		out.Message = "Caelis 拒绝此配置，请检查模型能力和角色绑定"
	default:
		out.Message = "操作结果未确认，请刷新核对，不要重复提交"
	}
	return out
}

// ChangeRuntimeConfiguration fences every shared mutation to the displayed
// revision. Native validation, side effects and idempotency remain in Caelis.
func ChangeRuntimeConfiguration(ctx context.Context, settings api.RuntimeSettings, change api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	if _, err := strconv.ParseUint(change.ExpectedRevision, 10, 64); err != nil {
		return api.RuntimeMutationResult{}, errors.New("配置版本无效，请刷新")
	}
	c, err := setupClient(ctx, settings)
	if err != nil {
		return api.RuntimeMutationResult{}, err
	}
	defer c.http.CloseIdleConnections()
	op := "settings-" + rand.Text()
	rev := wire.Uint64Decimal(change.ExpectedRevision)
	var body any
	path := ""
	switch change.Action {
	case "main":
		if change.Selection.Model == "" {
			return api.RuntimeMutationResult{}, errors.New("请选择主模型")
		}
		fast := change.Selection.ServiceTier == "priority"
		if change.Selection.ServiceTier != "" && !fast {
			return api.RuntimeMutationResult{}, errors.New("无效的响应速度")
		}
		body = wire.UseModelRequest{Model: change.Selection.Model, ReasoningEffort: &change.Selection.Effort, FastMode: &fast, ExpectedRevision: &rev, OperationId: &op}
		path = "/configuration/use-model"
	case "bind":
		body = wire.BindAgentBindingRequest{Binding: wire.AgentBinding{Handle: &change.ID, ProfileId: &change.Selection.Model, Effort: &change.Selection.Effort, Speed: &change.Selection.ServiceTier}, ExpectedRevision: &rev, OperationId: &op}
		path = "/agents/bind"
	case "reset":
		body = wire.ResetAgentBindingRequest{Handle: change.ID, ExpectedRevision: &rev, OperationId: &op}
		path = "/agents/reset-binding"
	case "create-role":
		body = wire.CreateAgentRoleRequest{Role: wire.JSONObject{"handle": change.ID, "description": change.Description}, ExpectedRevision: &rev, OperationId: &op}
		path = "/agents/create-role"
	case "delete-role":
		body = wire.DeleteAgentRoleRequest{Handle: change.ID, ExpectedRevision: &rev, OperationId: &op}
		path = "/agents/delete-role"
	case "save-set", "apply-set", "delete-set":
		body = wire.AgentBindingSetRequest{SetName: change.Name, ExpectedRevision: &rev, OperationId: &op}
		path = "/agents/" + strings.TrimSuffix(change.Action, "-set") + "-binding-set"
	case "remove-model":
		body = wire.DeleteModelRequest{Model: change.ID, ExpectedRevision: &rev, OperationId: &op}
		path = "/configuration/delete-model"
	case "disconnect-agent":
		body = wire.DisconnectACPRequest{AgentId: change.ID, ExpectedRevision: &rev, OperationId: &op}
		path = "/agents/disconnect-acp"
	default:
		return api.RuntimeMutationResult{}, errors.New("不支持的配置操作")
	}
	var result wire.CommandResult
	err = c.json(ctx, "POST", path, body, &result, op, string(rev))
	return mutationResult(op, result, err), nil
}
