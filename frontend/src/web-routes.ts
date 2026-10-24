// Mirrors the explicit server allowlist; never sends a receiver/method name.
export const webRoutes:Record<string,{path:string;read:boolean}> = {
 "backend.WorkerTasks":{"path":"/api/v1/tasks/worker-list","read":true},
 "backend.WorkerTaskConversation":{"path":"/api/v1/tasks/worker-conversation","read":true},
 "backend.WorkerTaskObservation":{"path":"/api/v1/tasks/worker-observation","read":true},
 "backend.WorkerFileCopy":{"path":"/api/v1/files/worker-copy","read":true},
 "backend.AuthorizeBotConnection":{"path":"/api/v1/machines/authorize-bot","read":false},
 "backend.ConnectBot":{"path":"/api/v1/machines/connect-bot","read":false},
 "backend.RecoverBotWork":{"path":"/api/v1/machines/recover-bot-work","read":false},
 "backend.RecoverWorkerSharing":{"path":"/api/v1/machines/recover-worker-sharing","read":false},
 "backend.SaveWorkerSharing":{"path":"/api/v1/machines/save-worker-sharing","read":false},
 "backend.WorkerSharing":{"path":"/api/v1/machines/worker-sharing","read":true},
 "backend.SetWorkerSharing":{"path":"/api/v1/machines/set-worker-sharing","read":false},
 "backend.CreateWorkerPairing":{"path":"/api/v1/machines/create-worker-pairing","read":false},
 "backend.RevokeWorkerPeer":{"path":"/api/v1/machines/revoke-worker-peer","read":false},
 "backend.RuntimeExperiments": {"path":"/api/v1/connections/experiments","read":true},
 "backend.ChangeRuntimeExperiments": {"path":"/api/v1/connections/experiments-save","read":false},
 "backend.DiagnosticReport":{"path":"/api/v1/diagnostics/read","read":true},
 "backend.Snapshot": {
  "path": "/api/v1/conversation/snapshot",
  "read": true
 },
 "backend.ApprovalURL":{"path":"/api/v1/conversation/approval-url","read":true},
 "backend.ApprovalPage":{"path":"/api/v1/conversation/approval-page","read":true},
 // Original native adapter receipts handle this submission; arguments never persist.
 "backend.SubmitApprovalPage":{"path":"/api/v1/conversation/approval-page-submit","read":true},
 "backend.ApprovalPageOperation":{"path":"/api/v1/conversation/approval-page-operation","read":true},
 "backend.PetSnapshot": {
  "path": "/api/v1/conversation/pet-snapshot",
  "read": true
 },
 "backend.ComposerSnapshot": {
  "path": "/api/v1/conversation/composer-snapshot",
  "read": true
 },
 "backend.ChatSnapshot": {
  "path": "/api/v1/conversation/chat-snapshot",
  "read": true
 },
 "backend.Draft": {
  "path": "/api/v1/conversation/draft",
  "read": true
 },
 "backend.ProviderInfo": {
  "path": "/api/v1/connections/provider-info",
  "read": true
 },
 "backend.NativePlugins": {
  "path": "/api/v1/connections/native-plugins",
  "read": true
 },
 "backend.RecoveryState": {
  "path": "/api/v1/connections/recovery-state",
  "read": true
 },
 "backend.RuntimeSettings": {
  "path": "/api/v1/connections/runtime-settings",
  "read": true
 },
 "backend.RuntimeProviders": {
  "path": "/api/v1/connections/runtime-providers",
  "read": true
 },
 "backend.SetupOverview": {
  "path": "/api/v1/connections/setup-overview",
  "read": true
 },
 "backend.SetupProfile": {
  "path": "/api/v1/connections/setup-profile",
  "read": true
 },
 "backend.InspectSetup": {
  "path": "/api/v1/connections/inspect-setup",
  "read": true
 },
 "backend.SetupCatalog": {
  "path": "/api/v1/connections/setup-catalog",
  "read": true
 },
 "backend.SetupInstallProgress": {
  "path": "/api/v1/connections/setup-install-progress",
  "read": true
 },
 "backend.ModelSelections": {
  "path": "/api/v1/connections/model-selections",
  "read": true
 },
 "backend.SetupModels": {
  "path": "/api/v1/connections/setup-models",
  "read": true
 },
 "backend.ProfileConfiguration": {
  "path": "/api/v1/connections/profile-configuration",
  "read": true
 },
 "backend.RuntimeReviewMode": {
  "path": "/api/v1/connections/runtime-review-mode",
  "read": true
 },
 "backend.LocalWorkerSelection": {
  "path": "/api/v1/connections/local-worker-selection",
  "read": true
 },
 "backend.ExecutionSettings": {
  "path": "/api/v1/connections/execution-settings",
  "read": true
 },
 "backend.ExecutionOptions": {
  "path": "/api/v1/connections/execution-options",
  "read": true
 },
 "backend.Models": {
  "path": "/api/v1/connections/models",
  "read": true
 },
 "backend.WorkExecutionSettings": {
  "path": "/api/v1/connections/work-execution-settings",
  "read": true
 },
 "backend.RuntimeDefaultModel": {
  "path": "/api/v1/connections/runtime-default-model",
  "read": true
 },
 "backend.RuntimeConfiguration": {
  "path": "/api/v1/connections/runtime-configuration",
  "read": true
 },
 "backend.RuntimeConnectionCatalog": {
  "path": "/api/v1/connections/runtime-connection-catalog",
  "read": true
 },
 "backend.WaitRuntimeConnection": {
  "path": "/api/v1/connections/wait-runtime-connection",
  "read": true
 },
 "backend.Machines": {
  "path": "/api/v1/machines/machines",
  "read": true
 },
 "backend.SSHConfigHosts": {
  "path": "/api/v1/machines/sshconfig-hosts",
  "read": true
 },
 "backend.InspectMachine": {
  "path": "/api/v1/machines/inspect-machine",
  "read": true
 },
 "backend.ReadMachineAdvanced": {
  "path": "/api/v1/machines/read-machine-advanced",
  "read": true
 },
 "backend.InspectLocalWorker": {
  "path": "/api/v1/connections/inspect-local-worker",
  "read": true
 },
 "backend.BotInitialization": {
  "path": "/api/v1/connections/bot-initialization",
  "read": true
 },
 "backend.MediaImage": {
  "path": "/api/v1/connections/media-image",
  "read": true
 },
 "backend.ScreenImage": {
  "path": "/api/v1/connections/screen-image",
  "read": true
 },
 "backend.ImageInput": {
  "path": "/api/v1/connections/image-input",
  "read": true
 },
 "backend.Submit": {
  "path": "/api/v1/conversation/submit",
  "read": false
 },
 "backend.SaveDraft": {
  "path": "/api/v1/conversation/save-draft",
  "read": false
 },
 "backend.LoadEarlier": {
  "path": "/api/v1/conversation/load-earlier",
  "read": true
 },
 "backend.DismissPreview": {
  "path": "/api/v1/conversation/dismiss-preview",
  "read": false
 },
 "backend.Reconnect": {
  "path": "/api/v1/connections/reconnect",
  "read": false
 },
 "backend.Connect": {
  "path": "/api/v1/connections/connect",
  "read": false
 },
 "backend.Interrupt": {
  "path": "/api/v1/conversation/interrupt",
  "read": false
 },
 "backend.Decide": {
  "path": "/api/v1/conversation/decide",
  "read": false
 },
 "backend.AnswerQuestion": {
  "path": "/api/v1/conversation/answer-question",
  "read": false
 },
 "backend.DismissQuestion": {
  "path": "/api/v1/conversation/dismiss-question",
  "read": false
 },
 "backend.ApplySetup": {
  "path": "/api/v1/connections/apply-setup",
  "read": false
 },
 "backend.ActivateRuntime": {
  "path": "/api/v1/connections/activate-runtime",
  "read": false
 },
 "backend.DismissSetup": {
  "path": "/api/v1/connections/dismiss-setup",
  "read": false
 },
 "backend.CancelSetupOperation": {
  "path": "/api/v1/connections/cancel-setup-operation",
  "read": false
 },
 "backend.SaveSelectedModel": {
  "path": "/api/v1/connections/save-selected-model",
  "read": false
 },
 "backend.SaveExecutionSettings": {
  "path": "/api/v1/connections/save-execution-settings",
  "read": false
 },
 "backend.SaveWorkExecutionSettings": {
  "path": "/api/v1/connections/save-work-execution-settings",
  "read": false
 },
 "backend.SaveLocalWorkerModel": {
  "path": "/api/v1/connections/save-local-worker-model",
  "read": false
 },
 "backend.ChangeRuntimeConfiguration": {
  "path": "/api/v1/connections/change-runtime-configuration",
  "read": false
 },
 "backend.ChangeProfileConfiguration": {
  "path": "/api/v1/connections/change-profile-configuration",
  "read": false
 },
 "backend.EnableRuntimeAutoReview": {
  "path": "/api/v1/connections/enable-runtime-auto-review",
  "read": false
 },
 "backend.StartRuntimeConnection": {
  "path": "/api/v1/connections/start-runtime-connection",
  "read": false
 },
 "backend.AdvanceRuntimeConnection": {
  "path": "/api/v1/connections/advance-runtime-connection",
  "read": false
 },
 "backend.CancelRuntimeConnection": {
  "path": "/api/v1/connections/cancel-runtime-connection",
  "read": false
 },
 "backend.ConnectMachine": {
  "path": "/api/v1/machines/connect-machine",
  "read": false
 },
 "backend.SaveMachineModel": {
  "path": "/api/v1/machines/save-machine-model",
  "read": false
 },
 "backend.ChangeMachineTeam": {
  "path": "/api/v1/machines/change-machine-team",
  "read": false
 },
 "backend.RemoveMachine": {
  "path": "/api/v1/machines/remove-machine",
  "read": false
 },
 "backend.InitializeBot": {
  "path": "/api/v1/connections/initialize-bot",
  "read": false
 },
 "backend.RetryBotIntroduction": {
  "path": "/api/v1/connections/retry-bot-introduction",
  "read": false
 },
 "desktop.LanguagePreferences": {
  "path": "/api/v1/preferences/language-preferences",
  "read": true
 },
 "desktop.DisplayPreferences": {
  "path": "/api/v1/preferences/display-preferences",
  "read": true
 },
 "desktop.ContentState": {
  "path": "/api/v1/preferences/content-state",
  "read": true
 },
 "desktop.Appearance": {
  "path": "/api/v1/preferences/appearance",
  "read": true
 },
 "desktop.Plugins": {
  "path": "/api/v1/plugins/plugins",
  "read": true
 },
 "desktop.PluginSkillDetail": {
  "path": "/api/v1/plugins/plugin-skill-detail",
  "read": true
 },
 "desktop.PluginServerDetail": {
  "path": "/api/v1/plugins/plugin-server-detail",
  "read": true
 },
 "desktop.TelegramStatus": {
  "path": "/api/v1/channels/telegram-status",
  "read": true
 },
 "desktop.WeixinStatus": {
  "path": "/api/v1/channels/weixin-status",
  "read": true
 },
 "desktop.FeatureCapabilities": {
  "path": "/api/v1/preferences/feature-capabilities",
  "read": true
 },
 "desktop.CapturePreferences": {
  "path": "/api/v1/preferences/capture-preferences",
  "read": true
 },
 "desktop.FeatureGuidePending": {
  "path": "/api/v1/preferences/feature-guide-pending",
  "read": true
 },
 "desktop.TaskPreferences": {
  "path": "/api/v1/preferences/task-preferences",
  "read": true
 },
 "desktop.AttachmentStorage": {
  "path": "/api/v1/preferences/attachment-storage",
  "read": true
 },
 "desktop.DraftFiles": {
  "path": "/api/v1/files/draft-files",
  "read": true
 },
 "desktop.DraftImage": {
  "path": "/api/v1/files/draft-image",
  "read": true
 },
 "desktop.DraftImagePreview": {
  "path": "/api/v1/files/draft-image-preview",
  "read": true
 },
 "desktop.PreviewArtifact": {
  "path": "/api/v1/files/preview-artifact",
  "read": true
 },
 "desktop.SaveLanguage": {
  "path": "/api/v1/preferences/save-language",
  "read": false
 },
 "desktop.SaveDisplay": {
  "path": "/api/v1/preferences/save-display",
  "read": false
 },
 "desktop.SelectAppearance": {
  "path": "/api/v1/preferences/select-appearance",
  "read": false
 },
 "desktop.FallbackAppearance": {
  "path": "/api/v1/preferences/fallback-appearance",
  "read": false
 },
 "desktop.RemoveContent": {
  "path": "/api/v1/preferences/remove-content",
  "read": false
 },
 "desktop.PluginAction": {
  "path": "/api/v1/plugins/plugin-action",
  "read": false
 },
 "desktop.PluginConnection": {
  "path": "/api/v1/plugins/plugin-connection",
  "read": false
 },
 "desktop.PluginOAuthStart": {
  "path": "/api/v1/plugins/plugin-oauth-start",
  "read": false
 },
 "desktop.PluginOAuthCancel": {
  "path": "/api/v1/plugins/plugin-oauth-cancel",
  "read": false
 },
 "desktop.ConnectTelegram": {
  "path": "/api/v1/channels/connect-telegram",
  "read": false
 },
 "desktop.ConfirmTelegram": {
  "path": "/api/v1/channels/confirm-telegram",
  "read": false
 },
 "desktop.DisconnectTelegram": {
  "path": "/api/v1/channels/disconnect-telegram",
  "read": false
 },
 "desktop.ForgetTelegram": {
  "path": "/api/v1/channels/forget-telegram",
  "read": false
 },
 "desktop.StartWeixinPairing": {
  "path": "/api/v1/channels/start-weixin-pairing",
  "read": false
 },
 "desktop.VerifyWeixinPairing": {
  "path": "/api/v1/channels/verify-weixin-pairing",
  "read": false
 },
 "desktop.ConfirmWeixinPairing": {
  "path": "/api/v1/channels/confirm-weixin-pairing",
  "read": false
 },
 "desktop.PauseWeixin": {
  "path": "/api/v1/channels/pause-weixin",
  "read": false
 },
 "desktop.ResumeWeixin": {
  "path": "/api/v1/channels/resume-weixin",
  "read": false
 },
 "desktop.ForgetWeixin": {
  "path": "/api/v1/channels/forget-weixin",
  "read": false
 },
 "desktop.FinishFeatureGuide": {
  "path": "/api/v1/preferences/finish-feature-guide",
  "read": false
 },
 "desktop.SaveTaskPreferences": {
  "path": "/api/v1/preferences/save-task-preferences",
  "read": false
 },
 "desktop.CleanAttachmentStorage": {
  "path": "/api/v1/preferences/clean-attachment-storage",
  "read": false
 },
 "desktop.RemoveFile": {
  "path": "/api/v1/files/remove-file",
  "read": false
 },
 "desktop.ReportPresentationWarning": {
  "path": "/api/v1/preferences/report-presentation-warning",
  "read": false
 }
};
