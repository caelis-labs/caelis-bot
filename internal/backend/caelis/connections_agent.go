package caelis

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (f *connectionFlow) startAgent() {
	launchers, err := rawCatalog(f.ctx, f.client, "connect-acp-launcher:"+f.request.Choice)
	if err != nil || len(launchers) == 0 {
		f.unknown()
		return
	}
	if launchers[0].Value == "install" || launchers[0].Value == "manual" {
		plan, err := rawCatalog(f.ctx, f.client, "connect-acp-install:"+f.request.Choice)
		if err != nil || len(plan) == 0 || plan[0].RuntimeSetup == nil {
			f.unknown()
			return
		}
		setup := plan[0].RuntimeSetup
		f.install = wire.RuntimeInstallation{Directory: setup.Directory, ArchiveUrl: value(setup.ArchiveUrl), Sha256: setup.Sha256}
		f.launcher = "installed"
		f.update(func(v *api.RuntimeFlow) {
			v.Stage = "installation"
			v.Title = "准备 " + f.request.Choice
			v.Message = "需要完整的 ACP 运行时，安装前请核对来源和目录。"
			v.Installation = &api.RuntimeInstallation{Destination: setup.Directory, Source: value(setup.ArchiveUrl), Instructions: strings.Join(setup.ManualSteps, "\n"), CanInstall: value(setup.ArchiveUrl) != ""}
		})
		return
	}
	if f.request.Choice != "custom" && (len(launchers) > 1 || launchers[0].Value == "npx" || launchers[0].Value == "managed") {
		f.update(func(v *api.RuntimeFlow) {
			v.Stage = "launcher"
			v.Title = "选择 Agent 启动方式"
			v.Message = "按本机安装情况选择；首次通过包管理器启动可能下载程序。"
			v.Launchers = []api.RuntimeConnectChoice{}
			for _, l := range launchers {
				v.Launchers = append(v.Launchers, api.RuntimeConnectChoice{ID: l.Value, Name: value(l.Display), Description: value(l.Detail)})
			}
		})
		return
	}
	f.launcher = wire.ACPLauncherChoice(launchers[0].Value)
	f.prepareAgent(wire.ACPPrepareRequest{AdapterId: &f.request.Choice, Launcher: f.launcher, CommandLine: pointer(f.request.Command)})
}
func (f *connectionFlow) prepareInstalled(action, destination string) {
	if !filepath.IsAbs(destination) {
		f.update(func(v *api.RuntimeFlow) {
			v.Stage = "failed"
			v.Title = "安装目录无效"
			v.Message = "请选择完整的本机目录路径。"
		})
		return
	}
	request := wire.ACPPrepareRequest{AdapterId: &f.request.Choice, Launcher: f.launcher}
	if action == "install" {
		installation := f.install
		installation.Directory = destination
		request.Install = &installation
	}
	// Manual checks use the native discovery path. A non-default existing path
	// is connected through Custom command, as in the TUI.
	if action == "check-installation" && destination != f.install.Directory {
		f.update(func(v *api.RuntimeFlow) {
			v.Stage = "failed"
			v.Title = "请使用自定义 Agent"
			v.Message = "非默认目录请通过自定义命令连接其完整可执行路径。"
		})
		return
	}
	f.prepareAgent(request)
}
func (f *connectionFlow) prepareAgent(request wire.ACPPrepareRequest) {
	op := f.setOperation()
	body := wire.PrepareACPRequest{Request: request, OperationId: &op, ExpectedRevision: &f.revision}
	var result wire.CommandResult
	err := f.client.jsonTimeout(f.ctx, "POST", "/agents/prepare-acp", body, &result, op, string(f.revision), 10*time.Minute)
	if !f.result(result, err) {
		return
	}
	f.readPreparation(result)
}
func (f *connectionFlow) readPreparation(result wire.CommandResult) {
	if result.Resource == nil || value(result.Resource.Ref) == "" || value(result.Resource.Kind) != "acp_preparation" {
		f.unknown()
		return
	}
	var preparation wire.ACPPreparation
	if f.client.json(f.ctx, "GET", "/agents/acp-preparations/"+idPath(value(result.Resource.Ref)), nil, &preparation, "", "") != nil {
		f.unknown()
		return
	}
	f.preparation = preparation
	if preparation.ContentDigest != value(result.Resource.Digest) || preparation.Ref != value(result.Resource.Ref) {
		f.unknown()
		return
	}
	f.projectPreparation()
}
func (f *connectionFlow) projectPreparation() {
	p := f.preparation
	if p.State == "needs_auth" {
		f.update(func(v *api.RuntimeFlow) {
			v.Stage = "auth-method"
			v.Title = "选择 Agent 认证方式"
			v.Message = "认证方式由本机 Agent 声明。"
			v.Methods = []api.RuntimeAuthMethod{}
			for _, m := range p.AuthenticationMethods {
				terminal := m.Type == "terminal"
				method := api.RuntimeAuthMethod{ID: m.Id, Name: value(m.Name), Description: value(m.Description), Available: !terminal}
				if terminal {
					method.Reason = "此认证需要交互式终端，请先在 Caelis TUI 中完成。"
				}
				v.Methods = append(v.Methods, method)
			}
		})
		return
	}
	if p.State != "ready" || p.Discovery == nil {
		f.unknown()
		return
	}
	f.update(func(v *api.RuntimeFlow) {
		v.Stage = "models"
		v.Title = "选择 Agent 模型"
		v.Message = "只使用 Agent 声明的模型和能力。"
		v.Models = []api.RuntimeFlowModel{{ID: "@default", Name: "Agent 默认模型", Description: "由 Agent 管理"}}
		for _, m := range p.Discovery.Models {
			id := value(m.Id)
			if id == "" {
				continue
			}
			name := value(m.Name)
			if name == "" {
				name = id
			}
			v.Models = append(v.Models, api.RuntimeFlowModel{ID: id, Name: name, Description: value(m.Description)})
		}
	})
}
func (f *connectionFlow) authenticateAgent(method string) {
	op := f.setOperation()
	body := wire.PrepareACPAuthenticationRequest{PreparationRef: f.preparation.Ref, PreparationDigest: f.preparation.ContentDigest, MethodId: method, OperationId: &op, ExpectedRevision: &f.revision}
	var result wire.CommandResult
	f.update(func(v *api.RuntimeFlow) {
		v.Title = "等待 Agent 认证"
		v.Message = "如 Agent 打开了浏览器，请在那里完成登录。"
	})
	err := f.client.jsonTimeout(f.ctx, "POST", "/agents/prepare-acp-auth", body, &result, op, string(f.revision), 10*time.Minute)
	if f.result(result, err) {
		f.readPreparation(result)
	}
}
func (f *connectionFlow) connectAgent(model string) {
	if model == "@default" {
		model = ""
	}
	if model != value(f.preparation.Request.ModelId) {
		request := f.preparation.Request
		request.ParentRef = &f.preparation.Ref
		request.ModelId = &model
		request.Install = nil
		f.prepareAgent(request)
		f.mu.Lock()
		prepared := f.view.Stage == "models"
		f.mu.Unlock()
		if f.preparation.State != "ready" || value(f.preparation.Request.ModelId) != model || !prepared {
			return
		}
	}
	op := f.setOperation()
	body := wire.ConnectACPRequest{PreparationRef: f.preparation.Ref, PreparationDigest: f.preparation.ContentDigest, OperationId: &op, ExpectedRevision: &f.revision}
	var result wire.CommandResult
	err := f.client.jsonTimeout(f.ctx, "POST", "/agents/connect-acp", body, &result, op, string(f.revision), 10*time.Minute)
	if f.result(result, err) {
		f.complete()
	}
}
