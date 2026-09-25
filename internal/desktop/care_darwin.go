//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics -framework IOKit
#include "care_darwin.h"
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/care"
)

func macCareSample() care.Sample {
	p := C.bot_care_sample()
	if p == nil {
		return care.Sample{}
	}
	defer C.free(unsafe.Pointer(p))
	var v struct {
		Awake       bool
		Unlocked    *bool
		IdleSeconds float64
		Application string
	}
	if json.Unmarshal([]byte(C.GoString(p)), &v) != nil {
		return care.Sample{}
	}
	return care.Sample{Presence: care.Presence{Awake: v.Awake, Unlocked: v.Unlocked}, IdleSeconds: v.IdleSeconds, Application: v.Application}
}
