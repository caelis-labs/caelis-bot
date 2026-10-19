//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics -framework IOKit
#include "presence_darwin.h"
#include <stdlib.h>
*/
import "C"
import (
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
)

func macPresenceSampler(log *diagnosticlog.Logger) func() scheduledata.Sample {
	decoder := presenceSampleDecoder{write: log.Write}
	return func() scheduledata.Sample {
		p := C.bot_presence_sample()
		if p == nil {
			return decoder.decode(nil)
		}
		defer C.free(unsafe.Pointer(p))
		return decoder.decode([]byte(C.GoString(p)))
	}
}
