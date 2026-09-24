package caelis

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// setupClient uses Host authority for explicit settings actions and read-only
// work-model resolution. Never reuse a Bot token for Host configuration, nor
// write secret-bearing requests to the Bot journal.
func setupClient(ctx context.Context, v api.RuntimeSettings) (*client, error) {
	d, token, e := Discover(v)
	if e != nil {
		return nil, e
	}
	c, e := newClient(d.Endpoint, token)
	if e != nil {
		return nil, e
	}
	i, e := initialize(ctx, c)
	if e != nil {
		c.http.CloseIdleConnections()
		return nil, e
	}
	if value(i.InstanceId) != d.InstanceID {
		c.http.CloseIdleConnections()
		return nil, errors.New("Caelis 服务已变化，请重新检测")
	}
	return c, nil
}
func InspectSetup(ctx context.Context, v api.RuntimeSettings) (api.SetupState, error) {
	out := api.SetupState{Settings: v, Models: []api.SetupChoice{}, State: "service"}
	d, _, e := Discover(v)
	if e != nil {
		out.ServiceState = "unknown"
		out.Message = e.Error()
		return out, nil
	}
	out.ServiceVersion = d.DistributionVersion
	out.ServiceState = "running"
	c, e := setupClient(ctx, v)
	if e != nil {
		if !errors.Is(e, errBotIncompatible) {
			out.ServiceState = "unknown"
		}
		out.State = "unavailable"
		if errors.Is(e, errBotIncompatible) {
			out.State = "incompatible"
		}
		out.Message = e.Error()
		return out, nil
	}
	defer c.http.CloseIdleConnections()
	models, e := setupCatalog(ctx, c, "model")
	if e != nil {
		return out, e
	}
	out.Models = models
	out.State = "models"
	for _, m := range models {
		if !m.NoAuth {
			out.State = "ready"
			break
		}
	}
	return out, nil
}
func setupCatalog(ctx context.Context, c *client, command string) ([]api.SetupChoice, error) {
	var candidates []wire.SlashArgCandidate
	if e := c.json(ctx, "POST", "/completion/slash-arguments", wire.CompletionRequest{Command: &command, Limit: pointer(1000)}, &candidates, "", ""); e != nil {
		return nil, e
	}
	out := []api.SetupChoice{}
	for _, m := range candidates {
		label := value(m.Display)
		if label == "" {
			label = m.Value
		}
		current := false
		if m.ModelSelection != nil {
			current = value(m.ModelSelection.Current)
		}
		out = append(out, api.SetupChoice{Value: m.Value, Label: label, NoAuth: value(m.NoAuth), Current: current})
	}
	return out, nil
}
func SetupCatalog(ctx context.Context, r api.SetupRequest) ([]api.SetupChoice, error) {
	c, e := setupClient(ctx, r.Settings)
	if e != nil {
		return nil, e
	}
	defer c.http.CloseIdleConnections()
	command := ""
	switch r.Action {
	case "providers":
		command = "connect-provider-api-key"
	case "endpoints":
		command = "connect-baseurl:" + r.Provider
	case "models":
		// The query carries only public model metadata, never a credential.
		payload, _ := json.Marshal(map[string]string{"provider": r.Provider, "base_url": r.BaseURL})
		command = "connect-model:" + url.QueryEscape(string(payload))
	default:
		return nil, errors.New("不支持的模型目录")
	}
	return setupCatalog(ctx, c, command)
}
func ApplySetup(ctx context.Context, r api.SetupRequest) error {
	c, e := setupClient(ctx, r.Settings)
	if e != nil {
		return e
	}
	defer c.http.CloseIdleConnections()
	var status wire.StatusSnapshot
	if e = c.json(ctx, "GET", "/status", nil, &status, "", ""); e != nil {
		return e
	}
	revision := status.Configuration.Revision
	op := "setup-" + rand.Text()
	var body any
	path := ""
	switch r.Action {
	case "connect-model":
		if strings.TrimSpace(r.Provider) == "" || strings.TrimSpace(r.Model) == "" || len(r.APIKey) > 16384 {
			return errors.New("请选择模型服务并填写模型名称")
		}
		providers, e := setupCatalog(ctx, c, "connect-provider-api-key")
		if e != nil {
			return e
		}
		valid := false
		for _, p := range providers {
			if p.Value == r.Provider {
				valid = true
			}
		}
		if !valid {
			return errors.New("该模型服务不支持此连接方式，请刷新")
		}
		cfg := wire.ConnectConfig{Provider: r.Provider, Model: strings.TrimSpace(r.Model)}
		if r.APIKey != "" {
			cfg.ApiKey = &r.APIKey
		}
		if r.BaseURL != "" {
			cfg.BaseUrl = &r.BaseURL
		}
		body = wire.ConnectModelRequest{Config: cfg, ExpectedRevision: &revision, OperationId: &op}
		path = "/configuration/connect-model"
	case "remove-model":
		models, e := setupCatalog(ctx, c, "model")
		if e != nil {
			return e
		}
		found := false
		for _, m := range models {
			if m.Value == r.Model {
				found = true
				if m.Current {
					return errors.New("请先选择其他模型，再移除当前模型")
				}
			}
		}
		if !found {
			return errors.New("该模型已不存在，请刷新")
		}
		body = wire.DeleteModelRequest{Model: r.Model, ExpectedRevision: &revision, OperationId: &op}
		path = "/configuration/delete-model"
	case "use-model":
		body = wire.UseModelRequest{Model: r.Model, ExpectedRevision: &revision, OperationId: &op}
		path = "/configuration/use-model"
	default:
		return errors.New("不支持的模型操作")
	}
	var result wire.CommandResult
	if e = c.json(ctx, "POST", path, body, &result, op, string(revision)); e != nil {
		return errors.New("配置结果未确认，请重新检测模型列表后再操作")
	}
	if !succeeded(result.Outcome) {
		switch result.ErrorCode {
		case "invalid_argument":
			return errors.New("模型配置不完整，请检查 API Key、模型名称和服务地址")
		case "already_exists":
			return errors.New("该模型已连接，请重新检测模型列表")
		case "conflict":
			return errors.New("Caelis 配置已变化，请重新检测后再操作")
		case "permission_denied", "unauthenticated":
			return errors.New("Caelis 未允许修改配置，请检查运行时的本机授权")
		default:
			return errors.New("Caelis 未确认配置成功，请重新检测模型列表后再操作")
		}
	}
	return nil
}
