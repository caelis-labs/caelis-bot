//go:build darwin && cgo

package desktop

/*
#include "updater_darwin.h"
#include <stdlib.h>
*/
import "C"
import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type macUpdater struct {
	available      bool
	preparing      atomic.Bool
	phase          atomic.Int32 // 0 idle, 1 preparing, 2 claimed, 3 cancelled/failed, 4 committing
	statusMu       sync.Mutex
	state, message string
	prepare        func() error
	cancel         func()
	close          func() error
}

var activeUpdater atomic.Pointer[macUpdater]

func (u *macUpdater) status(state, message string) {
	u.statusMu.Lock()
	u.state, u.message = state, message
	u.statusMu.Unlock()
}

func (u *macUpdater) fail(err error) {
	u.phase.Store(3)
	u.status("failed", err.Error())
	if activeUpdater.Load() != u {
		return
	}
	message := C.CString(err.Error())
	defer C.free(unsafe.Pointer(message))
	application.InvokeSync(func() { C.bot_updater_fail(message) })
}

func startMacUpdater(s *Service, prepare func() error, cancel func(), close func() error) {
	u := &macUpdater{prepare: prepare, cancel: cancel, close: close}
	application.InvokeSync(func() { u.available = C.bot_updater_start() == 1 })
	activeUpdater.Store(u)
	s.mu.Lock()
	s.updatePreferences = func() UpdatePreferences {
		preferences := application.InvokeSyncWithResult(func() UpdatePreferences {
			return UpdatePreferences{Available: u.available, Automatic: bool(C.bot_updater_automatic()), Waiting: bool(C.bot_updater_waiting())}
		})
		u.statusMu.Lock()
		preferences.State, preferences.Message = u.state, u.message
		u.statusMu.Unlock()
		return preferences
	}
	s.setAutomaticUpdates = func(enabled bool) error {
		if !u.available {
			return errors.New(s.text("native.autoUpdateDisabledBuild", nil))
		}
		application.InvokeSync(func() { C.bot_updater_set_automatic(C.bool(enabled)) })
		return nil
	}
	s.checkNativeUpdates = func() error {
		if !u.available {
			return errors.New(s.text("native.autoUpdateDisabledBuild", nil))
		}
		result := application.InvokeSyncWithResult(func() int { return int(C.bot_updater_check()) })
		if result != 1 {
			return errors.New(s.text("native.updateInProgress", nil))
		}
		u.phase.Store(0)
		u.status("", "")
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
	u.phase.Store(1)
	go func() {
		defer u.preparing.Store(false)
		// Scheduled admission and native owners are fenced off the AppKit thread.
		u.status("preparing", "")
		deadline := time.AfterFunc(30*time.Second, func() {
			if u.phase.CompareAndSwap(1, 3) {
				go u.cancel()
				u.fail(errors.New("更新准备超过 30 秒，安装已停止；请检查本机状态后重试"))
			}
		})
		defer deadline.Stop()
		if err := u.prepare(); err != nil {
			u.cancel()
			if u.phase.CompareAndSwap(1, 3) {
				u.fail(err)
			}
			return
		}
		if activeUpdater.Load() != u {
			u.cancel()
			u.phase.Store(3)
			return
		}
		if !u.phase.CompareAndSwap(1, 2) {
			u.cancel()
			return
		}
		if !application.InvokeSyncWithResult(func() bool { return bool(C.bot_updater_claim()) }) {
			u.cancel()
			u.phase.Store(3)
			u.status("", "")
			return
		}
		u.status("closing", "")
		closeDeadline := time.AfterFunc(45*time.Second, func() {
			if u.phase.CompareAndSwap(2, 3) {
				u.fail(errors.New("更新关闭超过 45 秒，安装已停止；请检查本机状态后重试"))
			}
		})
		if err := u.close(); err != nil {
			closeDeadline.Stop()
			if u.phase.CompareAndSwap(2, 3) {
				u.fail(err)
			}
			// The core close is one-way. Surface the failure before exiting the
			// old process; never leave a stopped backend looking operational.
			application.InvokeSync(func() { C.bot_updater_quit_after_failure() })
			return
		}
		closeDeadline.Stop()
		if !u.phase.CompareAndSwap(2, 4) {
			// Shutdown completed after the deadline. The app is now closed,
			// but the postponed install was cancelled and must not run.
			application.InvokeSync(func() { C.bot_updater_quit_after_failure() })
			return
		}
		if !application.InvokeSyncWithResult(func() bool { return bool(C.bot_updater_finish()) }) {
			u.fail(errors.New("安装交接已取消；请重新打开 Caelis Bot 并检查版本"))
			application.InvokeSync(func() { C.bot_updater_quit_after_failure() })
			return
		}
		u.status("restarting", "")
	}()
}

//export botUpdaterAborted
func botUpdaterAborted() {
	u := activeUpdater.Load()
	if u == nil || !u.phase.CompareAndSwap(1, 3) {
		return
	}
	go func() {
		u.cancel()
		u.status("", "")
	}()
}

func stopMacUpdater() {
	activeUpdater.Store(nil)
	application.InvokeSync(func() { C.bot_updater_stop() })
}
