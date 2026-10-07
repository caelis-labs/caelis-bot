//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ServiceManagement -framework Carbon
#include "login_at_login_darwin.h"
#include <stdlib.h>
*/
import "C"
import (
	"errors"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type macLoginAtLogin struct{ bundleID string }

func newMacLoginAtLogin() loginAtLoginController {
	_, id := applicationIdentity()
	return macLoginAtLogin{bundleID: id}
}

func (m macLoginAtLogin) Status() LoginAtLoginStatus {
	id := C.CString(m.bundleID)
	defer C.free(unsafe.Pointer(id))
	state := application.InvokeSyncWithResult(func() int { return int(C.bot_login_item_status(id)) })
	switch state {
	case -1:
		return LoginAtLoginStatus{State: "unsupported"}
	case 0:
		return LoginAtLoginStatus{Supported: true, State: "off"}
	case 1:
		return LoginAtLoginStatus{Supported: true, State: "on", Enabled: true, Registered: true}
	case 2:
		return LoginAtLoginStatus{Supported: true, State: "needsApproval", Registered: true}
	default:
		return LoginAtLoginStatus{Supported: true, State: "unavailable"}
	}
}

func (m macLoginAtLogin) Set(enabled bool) error {
	id := C.CString(m.bundleID)
	defer C.free(unsafe.Pointer(id))
	value := C.int(0)
	if enabled {
		value = 1
	}
	type result struct {
		ok      bool
		message string
	}
	r := application.InvokeSyncWithResult(func() result {
		var failure *C.char
		ok := C.bot_login_item_set(id, value, &failure) != 0
		if failure == nil {
			return result{ok: ok}
		}
		defer C.free(unsafe.Pointer(failure))
		return result{ok: ok, message: C.GoString(failure)}
	})
	if !r.ok {
		if r.message == "" {
			r.message = "system login item unavailable"
		}
		return errors.New(r.message)
	}
	return nil
}

func (m macLoginAtLogin) OpenSystemSettings() error {
	if application.InvokeSyncWithResult(func() bool { return C.bot_login_item_open_settings() != 0 }) {
		return nil
	}
	return errLoginAtLoginUnsupported
}

func macTrackLoginLaunch() { C.bot_track_login_launch() }

func macLaunchedAtLogin() bool {
	return application.InvokeSyncWithResult(func() bool { return C.bot_launched_at_login() != 0 })
}
