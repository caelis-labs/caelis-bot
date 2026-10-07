package desktop

import (
	"log"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/contentpack"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
)

// productAssembly is the one product owner for a native host. It has no Wails
// dependency: only the host creates windows, menus and platform effects.
type productAssembly struct {
	Service     *Service
	Core        *app.Application
	Diagnostics *diagnosticlog.Logger
}

func newProductAssembly(root string, languages []string, effects func(*Service) app.Host) (*productAssembly, error) {
	diagnostics := diagnosticlog.NewAsync(filepath.Join(root, "Logs"))
	s := newService(fileStore{filepath.Join(root, "placement.json")})
	s.configurePermissionGuide(filepath.Join(root, "permission-guide.json"))
	s.configureFeatureGuide(filepath.Join(root, "feature-guide.json"))
	s.configureShortcut(filepath.Join(root, "shortcut.json"))
	s.configureTaskShortcut(filepath.Join(root, "task-shortcut.json"))
	logError(s.configureCapture(filepath.Join(root, "Captures")))
	logError(s.configureLanguage(filepath.Join(root, "language.json"), languages))
	s.content = contentpack.NewRecoveringRegistry(filepath.Join(root, "content"))
	host := effects(s)
	host.Locale = func() i18n.Locale { return s.LanguagePreferences().Locale }
	host.Diagnostics = diagnostics
	host.ResolveFiles = s.resolveDraftFiles
	host.ConsumeFiles = s.consumeDraftFilesChecked
	host.Gesture = s.Gesture
	host.Notify = s.Notify
	host.Observe = s.observeCharacter
	host.ObserveTasks = s.observeTasks
	host.ReportError = logError
	core, err := app.New(root, host)
	if err != nil {
		return nil, err
	}
	back := core.Backend
	s.telegram = core.Telegram
	s.openExternalURL = back.OpenMessageLink
	s.configureCaptureBackend(back)
	s.taskPreferences = core.TaskPreferences
	s.saveTaskPreferences = core.SaveTaskPreferences
	s.removeTaskPin = func(id string) error { _, err := core.PinTask(id, false); return err }
	s.lockTaskPin = func(id string, locked bool) error { _, err := core.LockTask(id, locked); return err }
	s.clearTaskPins = core.ClearTasks
	s.moveTaskPin = core.MoveTask
	s.needsIntroduction = func() bool {
		v := back.BotInitialization()
		return v.Required || v.Status == "rejected" || v.Status == "unknown"
	}
	logError(s.configureSelection(filepath.Join(core.ProviderDirectory(), "draft-files.json")))
	s.storage, s.cleanStorage = core.AttachmentStorage, core.CleanAttachments
	s.diagnosticReport = back.DiagnosticReport
	s.activate = func() {
		if core.NeedsSetup() || !core.HasRuntimeChoice() {
			s.showSettings("setup")
			return
		}
		v := back.ComposerSnapshot()
		for _, approval := range v.Approvals {
			if approval.Status != "resolved" {
				s.OpenApproval()
				return
			}
		}
		if v.CanSend {
			s.CloseHistory()
			s.TogglePanel()
		} else {
			s.OpenHistory()
		}
	}
	return &productAssembly{Service: s, Core: core, Diagnostics: diagnostics}, nil
}

func (p *productAssembly) Start() error {
	if p.Core.NeedsSetup() {
		p.Service.showSettings("setup")
	}
	p.Core.StartBackground()
	return nil
}

func logError(err error) {
	if err != nil {
		log.Print(err)
	}
}
