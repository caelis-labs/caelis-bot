package caelis

import (
	"bufio"
	"encoding/json"
	"mime"
	"net/url"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (f *connectionFlow) accountModels() {
	endpoints, err := rawCatalog(f.ctx, f.client, "connect-baseurl:"+f.request.Choice)
	if err != nil || len(endpoints) == 0 {
		f.unknown()
		return
	}
	f.request.BaseURL = endpoints[0].Value
	payload, _ := json.Marshal(map[string]string{"provider": f.request.Choice, "base_url": f.request.BaseURL})
	models, err := rawCatalog(f.ctx, f.client, "connect-model:"+url.QueryEscape(string(payload)))
	if err != nil {
		f.unknown()
		return
	}
	f.update(func(v *api.RuntimeFlow) {
		v.Stage = "models"
		v.Title = "选择账号模型"
		v.Message = "选择模型后，通过提供方的浏览器页面登录。"
		for _, m := range models {
			v.Models = append(v.Models, api.RuntimeFlowModel{ID: m.Value, Name: value(m.Display), Description: value(m.Detail)})
		}
	})
}
func (f *connectionFlow) setOperation() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.operation = "connection-" + f.view.ID + "-" + f.view.Revision
	return f.operation
}
func (f *connectionFlow) connectProvider(model, secret string, oauth bool) {
	op := f.setOperation()
	config := wire.ConnectConfig{Provider: f.request.Choice, Model: model, ImageInput: f.request.ImageInput, ReasoningLevels: f.request.ReasoningLevels}
	if f.request.ContextWindowTokens > 0 {
		config.ContextWindowTokens = &f.request.ContextWindowTokens
	}
	if f.request.MaxOutputTokens > 0 {
		config.MaxOutputTokens = &f.request.MaxOutputTokens
	}
	if f.request.BaseURL != "" {
		config.BaseUrl = &f.request.BaseURL
	}
	if secret != "" {
		config.ApiKey = &secret
	}
	request := wire.ConnectModelRequest{Config: config, OperationId: &op, ExpectedRevision: &f.revision}
	if !oauth {
		var result wire.CommandResult
		err := f.client.jsonTimeout(f.ctx, "POST", "/configuration/connect-model", request, &result, op, string(f.revision), 10*time.Minute)
		secret = ""
		if f.result(result, err) {
			f.complete()
		}
		return
	}
	response, err := f.client.requestMedia(f.ctx, "POST", "/configuration/connect-model", request, op, string(f.revision), "text/event-stream")
	if err != nil {
		f.unknown()
		return
	}
	defer response.Body.Close()
	media, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if media != "text/event-stream" || response.StatusCode != 200 {
		f.unknown()
		return
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for {
		frame, err := readFrame(scanner)
		if err != nil {
			f.unknown()
			return
		}
		if frame.event != "model_authentication" {
			continue
		}
		var snapshot wire.ModelAuthenticationSnapshot
		if json.Unmarshal(frame.data, &snapshot) != nil || snapshot.OperationId != op {
			f.unknown()
			return
		}
		if snapshot.Result != nil {
			if f.result(*snapshot.Result, nil) {
				f.complete()
			}
			return
		}
		f.mu.Lock()
		f.challenge = value(snapshot.ChallengeId)
		f.view.Stage = "authorization"
		f.view.Title = "完成浏览器授权"
		f.view.Message = "登录信息由 Caelis 保存。完成后会自动继续。"
		f.view.Authorization = &api.RuntimeAuthorization{URL: value(snapshot.VerificationUrl), UserCode: value(snapshot.UserCode), InputLabel: value(snapshot.Prompt), CanSubmit: f.challenge != "", ExpiresAt: f.created.Add(10 * time.Minute).Format(time.RFC3339)}
		if strings.TrimSpace(value(snapshot.VerificationUrl)) == "" && f.challenge == "" {
			f.view.Stage = "preparing"
			f.view.Title = "正在连接模型"
		}
		f.publishLocked()
		f.mu.Unlock()
	}
}
