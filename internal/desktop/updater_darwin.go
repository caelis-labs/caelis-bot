//go:build darwin && cgo

package desktop

/*
#include "updater_darwin.h"
*/
import "C"
import (
	"errors"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type macUpdater struct {
	available bool
	preparing atomic.Bool
	prepare   func() error
	cancel    func()
	close     func()
}

var activeUpdater atomic.Pointer[macUpdater]

func startMacUpdater(s *Service, prepare func() error, cancel, close func()) {
	u := &macUpdater{prepare: prepare, cancel: cancel, close: close}
	application.InvokeSync(func() { u.available = C.bot_updater_start() == 1 })
	activeUpdater.Store(u)
	s.mu.Lock()
	s.updatePreferences = func() UpdatePreferences {
		return application.InvokeSyncWithResult(func() UpdatePreferences {
			return UpdatePreferences{Available: u.available, Automatic: bool(C.bot_updater_automatic()), Waiting: bool(C.bot_updater_waiting())}
		})
	}
	s.setAutomaticUpdates = func(enabled bool) error {
		if !u.available {
			return errors.New("此构建未启用自动更新，请从发布页安装正式版")
		}
		application.InvokeSync(func() { C.bot_updater_set_automatic(C.bool(enabled)) })
		return nil
	}
	s.checkNativeUpdates = func() error {
		if !u.available {
			return errors.New("此构建未启用自动更新，请从发布页安装正式版")
		}
		result := application.InvokeSyncWithResult(func() int { return int(C.bot_updater_check()) })
		if result != 1 {
			return errors.New("更新窗口已打开或正在处理更新")
		}
		return nil
	}
	s.mu.Unlock()
}

//export botUpdaterPrepare
func botUpdaterPrepare() {
	u := activeUpdater.Load()
	if u == nil || !u.preparing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		// This may wait for a scheduled submission. Never hold the AppKit thread.
		if err := u.prepare(); err != nil {
			u.preparing.Store(false)
			return
		}
		if !application.InvokeSyncWithResult(func() bool { return bool(C.bot_updater_claim()) }) {
			u.cancel()
			u.preparing.Store(false)
			return
		}
		u.close()
		application.InvokeSync(func() { C.bot_updater_finish() })
	}()
}

func stopMacUpdater() {
	activeUpdater.Store(nil)
	application.InvokeSync(func() { C.bot_updater_stop() })
}
